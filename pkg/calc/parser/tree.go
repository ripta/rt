package parser

import (
	"fmt"

	"github.com/ripta/rt/pkg/calc/tokens"
)

type Node interface {
	Eval(*Env) (float64, error)
}

type Env struct {
	vars map[string]float64
}

func NewEnv() *Env {
	return &Env{
		vars: map[string]float64{},
	}
}

func (e *Env) Get(name string) (float64, bool) {
	val, ok := e.vars[name]
	return val, ok
}

func (e *Env) Set(name string, val float64) {
	e.vars[name] = val
}

type NumberNode struct {
	Value float64
}

func (n *NumberNode) Eval(_ *Env) (float64, error) {
	return n.Value, nil
}

type BinaryNode struct {
	Op    tokens.Token
	Left  Node
	Right Node
}

func (n *BinaryNode) Eval(env *Env) (float64, error) {
	l, err := n.Left.Eval(env)
	if err != nil {
		return 0, err
	}

	r, err := n.Right.Eval(env)
	if err != nil {
		return 0, err
	}

	switch n.Op.Type {
	case tokens.OP_PLUS:
		return l + r, nil

	case tokens.OP_MINUS:
		return l - r, nil

	case tokens.OP_STAR:
		return l * r, nil

	case tokens.OP_SLASH:
		if r == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return l / r, nil

	default:
		return 0, fmt.Errorf("unknown operator")
	}
}

type UnaryNode struct {
	Op   tokens.Token
	Expr Node
}

func (n *UnaryNode) Eval(env *Env) (float64, error) {
	val, err := n.Expr.Eval(env)
	if err != nil {
		return 0, err
	}

	switch n.Op.Type {
	case tokens.OP_MINUS:
		return -val, nil

	default:
		return 0, fmt.Errorf("unknown unary operator")
	}
}

type IdentNode struct {
	Name tokens.Token
}

func (n *IdentNode) Eval(env *Env) (float64, error) {
	if env == nil {
		return 0, fmt.Errorf("%s: undefined identifier %q", n.Name.Pos, n.Name.Value)
	}

	if val, ok := env.Get(n.Name.Value); ok {
		return val, nil
	}

	return 0, fmt.Errorf("%s: undefined identifier %q", n.Name.Pos, n.Name.Value)
}

type AssignNode struct {
	Name  tokens.Token
	Value Node
}

func (n *AssignNode) Eval(env *Env) (float64, error) {
	if env == nil {
		env = NewEnv()
	}

	val, err := n.Value.Eval(env)
	if err != nil {
		return 0, err
	}

	env.Set(n.Name.Value, val)
	return val, nil
}
