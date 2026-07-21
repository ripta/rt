package shast

import "mvdan.cc/sh/v3/syntax"

// arithmExpr converts any of the five ArithmExpr implementors. Word doubles
// as a leaf here (e.g. a bare variable name in an arithmetic expression), and
// reuses the same word converter as everywhere else.
func (c *converter) arithmExpr(x syntax.ArithmExpr) any {
	if x == nil {
		return nil
	}

	switch v := x.(type) {
	case *syntax.BinaryArithm:
		return c.binaryArithm(v)
	case *syntax.UnaryArithm:
		return c.unaryArithm(v)
	case *syntax.ParenArithm:
		return c.parenArithm(v)
	case *syntax.FlagsArithm:
		return c.flagsArithm(v)
	case *syntax.Word:
		return c.word(v)
	default:
		return c.unsupported(v)
	}
}

type arithmeticBinaryNode struct {
	Kind string   `json:"kind"`
	Pos  *posSpan `json:"pos,omitempty"`
	Op   string   `json:"op"`
	X    any      `json:"x"`
	Y    any      `json:"y"`
}

func (c *converter) binaryArithm(x *syntax.BinaryArithm) *arithmeticBinaryNode {
	return &arithmeticBinaryNode{
		Kind: "arithmetic_binary",
		Pos:  c.posOf(x),
		Op:   x.Op.String(),
		X:    c.arithmExpr(x.X),
		Y:    c.arithmExpr(x.Y),
	}
}

type arithmeticUnaryNode struct {
	Kind string   `json:"kind"`
	Pos  *posSpan `json:"pos,omitempty"`
	Op   string   `json:"op"`
	Post bool     `json:"post,omitempty"`
	X    any      `json:"x"`
}

func (c *converter) unaryArithm(x *syntax.UnaryArithm) *arithmeticUnaryNode {
	return &arithmeticUnaryNode{
		Kind: "arithmetic_unary",
		Pos:  c.posOf(x),
		Op:   x.Op.String(),
		Post: x.Post,
		X:    c.arithmExpr(x.X),
	}
}

type arithmeticParenNode struct {
	Kind string   `json:"kind"`
	Pos  *posSpan `json:"pos,omitempty"`
	X    any      `json:"x"`
}

func (c *converter) parenArithm(x *syntax.ParenArithm) *arithmeticParenNode {
	return &arithmeticParenNode{Kind: "arithmetic_paren", Pos: c.posOf(x), X: c.arithmExpr(x.X)}
}

type arithmeticFlagsNode struct {
	Kind  string       `json:"kind"`
	Pos   *posSpan     `json:"pos,omitempty"`
	Flags *literalNode `json:"flags"`
	X     any          `json:"x"`
}

func (c *converter) flagsArithm(x *syntax.FlagsArithm) *arithmeticFlagsNode {
	return &arithmeticFlagsNode{
		Kind:  "arithmetic_flags",
		Pos:   c.posOf(x),
		Flags: c.literal(x.Flags),
		X:     c.arithmExpr(x.X),
	}
}

// loop converts either Loop implementor: WordIter (for x in ...) or
// CStyleLoop (for ((init; cond; post))).
func (c *converter) loop(l syntax.Loop) any {
	if l == nil {
		return nil
	}

	switch v := l.(type) {
	case *syntax.WordIter:
		return c.wordIter(v)
	case *syntax.CStyleLoop:
		return c.cStyleLoop(v)
	default:
		return c.unsupported(v)
	}
}

type wordIterNode struct {
	Kind  string       `json:"kind"`
	Pos   *posSpan     `json:"pos,omitempty"`
	Name  *literalNode `json:"name"`
	HasIn bool         `json:"has_in,omitempty"`
	Items []*wordNode  `json:"items"`
}

// wordIter's HasIn is structural, not positional: an absent "in" keyword
// means the loop iterates the shell's positional parameters rather than an
// explicit (possibly empty) word list, and that distinction matters even
// without --pos.
func (c *converter) wordIter(x *syntax.WordIter) *wordIterNode {
	return &wordIterNode{
		Kind:  "word_iter",
		Pos:   c.posOf(x),
		Name:  c.literal(x.Name),
		HasIn: x.InPos.IsValid(),
		Items: c.words(x.Items),
	}
}

type cStyleLoopNode struct {
	Kind string   `json:"kind"`
	Pos  *posSpan `json:"pos,omitempty"`
	Init any      `json:"init,omitempty"`
	Cond any      `json:"cond,omitempty"`
	Post any      `json:"post,omitempty"`
}

func (c *converter) cStyleLoop(x *syntax.CStyleLoop) *cStyleLoopNode {
	var init, cond, post any
	if x.Init != nil {
		init = c.arithmExpr(x.Init)
	}
	if x.Cond != nil {
		cond = c.arithmExpr(x.Cond)
	}
	if x.Post != nil {
		post = c.arithmExpr(x.Post)
	}

	return &cStyleLoopNode{Kind: "c_style_loop", Pos: c.posOf(x), Init: init, Cond: cond, Post: post}
}
