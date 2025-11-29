package parser

import (
	"fmt"
	"math/big"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/rational"
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
	precision int
	vars      map[string]*binding
}

// NewEnv creates a new environment with default precision (-100).
func NewEnv() *Env {
	return &Env{
		precision: -100,
		vars:      seedConstants(),
	}
}

// convertDecimalPlacesToPrecision computes the binary precision needed to
// represent the specified number of decimal places.
func convertDecimalPlacesToPrecision(decimalPlaces int) int {
	if decimalPlaces <= 0 {
		return 0
	}

	// log2(10) ~ 3.32193
	return -(decimalPlaces*332193 + 99999) / 100000
}

// NewEnvWithDecimalPlaces creates a new environment with the specified number
// of decimal places of precision.
func NewEnvWithDecimalPlaces(decimalPlaces int) *Env {
	return NewEnvWithPrecision(convertDecimalPlacesToPrecision(decimalPlaces))
}

// NewEnvWithPrecision creates a new environment with the specified binary
// precision.
func NewEnvWithPrecision(precision int) *Env {
	return &Env{
		precision: precision,
		vars:      seedConstants(),
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

	case tokens.OP_POW:
		return power(l, r, env.precision)

	case tokens.OP_PERCENT:
		if r.IsZero() {
			return nil, fmt.Errorf("modulo by zero")
		}
		return modulo(l, r, env.precision)

	case tokens.OP_SHL:
		shiftCount, err := extractInteger(r, n.Op, env.precision)
		if err != nil {
			return nil, err
		}
		return l.ShiftLeft(shiftCount), nil

	case tokens.OP_SHR:
		shiftCount, err := extractInteger(r, n.Op, env.precision)
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
func modulo(a, b *unified.Real, precision int) (*unified.Real, error) {
	// scale = 2^(-precision)
	scale := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(-precision)), nil)

	// Approximate a
	aApproxInt := constructive.Approximate(a.Constructive(), precision)
	if aApproxInt == nil {
		return nil, fmt.Errorf("failed to approximate dividend for modulo")
	}
	aApproxRat := new(big.Rat).SetFrac(aApproxInt, scale)

	// Approximate b
	bApproxInt := constructive.Approximate(b.Constructive(), precision)
	if bApproxInt == nil {
		return nil, fmt.Errorf("failed to approximate divisor for modulo")
	}
	bApproxRat := new(big.Rat).SetFrac(bApproxInt, scale)

	// Compute rational quotient a/b
	quotientRat := new(big.Rat).Quo(aApproxRat, bApproxRat)

	// Floor the quotient, i.e. (num / denom) with truncation
	floor := new(big.Int).Quo(quotientRat.Num(), quotientRat.Denom())

	// For negative quotients with a remainder, subtract 1 to get floor
	remainder := new(big.Int).Rem(quotientRat.Num(), quotientRat.Denom())
	if quotientRat.Sign() < 0 && remainder.Sign() != 0 {
		floor.Sub(floor, big.NewInt(1))
	}

	// Convert floor back to unified.Real
	floorRat := new(big.Rat).SetInt(floor)
	floorReal := unified.New(constructive.One(), rational.FromRational(floorRat))

	// Compute actual modulo: a - b * floor
	return a.Subtract(b.Multiply(floorReal)), nil
}

// extractInteger validates that a Real number is an integer and extracts it as an int.
// Returns an error if the number is not an integer or is out of range.
func extractInteger(r *unified.Real, op tokens.Token, precision int) (int, error) {
	// scale = 2^(-precision)
	scale := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(-precision)), nil)

	// Approximate r
	approxInt := constructive.Approximate(r.Constructive(), precision)
	if approxInt == nil {
		return 0, fmt.Errorf("%s: failed to approximate shift count", op.Pos)
	}

	// Check if denominator is 1 (i.e., it's an integer)
	approxRat := new(big.Rat).SetFrac(approxInt, scale)
	if approxRat.Denom().Cmp(big.NewInt(1)) != 0 {
		return 0, fmt.Errorf("%s: shift count must be an integer, got non-integer value", op.Pos)
	}

	// Convert to int, checking for overflow
	num := approxRat.Num()
	if !num.IsInt64() {
		return 0, fmt.Errorf("%s: shift count out of range", op.Pos)
	}

	return int(num.Int64()), nil
}

func power(l, r *unified.Real, precision int) (*unified.Real, error) {
	// Approximate both operands to check for special cases
	scale := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(-precision)), nil)

	// Approximate left (base
	lApprox := constructive.Approximate(l.Constructive(), precision)
	if lApprox == nil {
		return nil, fmt.Errorf("failed to approximate base")
	}
	lRat := new(big.Rat).SetFrac(lApprox, scale)

	// Approximate right (exponent)
	rApprox := constructive.Approximate(r.Constructive(), precision)
	if rApprox == nil {
		return nil, fmt.Errorf("failed to approximate exponent")
	}
	rRat := new(big.Rat).SetFrac(rApprox, scale)

	// Case 1: 0^exponent
	// When exponent is negative, the result is undefined.
	// When exponent is zero or positive, the result is zero.
	if l.IsZero() {
		if rRat.Sign() < 0 {
			return nil, fmt.Errorf("zero to negative power is undefined")
		}
		return unified.Zero(), nil
	}

	// Case 2: negative^non-integer = complex (non-real)
	// constructive.Pow uses logarithms internally, so it can't handle negative bases at all
	// We must handle negative bases specially
	if lRat.Sign() < 0 {
		// Base is negative, check if exponent is an integer
		if rRat.Denom().Cmp(big.NewInt(1)) != 0 {
			return nil, fmt.Errorf("negative base to non-integer power is non-real")
		}

		// For integer exponents, compute using big.Rat since we know n is an integer
		result := new(big.Rat).SetInt64(1)
		base := new(big.Rat).Set(lRat)
		exp := rRat.Num() // We know denom is 1

		// Handle negative exponents
		if exp.Sign() < 0 {
			base.Inv(base)
			exp = new(big.Int).Neg(exp)
		}

		// Compute base^exp using repeated multiplication
		for i := new(big.Int).Set(exp); i.Sign() > 0; i.Sub(i, big.NewInt(1)) {
			result.Mul(result, base)
		}

		return unified.New(constructive.One(), rational.FromRational(result)), nil
	}

	// Positive base: check if we can compute exactly using rationals
	// If both base and exponent are rational and exponent is a positive integer, use rational arithmetic
	if rRat.Denom().Cmp(big.NewInt(1)) == 0 && rRat.Sign() >= 0 {
		// Exponent is a non-negative integer
		// Check if base is also rational (or can be approximated as such)
		// For now, let's compute using big.Rat for integer exponents
		result := new(big.Rat).SetInt64(1)
		base := new(big.Rat).Set(lRat)
		exp := rRat.Num()

		// Compute base^exp using repeated multiplication
		for i := new(big.Int).Set(exp); i.Sign() > 0; i.Sub(i, big.NewInt(1)) {
			result.Mul(result, base)
		}

		return unified.New(constructive.One(), rational.FromRational(result)), nil
	}

	// Exponent is negative integer or non-integer: use constructive.Pow
	// This handles fractional powers, irrational powers, and negative powers
	cr := constructive.Pow(l.Constructive(), r.Constructive())
	return unified.New(cr, rational.One()), nil
}
