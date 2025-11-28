package parser

import (
	"fmt"

	"github.com/ripta/rt/pkg/calc/tokens"
)

type Node interface {
	Eval() (float64, error)
}

type NumberNode struct {
	Value float64
}

func (n *NumberNode) Eval() (float64, error) {
	return n.Value, nil
}

type BinaryNode struct {
	Op    tokens.Token
	Left  Node
	Right Node
}

func (n *BinaryNode) Eval() (float64, error) {
	l, err := n.Left.Eval()
	if err != nil {
		return 0, err
	}
	r, err := n.Right.Eval()
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

func (n *UnaryNode) Eval() (float64, error) {
	val, err := n.Expr.Eval()
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
