package shast

import (
	"fmt"

	"mvdan.cc/sh/v3/syntax"
)

// converter turns a parsed *syntax.File into this package's JSON node tree.
// It carries the original source, so word nodes can report a verbatim text
// span, and whether --pos output was requested.
type converter struct {
	src     string
	withPos bool
}

func (c *converter) sliceSrc(start, end syntax.Pos) string {
	if !start.IsValid() || !end.IsValid() {
		return ""
	}
	s, e := int(start.Offset()), int(end.Offset())
	if s < 0 || e > len(c.src) || s > e {
		return ""
	}
	return c.src[s:e]
}

type unsupportedNode struct {
	Kind   string   `json:"kind"`
	Pos    *posSpan `json:"pos,omitempty"`
	GoType string   `json:"go_type"`
}

// unsupported is the fallback for a concrete Node type not covered by this
// package's type switches. Every concrete type mvdan.cc/sh/v3/syntax exposes
// today is handled explicitly, so this only fires if a future dependency
// upgrade adds a new node kind; it degrades visibly instead of panicking or
// silently dropping the node.
func (c *converter) unsupported(n syntax.Node) *unsupportedNode {
	return &unsupportedNode{Kind: "unsupported", Pos: c.posOf(n), GoType: fmt.Sprintf("%T", n)}
}

type fileNode struct {
	Kind             string         `json:"kind"`
	Pos              *posSpan       `json:"pos,omitempty"`
	Statements       []*stmtNode    `json:"statements"`
	TrailingComments []*commentNode `json:"trailing_comments"`
}

func (c *converter) file(f *syntax.File) *fileNode {
	return &fileNode{
		Kind:             "file",
		Pos:              c.posOf(f),
		Statements:       c.stmts(f.Stmts),
		TrailingComments: c.comments(f.Last),
	}
}

type stmtNode struct {
	Kind       string          `json:"kind"`
	Pos        *posSpan        `json:"pos,omitempty"`
	Command    any             `json:"command,omitempty"`
	Comments   []*commentNode  `json:"comments"`
	Negated    bool            `json:"negated,omitempty"`
	Background bool            `json:"background,omitempty"`
	Coprocess  bool            `json:"coprocess,omitempty"`
	Disown     bool            `json:"disown,omitempty"`
	Redirects  []*redirectNode `json:"redirects"`
}

func (c *converter) stmt(s *syntax.Stmt) *stmtNode {
	if s == nil {
		return nil
	}

	return &stmtNode{
		Kind:       "statement",
		Pos:        c.posOf(s),
		Command:    c.command(s.Cmd),
		Comments:   c.comments(s.Comments),
		Negated:    s.Negated,
		Background: s.Background,
		Coprocess:  s.Coprocess,
		Disown:     s.Disown,
		Redirects:  c.redirects(s.Redirs),
	}
}

func (c *converter) stmts(ss []*syntax.Stmt) []*stmtNode {
	out := make([]*stmtNode, 0, len(ss))
	for _, s := range ss {
		out = append(out, c.stmt(s))
	}
	return out
}

type assignNode struct {
	Kind   string       `json:"kind"`
	Pos    *posSpan     `json:"pos,omitempty"`
	Append bool         `json:"append,omitempty"`
	Naked  bool         `json:"naked,omitempty"`
	Name   *literalNode `json:"name,omitempty"`
	Index  any          `json:"index,omitempty"`
	Value  *wordNode    `json:"value,omitempty"`
	Array  *arrayNode   `json:"array,omitempty"`
}

func (c *converter) assign(x *syntax.Assign) *assignNode {
	var name *literalNode
	if x.Name != nil {
		name = c.literal(x.Name)
	}

	var index any
	if x.Index != nil {
		index = c.arithmExpr(x.Index)
	}

	var value *wordNode
	if x.Value != nil {
		value = c.word(x.Value)
	}

	var array *arrayNode
	if x.Array != nil {
		array = c.array(x.Array)
	}

	return &assignNode{
		Kind:   "assign",
		Pos:    c.posOf(x),
		Append: x.Append,
		Naked:  x.Naked,
		Name:   name,
		Index:  index,
		Value:  value,
		Array:  array,
	}
}

func (c *converter) assigns(xs []*syntax.Assign) []*assignNode {
	out := make([]*assignNode, 0, len(xs))
	for _, x := range xs {
		out = append(out, c.assign(x))
	}
	return out
}

type arrayNode struct {
	Kind             string           `json:"kind"`
	Pos              *posSpan         `json:"pos,omitempty"`
	Elems            []*arrayElemNode `json:"elems"`
	TrailingComments []*commentNode   `json:"trailing_comments"`
}

func (c *converter) array(x *syntax.ArrayExpr) *arrayNode {
	elems := make([]*arrayElemNode, 0, len(x.Elems))
	for _, e := range x.Elems {
		elems = append(elems, c.arrayElem(e))
	}

	return &arrayNode{
		Kind:             "array",
		Pos:              c.posOf(x),
		Elems:            elems,
		TrailingComments: c.comments(x.Last),
	}
}

type arrayElemNode struct {
	Kind     string         `json:"kind"`
	Pos      *posSpan       `json:"pos,omitempty"`
	Index    any            `json:"index,omitempty"`
	Value    *wordNode      `json:"value,omitempty"`
	Comments []*commentNode `json:"comments"`
}

func (c *converter) arrayElem(x *syntax.ArrayElem) *arrayElemNode {
	var index any
	if x.Index != nil {
		index = c.arithmExpr(x.Index)
	}

	var value *wordNode
	if x.Value != nil {
		value = c.word(x.Value)
	}

	return &arrayElemNode{
		Kind:     "array_elem",
		Pos:      c.posOf(x),
		Index:    index,
		Value:    value,
		Comments: c.comments(x.Comments),
	}
}

type redirectNode struct {
	Kind    string       `json:"kind"`
	Pos     *posSpan     `json:"pos,omitempty"`
	Op      string       `json:"op"`
	Fd      *literalNode `json:"fd,omitempty"`
	Word    *wordNode    `json:"word,omitempty"`
	Heredoc *wordNode    `json:"heredoc,omitempty"`
}

func (c *converter) redirect(x *syntax.Redirect) *redirectNode {
	var fd *literalNode
	if x.N != nil {
		fd = c.literal(x.N)
	}

	var word *wordNode
	if x.Word != nil {
		word = c.word(x.Word)
	}

	var heredoc *wordNode
	if x.Hdoc != nil {
		heredoc = c.word(x.Hdoc)
	}

	return &redirectNode{
		Kind:    "redirect",
		Pos:     c.posOf(x),
		Op:      x.Op.String(),
		Fd:      fd,
		Word:    word,
		Heredoc: heredoc,
	}
}

func (c *converter) redirects(xs []*syntax.Redirect) []*redirectNode {
	out := make([]*redirectNode, 0, len(xs))
	for _, x := range xs {
		out = append(out, c.redirect(x))
	}
	return out
}

type commentNode struct {
	Kind string   `json:"kind"`
	Pos  *posSpan `json:"pos,omitempty"`
	Text string   `json:"text"`
}

func (c *converter) comment(x syntax.Comment) *commentNode {
	return &commentNode{Kind: "comment", Pos: c.posOf(&x), Text: x.Text}
}

func (c *converter) comments(xs []syntax.Comment) []*commentNode {
	out := make([]*commentNode, 0, len(xs))
	for i := range xs {
		out = append(out, c.comment(xs[i]))
	}
	return out
}
