package shast

import "mvdan.cc/sh/v3/syntax"

type wordNode struct {
	Kind  string   `json:"kind"`
	Pos   *posSpan `json:"pos,omitempty"`
	Text  string   `json:"text"`
	Parts []any    `json:"parts"`
}

// word converts a single shell word. Text is a verbatim slice of the source
// spanning the word, independent of Parts, so a jq consumer can read plain
// argv-like strings via .text without walking .parts and switching on kind.
//
// Brace expansions like {a,b} or {1..10} are deliberately left as opaque
// literal text rather than run through syntax.SplitBraces: that function
// copies a literal's original ValuePos/ValueEnd onto each split-off piece
// without adjusting them for the substring, so a converted brace_expansion
// element's Text would span the whole original literal instead of its own
// piece, silently breaking Text's verbatim-source guarantee. brace_expansion
// stays a defined node kind for schema completeness; it just isn't reachable
// from default parsing, matching syntax.Parse's own behavior.
func (c *converter) word(w *syntax.Word) *wordNode {
	if w == nil {
		return nil
	}

	return &wordNode{
		Kind:  "word",
		Pos:   c.posOf(w),
		Text:  c.sliceSrc(w.Pos(), w.End()),
		Parts: c.wordParts(w.Parts),
	}
}

func (c *converter) words(ws []*syntax.Word) []*wordNode {
	out := make([]*wordNode, 0, len(ws))
	for _, w := range ws {
		out = append(out, c.word(w))
	}
	return out
}

func (c *converter) wordParts(parts []syntax.WordPart) []any {
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		out = append(out, c.wordPart(p))
	}
	return out
}

// wordPart converts any of the nine WordPart implementors.
func (c *converter) wordPart(p syntax.WordPart) any {
	if p == nil {
		return nil
	}

	switch x := p.(type) {
	case *syntax.Lit:
		return c.literal(x)
	case *syntax.SglQuoted:
		return c.sglQuoted(x)
	case *syntax.DblQuoted:
		return c.dblQuoted(x)
	case *syntax.ParamExp:
		return c.paramExp(x)
	case *syntax.CmdSubst:
		return c.cmdSubst(x)
	case *syntax.ArithmExp:
		return c.arithmExp(x)
	case *syntax.ProcSubst:
		return c.procSubst(x)
	case *syntax.ExtGlob:
		return c.extGlob(x)
	case *syntax.BraceExp:
		return c.braceExp(x)
	default:
		return c.unsupported(x)
	}
}

type literalNode struct {
	Kind  string   `json:"kind"`
	Pos   *posSpan `json:"pos,omitempty"`
	Value string   `json:"value"`
}

func (c *converter) literal(x *syntax.Lit) *literalNode {
	if x == nil {
		return nil
	}
	return &literalNode{Kind: "literal", Pos: c.posOf(x), Value: x.Value}
}

func (c *converter) literals(xs []*syntax.Lit) []*literalNode {
	out := make([]*literalNode, 0, len(xs))
	for _, x := range xs {
		out = append(out, c.literal(x))
	}
	return out
}

type singleQuotedNode struct {
	Kind   string   `json:"kind"`
	Pos    *posSpan `json:"pos,omitempty"`
	Dollar bool     `json:"dollar,omitempty"`
	Value  string   `json:"value"`
}

func (c *converter) sglQuoted(x *syntax.SglQuoted) *singleQuotedNode {
	return &singleQuotedNode{
		Kind:   "single_quoted",
		Pos:    c.posOf(x),
		Dollar: x.Dollar,
		Value:  x.Value,
	}
}

type doubleQuotedNode struct {
	Kind   string   `json:"kind"`
	Pos    *posSpan `json:"pos,omitempty"`
	Dollar bool     `json:"dollar,omitempty"`
	Parts  []any    `json:"parts"`
}

func (c *converter) dblQuoted(x *syntax.DblQuoted) *doubleQuotedNode {
	return &doubleQuotedNode{
		Kind:   "double_quoted",
		Pos:    c.posOf(x),
		Dollar: x.Dollar,
		Parts:  c.wordParts(x.Parts),
	}
}

type paramExpansionNode struct {
	Kind        string              `json:"kind"`
	Pos         *posSpan            `json:"pos,omitempty"`
	Short       bool                `json:"short,omitempty"`
	Excl        bool                `json:"excl,omitempty"`
	Length      bool                `json:"length,omitempty"`
	Width       bool                `json:"width,omitempty"`
	IsSet       bool                `json:"is_set,omitempty"`
	Flags       *literalNode        `json:"flags,omitempty"`
	Param       *literalNode        `json:"param,omitempty"`
	NestedParam any                 `json:"nested_param,omitempty"`
	Index       any                 `json:"index,omitempty"`
	Modifiers   []*literalNode      `json:"modifiers"`
	Slice       *paramSliceNode     `json:"slice,omitempty"`
	Replace     *paramReplaceNode   `json:"replace,omitempty"`
	NamesOp     string              `json:"names_op,omitempty"`
	Expansion   *paramOperationNode `json:"expansion,omitempty"`
}

