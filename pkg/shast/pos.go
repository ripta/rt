package shast

import "mvdan.cc/sh/v3/syntax"

// posPoint is a single source location within a parsed script.
type posPoint struct {
	Offset uint `json:"offset"`
	Line   uint `json:"line"`
	Col    uint `json:"col"`
}

// posSpan is the --pos payload attached to a node: where it starts and ends.
type posSpan struct {
	Start posPoint `json:"start"`
	End   posPoint `json:"end"`
}

func newPosSpan(start, end syntax.Pos) *posSpan {
	return &posSpan{
		Start: posPoint{Offset: start.Offset(), Line: start.Line(), Col: start.Col()},
		End:   posPoint{Offset: end.Offset(), Line: end.Line(), Col: end.Col()},
	}
}

// posOf returns n's source span, or nil when position info wasn't requested.
func (c *converter) posOf(n syntax.Node) *posSpan {
	if !c.withPos {
		return nil
	}
	return newPosSpan(n.Pos(), n.End())
}
