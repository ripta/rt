package parser

import "github.com/ripta/rt/pkg/calc/tokens"

// walkFree calls visit on n and on every node below it where name is not rebound. It stops at the
// first error visit returns.
//
// A binder whose binding rebinds name shadows it in its body. Its bounds are evaluated in the
// scope around the binder, so they still see name.
func walkFree(n Node, name string, visit func(Node) error) error {
	if err := visit(n); err != nil {
		return err
	}

	if call, ok := n.(*CallNode); ok && len(call.Args) > 0 {
		if b, ok := call.Args[len(call.Args)-1].(*BindingNode); ok && b.Name.Value == name {
			for _, c := range children(b) {
				if err := walkFree(c, name, visit); err != nil {
					return err
				}
			}
			return nil
		}
	}

	for _, c := range children(n) {
		if err := walkFree(c, name, visit); err != nil {
			return err
		}
	}
	return nil
}

// freeRef returns the first reference to name within n that the name is not rebound around.
func freeRef(n Node, name string) *IdentNode {
	var found *IdentNode
	_ = walkFree(n, name, func(n Node) error {
		if id, ok := n.(*IdentNode); ok && found == nil && id.Name.Value == name {
			found = id
		}
		return nil
	})
	return found
}

// checkIntegerOnly rejects a reference to name, the variable of a diff, in a position that takes
// only integers. Those are the arguments of the integer-only catalog entries and the count of a
// shift. Every enclosing diff reaches this through its body, so an inner diff body is covered.
func (p *P) checkIntegerOnly(body Node, name string) error {
	return walkFree(body, name, func(n Node) error {
		var guarded []Node
		var what string

		switch n := n.(type) {
		case *CallNode:
			fn, ok := p.catalog[n.Func.Value]
			if !ok || !fn.integerArgs {
				return nil
			}
			guarded, what = n.Args, "the arguments of "+n.Func.Value

		case *BinaryNode:
			if n.Op.Type != tokens.OP_SHL && n.Op.Type != tokens.OP_SHR {
				return nil
			}
			guarded, what = []Node{n.Right}, "the shift count of "+n.Op.Value

		default:
			return nil
		}

		for _, g := range guarded {
			if id := freeRef(g, name); id != nil {
				return p.errorf(id.Name, "%s is the variable of diff, so it cannot appear in %s, which takes only integers", name, what)
			}
		}
		return nil
	})
}

// checkIntegerBounds rejects a reference to name, the variable of a diff, in a bound of a sum or
// product. Integer bounds make the result defined only on the integers. A bound is evaluated in
// the scope around its binder, so a binder that rebinds name does not hide it.
func (p *P) checkIntegerBounds(body Node, name string) error {
	return walkFree(body, name, func(n Node) error {
		call, ok := n.(*CallNode)
		if !ok || len(call.Args) == 0 {
			return nil
		}
		fn, ok := p.catalog[call.Func.Value]
		if !ok || fn.binder == nil || !fn.binder.integerBounds {
			return nil
		}
		b, ok := call.Args[len(call.Args)-1].(*BindingNode)
		if !ok {
			return nil
		}

		for _, bound := range []Node{b.Lo, b.Hi} {
			if bound == nil {
				continue
			}
			if id := freeRef(bound, name); id != nil {
				return p.errorf(id.Name, "%s is the variable of diff, so it cannot appear in a bound of %s, which takes only integers", name, call.Func.Value)
			}
		}
		return nil
	})
}