func (c *converter) paramExp(x *syntax.ParamExp) *paramExpansionNode {
	var flags, param *literalNode
	if x.Flags != nil {
		flags = c.literal(x.Flags)
	}
	if x.Param != nil {
		param = c.literal(x.Param)
	}

	var nestedParam any
	if x.NestedParam != nil {
		nestedParam = c.wordPart(x.NestedParam)
	}

	var index any
	if x.Index != nil {
		index = c.arithmExpr(x.Index)
	}

	var slice *paramSliceNode
	if x.Slice != nil {
		slice = c.paramSlice(x.Slice)
	}

	var replace *paramReplaceNode
	if x.Repl != nil {
		replace = c.paramReplace(x.Repl)
	}

	namesOp := ""
	if x.Names != 0 {
		namesOp = x.Names.String()
	}

	var expansion *paramOperationNode
	if x.Exp != nil {
		expansion = c.paramOperation(x.Exp)
	}

	return &paramExpansionNode{
		Kind:        "param_expansion",
		Pos:         c.posOf(x),
		Short:       x.Short,
		Excl:        x.Excl,
		Length:      x.Length,
		Width:       x.Width,
		IsSet:       x.IsSet,
		Flags:       flags,
		Param:       param,
		NestedParam: nestedParam,
		Index:       index,
		Modifiers:   c.literals(x.Modifiers),
		Slice:       slice,
		Replace:     replace,
		NamesOp:     namesOp,
		Expansion:   expansion,
	}
}

// paramSliceNode, paramReplaceNode, and paramOperationNode carry no pos: the
// underlying syntax.Slice, syntax.Replace, and syntax.Expansion types don't
// implement syntax.Node (no Pos()/End()), so there's no source span to report.

type paramSliceNode struct {
	Kind   string `json:"kind"`
	Offset any    `json:"offset,omitempty"`
	Length any    `json:"length,omitempty"`
}

func (c *converter) paramSlice(x *syntax.Slice) *paramSliceNode {
	var offset, length any
	if x.Offset != nil {
		offset = c.arithmExpr(x.Offset)
	}
	if x.Length != nil {
		length = c.arithmExpr(x.Length)
	}

	return &paramSliceNode{Kind: "param_slice", Offset: offset, Length: length}
}

type paramReplaceNode struct {
	Kind string    `json:"kind"`
	All  bool      `json:"all,omitempty"`
	Orig *wordNode `json:"orig,omitempty"`
	With *wordNode `json:"with,omitempty"`
}

func (c *converter) paramReplace(x *syntax.Replace) *paramReplaceNode {
	var orig, with *wordNode
	if x.Orig != nil {
		orig = c.word(x.Orig)
	}
	if x.With != nil {
		with = c.word(x.With)
	}

	return &paramReplaceNode{Kind: "param_replace", All: x.All, Orig: orig, With: with}
}

type paramOperationNode struct {
	Kind string    `json:"kind"`
	Op   string    `json:"op"`
	Word *wordNode `json:"word"`
}

func (c *converter) paramOperation(x *syntax.Expansion) *paramOperationNode {
	return &paramOperationNode{Kind: "param_operation", Op: x.Op.String(), Word: c.word(x.Word)}
}

type commandSubstitutionNode struct {
	Kind             string         `json:"kind"`
	Pos              *posSpan       `json:"pos,omitempty"`
	Statements       []*stmtNode    `json:"statements"`
	TrailingComments []*commentNode `json:"trailing_comments"`
	Backquotes       bool           `json:"backquotes,omitempty"`
	TempFile         bool           `json:"temp_file,omitempty"`
	ReplyVar         bool           `json:"reply_var,omitempty"`
}

func (c *converter) cmdSubst(x *syntax.CmdSubst) *commandSubstitutionNode {
	return &commandSubstitutionNode{
		Kind:             "command_substitution",
		Pos:              c.posOf(x),
		Statements:       c.stmts(x.Stmts),
		TrailingComments: c.comments(x.Last),
		Backquotes:       x.Backquotes,
		TempFile:         x.TempFile,
		ReplyVar:         x.ReplyVar,
	}
}

type arithmeticExpansionNode struct {
	Kind     string   `json:"kind"`
	Pos      *posSpan `json:"pos,omitempty"`
	Bracket  bool     `json:"bracket,omitempty"`
	Unsigned bool     `json:"unsigned,omitempty"`
	Expr     any      `json:"expr"`
}

func (c *converter) arithmExp(x *syntax.ArithmExp) *arithmeticExpansionNode {
	return &arithmeticExpansionNode{
		Kind:     "arithmetic_expansion",
		Pos:      c.posOf(x),
		Bracket:  x.Bracket,
		Unsigned: x.Unsigned,
		Expr:     c.arithmExpr(x.X),
	}
}

type processSubstitutionNode struct {
	Kind             string         `json:"kind"`
	Pos              *posSpan       `json:"pos,omitempty"`
	Op               string         `json:"op"`
	Statements       []*stmtNode    `json:"statements"`
	TrailingComments []*commentNode `json:"trailing_comments"`
}

func (c *converter) procSubst(x *syntax.ProcSubst) *processSubstitutionNode {
	return &processSubstitutionNode{
		Kind:             "process_substitution",
		Pos:              c.posOf(x),
		Op:               x.Op.String(),
		Statements:       c.stmts(x.Stmts),
		TrailingComments: c.comments(x.Last),
	}
}

type extendedGlobNode struct {
	Kind    string       `json:"kind"`
	Pos     *posSpan     `json:"pos,omitempty"`
	Op      string       `json:"op"`
	Pattern *literalNode `json:"pattern"`
}

func (c *converter) extGlob(x *syntax.ExtGlob) *extendedGlobNode {
	return &extendedGlobNode{
		Kind:    "extended_glob",
		Pos:     c.posOf(x),
		Op:      x.Op.String(),
		Pattern: c.literal(x.Pattern),
	}
}

type braceExpansionNode struct {
	Kind     string      `json:"kind"`
	Pos      *posSpan    `json:"pos,omitempty"`
	Sequence bool        `json:"sequence,omitempty"`
	Elems    []*wordNode `json:"elems"`
}

func (c *converter) braceExp(x *syntax.BraceExp) *braceExpansionNode {
	return &braceExpansionNode{
		Kind:     "brace_expansion",
		Pos:      c.posOf(x),
		Sequence: x.Sequence,
		Elems:    c.words(x.Elems),
	}
}
