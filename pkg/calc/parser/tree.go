package parser

import (
	"fmt"

	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/tokens"
)

type Node interface {
	Eval(*Env) (*unified.Real, error)
}

type binding struct {
	value   *unified.Real
	mutable bool
}

type Env struct {
	vars map[string]*binding
}

func NewEnv() *Env {
	return &Env{
		vars: seedConstants(),
	}
}

var transcendentalConstants = map[string]func() *unified.Real{
	"E":     unified.E,
	"PI":    unified.Pi,
	"PHI":   unified.Phi,
	"SQRT2": unified.Sqrt2,
	"LN2":   unified.Ln2,
}

func seedConstants() map[string]*binding {
	vars := map[string]*binding{}
	for name, supplier := range transcendentalConstants {
		vars[name] = &binding{
			value:   supplier(),
			mutable: false,
		}
	}

	return vars
}

func (e *Env) Get(name string) (*unified.Real, bool) {
	if binding, ok := e.vars[name]; ok {
		return binding.value, true
	}
	return nil, false
}

func (e *Env) Set(name string, val *unified.Real) error {
	if binding, ok := e.vars[name]; ok && !binding.mutable {
		return fmt.Errorf("cannot assign to constant %q", name)
	}

	e.vars[name] = &binding{
		value:   val,
		mutable: true,
	}
	return nil
}

type NumberNode struct {
	Value *unified.Real
}

func (n *NumberNode) Eval(_ *Env) (*unified.Real, error) {
	return n.Value, nil
}

type BinaryNode struct {
	Op    tokens.Token
	Left  Node
	Right Node
}

func (n *BinaryNode) Eval(env *Env) (*unified.Real, error) {
	l, err := n.Left.Eval(env)
	if err != nil {
		return nil, err
	}

	r, err := n.Right.Eval(env)
	if err != nil {
		return nil, err
	}

	switch n.Op.Type {
	case tokens.OP_PLUS:
		return l.Add(r), nil

	case tokens.OP_MINUS:
		return l.Subtract(r), nil

	case tokens.OP_STAR:
		return l.Multiply(r), nil

	case tokens.OP_SLASH:
		if r.IsZero() {
			return nil, fmt.Errorf("division by zero")
		}
		return l.Divide(r), nil

	default:
		return nil, fmt.Errorf("unknown operator")
	}
}

type UnaryNode struct {
	Op   tokens.Token
	Expr Node
}

func (n *UnaryNode) Eval(env *Env) (*unified.Real, error) {
	val, err := n.Expr.Eval(env)
	if err != nil {
		return nil, err
	}

	switch n.Op.Type {
	case tokens.OP_MINUS:
		return val.Negate(), nil

	default:
		return nil, fmt.Errorf("unknown unary operator")
	}
}

type IdentNode struct {
	Name tokens.Token
}

func (n *IdentNode) Eval(env *Env) (*unified.Real, error) {
	if env == nil {
		return nil, fmt.Errorf("%s: undefined identifier %q", n.Name.Pos, n.Name.Value)
	}

	if val, ok := env.Get(n.Name.Value); ok {
		return val, nil
	}

	return nil, fmt.Errorf("%s: undefined identifier %q", n.Name.Pos, n.Name.Value)
}

type AssignNode struct {
	Name  tokens.Token
	Value Node
}

func (n *AssignNode) Eval(env *Env) (*unified.Real, error) {
	if env == nil {
		env = NewEnv()
	}

	val, err := n.Value.Eval(env)
	if err != nil {
		return nil, err
	}

	if err := env.Set(n.Name.Value, val); err != nil {
		return nil, fmt.Errorf("%s: %w", n.Name.Pos, err)
	}
	return val, nil
}
