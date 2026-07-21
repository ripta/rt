package shast

import "mvdan.cc/sh/v3/syntax"

type flatCommandNode struct {
	Kind       string          `json:"kind"`
	Pos        *posSpan        `json:"pos,omitempty"`
	Assigns    []*assignNode   `json:"assigns"`
	Args       []*wordNode     `json:"args"`
	Negated    bool            `json:"negated,omitempty"`
	Background bool            `json:"background,omitempty"`
	Coprocess  bool            `json:"coprocess,omitempty"`
	Disown     bool            `json:"disown,omitempty"`
	Redirects  []*redirectNode `json:"redirects"`
}

// flatCommands walks f for every *syntax.Stmt whose Cmd is a *syntax.CallExpr
// - a simple command - in AST walk order, including ones reached only through
// a nested command or process substitution (syntax.Walk recurses into those
// regardless of what the callback returns). Each becomes one flatCommandNode
// merging that statement's own modifiers with the call's assigns/args, so a
// jq consumer can walk every command in a script without recursing into the
// nested tree themselves. Kind "command" is deliberately distinct from nested
// mode's "call", since the field sets differ.
func (c *converter) flatCommands(f *syntax.File) []*flatCommandNode {
	out := make([]*flatCommandNode, 0)

	syntax.Walk(f, func(n syntax.Node) bool {
		s, ok := n.(*syntax.Stmt)
		if !ok {
			return true
		}
		call, ok := s.Cmd.(*syntax.CallExpr)
		if !ok {
			return true
		}

		out = append(out, &flatCommandNode{
			Kind:       "command",
			Pos:        c.posOf(s),
			Assigns:    c.assigns(call.Assigns),
			Args:       c.words(call.Args),
			Negated:    s.Negated,
			Background: s.Background,
			Coprocess:  s.Coprocess,
			Disown:     s.Disown,
			Redirects:  c.redirects(s.Redirs),
		})
		return true
	})

	return out
}
