package shast

import "mvdan.cc/sh/v3/syntax"

// testExpr converts any of the four TestExpr implementors, appearing inside
// a Bash [[ ]] extended test clause. Word doubles as a leaf here too.
func (c *converter) testExpr(x syntax.TestExpr) any {
	if x == nil {
		return nil
	}

	switch v := x.(type) {
	case *syntax.BinaryTest:
		return c.binaryTest(v)
	case *syntax.UnaryTest:
		return c.unaryTest(v)
	case *syntax.ParenTest:
		return c.parenTest(v)
	case *syntax.Word:
		return c.word(v)
	default:
		return c.unsupported(v)
	}
}

type testBinaryNode struct {
	Kind string   `json:"kind"`
	Pos  *posSpan `json:"pos,omitempty"`
	Op   string   `json:"op"`
	X    any      `json:"x"`
	Y    any      `json:"y"`
}

func (c *converter) binaryTest(x *syntax.BinaryTest) *testBinaryNode {
	return &testBinaryNode{
		Kind: "test_binary",
		Pos:  c.posOf(x),
		Op:   x.Op.String(),
		X:    c.testExpr(x.X),
		Y:    c.testExpr(x.Y),
	}
}

type testUnaryNode struct {
	Kind string   `json:"kind"`
	Pos  *posSpan `json:"pos,omitempty"`
	Op   string   `json:"op"`
	X    any      `json:"x"`
}

func (c *converter) unaryTest(x *syntax.UnaryTest) *testUnaryNode {
	return &testUnaryNode{
		Kind: "test_unary",
		Pos:  c.posOf(x),
		Op:   x.Op.String(),
		X:    c.testExpr(x.X),
	}
}

type testParenNode struct {
	Kind string   `json:"kind"`
	Pos  *posSpan `json:"pos,omitempty"`
	X    any      `json:"x"`
}

func (c *converter) parenTest(x *syntax.ParenTest) *testParenNode {
	return &testParenNode{Kind: "test_paren", Pos: c.posOf(x), X: c.testExpr(x.X)}
}
