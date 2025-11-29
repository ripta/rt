package parser

import (
	"fmt"
	"math/big"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/rational"
	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/tokens"
)

const precision = -100

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

	case tokens.OP_PERCENT:
		if r.IsZero() {
			return nil, fmt.Errorf("modulo by zero")
		}
		return modulo(l, r)

	case tokens.OP_SHL:
		shiftCount, err := extractInteger(r, n.Op)
		if err != nil {
			return nil, err
		}
		return l.ShiftLeft(shiftCount), nil

	case tokens.OP_SHR:
		shiftCount, err := extractInteger(r, n.Op)
		if err != nil {
			return nil, err
		}
		return l.ShiftRight(shiftCount), nil

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

	case tokens.OP_ROOT:
		cr := constructive.Sqrt(val.Constructive())
		return unified.New(cr, rational.One()), nil

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

// modulo computes a % b = a - b * floor(a/b) for real numbers
func modulo(a, b *unified.Real) (*unified.Real, error) {
	scale := new(big.Int).Exp(big.NewInt(2), big.NewInt(-precision), nil)

	aApproxInt := constructive.Approximate(a.Constructive(), precision)
	if aApproxInt == nil {
		return nil, fmt.Errorf("failed to approximate dividend for modulo")
	}
	aApproxRat := new(big.Rat).SetFrac(aApproxInt, scale)

	bApproxInt := constructive.Approximate(b.Constructive(), precision)
	if bApproxInt == nil {
		return nil, fmt.Errorf("failed to approximate divisor for modulo")
	}
	bApproxRat := new(big.Rat).SetFrac(bApproxInt, scale)

	quotientRat := new(big.Rat).Quo(aApproxRat, bApproxRat)

	floor := new(big.Int).Quo(quotientRat.Num(), quotientRat.Denom())

	remainder := new(big.Int).Rem(quotientRat.Num(), quotientRat.Denom())
	if quotientRat.Sign() < 0 && remainder.Sign() != 0 {
		floor.Sub(floor, big.NewInt(1))
	}

	floorRat := new(big.Rat).SetInt(floor)
	floorReal := unified.New(constructive.One(), rational.FromRational(floorRat))

	return a.Subtract(b.Multiply(floorReal)), nil
}

// extractInteger validates that a Real number is an integer and extracts it as an int.
// Returns an error if the number is not an integer or is out of range.
func extractInteger(r *unified.Real, op tokens.Token) (int, error) {
	scale := new(big.Int).Exp(big.NewInt(2), big.NewInt(-precision), nil)

	approxInt := constructive.Approximate(r.Constructive(), precision)
	if approxInt == nil {
		return 0, fmt.Errorf("%s: failed to approximate shift count", op.Pos)
	}

	approxRat := new(big.Rat).SetFrac(approxInt, scale)

	if approxRat.Denom().Cmp(big.NewInt(1)) != 0 {
		return 0, fmt.Errorf("%s: shift count must be an integer, got non-integer value", op.Pos)
	}

	num := approxRat.Num()
	if !num.IsInt64() {
		return 0, fmt.Errorf("%s: shift count out of range", op.Pos)
	}

	i64 := num.Int64()
	if i64 > int64(int(^uint(0)>>1)) || i64 < int64(-int(^uint(0)>>1)-1) {
		return 0, fmt.Errorf("%s: shift count out of range", op.Pos)
	}

	return int(i64), nil
}
