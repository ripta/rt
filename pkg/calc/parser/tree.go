package parser

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/rational"
	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/tokens"
)

type Node interface {
	Eval(context.Context, *Env) (*unified.Real, error)
}

type binding struct {
	value   *unified.Real
	mutable bool
}

type Env struct {
	parent    *Env
	precision int
	vars      map[string]*binding
	trace     bool
	traceOut  io.Writer
}

// NewEnv creates a new environment with default precision (-100).
func NewEnv() *Env {
	return &Env{
		precision: -100,
		vars:      seedConstants(),
		traceOut:  os.Stdout,
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

// transcendentalConstants is the one list of immutable constant names. NewEnv seeds
// from it and the parser refuses to bind these names.
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

// child returns a scope holding only name, bound to val. Lookups that miss fall through to e.
// Precision and tracing are copied from e, which is safe because nothing changes them during an
// evaluation.
func (e *Env) child(name string, val *unified.Real) *Env {
	return &Env{
		parent:    e,
		precision: e.precision,
		vars:      map[string]*binding{name: {value: val}},
		trace:     e.trace,
		traceOut:  e.traceOut,
	}
}

func (e *Env) Get(name string) (*unified.Real, bool) {
	for s := e; s != nil; s = s.parent {
		if binding, ok := s.vars[name]; ok {
			return binding.value, true
		}
	}
	return nil, false
}

// Set binds a mutable variable. Names starting with "$" are result history, which only
// SetConstant may write, so assigning to one fails whether or not it is defined yet.
func (e *Env) Set(name string, val *unified.Real) error {
	if strings.HasPrefix(name, "$") {
		return fmt.Errorf("cannot assign to constant %q", name)
	}

	if e.parent != nil {
		if _, ok := e.vars[name]; ok {
			return fmt.Errorf("cannot assign to constant %q", name)
		}
		return e.parent.Set(name, val)
	}

	if binding, ok := e.vars[name]; ok && !binding.mutable {
		return fmt.Errorf("cannot assign to constant %q", name)
	}

	e.vars[name] = &binding{
		value:   val,
		mutable: true,
	}
	return nil
}

func (e *Env) SetConstant(name string, val *unified.Real) {
	if e.parent != nil {
		e.parent.SetConstant(name, val)
		return
	}

	e.vars[name] = &binding{
		value:   val,
		mutable: false,
	}
}

// CopyResults copies every "$"-prefixed result binding from src into e. The copies stay immutable.
func (e *Env) CopyResults(src *Env) {
	for name, b := range src.vars {
		if strings.HasPrefix(name, "$") {
			e.SetConstant(name, b.value)
		}
	}
}

func (e *Env) SetDecimalPlaces(decimalPlaces int) {
	e.precision = convertDecimalPlacesToPrecision(decimalPlaces)
}

func (e *Env) SetPrecision(precision int) {
	e.precision = precision
}

func (e *Env) SetTrace(enabled bool) {
	e.trace = enabled
}

func (e *Env) SetTraceOutput(w io.Writer) {
	e.traceOut = w
}

func extractCommentText(tok tokens.Token) string {
	val := tok.Value
	if len(val) >= 2 {
		// Remove surrounding quotes: "text" → text, `text` → text
		if (val[0] == '"' && val[len(val)-1] == '"') ||
			(val[0] == '`' && val[len(val)-1] == '`') {
			return val[1 : len(val)-1]
		}
	}
	return val
}

type NumberNode struct {
	Value *unified.Real
}

func (n *NumberNode) Eval(_ context.Context, _ *Env) (*unified.Real, error) {
	return n.Value, nil
}

type BinaryNode struct {
	Op    tokens.Token
	Left  Node
	Right Node
}

func (n *BinaryNode) Eval(ctx context.Context, env *Env) (*unified.Real, error) {
	l, err := n.Left.Eval(ctx, env)
	if err != nil {
		return nil, err
	}

	r, err := n.Right.Eval(ctx, env)
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
		return power(ctx, l, r, env.precision)

	case tokens.OP_PERCENT:
		if r.IsZero() {
			return nil, fmt.Errorf("modulo by zero")
		}
		return modulo(ctx, l, r, env.precision)

	case tokens.OP_SHL:
		shiftCount, err := extractInteger(ctx, r, n.Op, env.precision)
		if err != nil {
			return nil, err
		}
		return l.ShiftLeft(shiftCount), nil

	case tokens.OP_SHR:
		shiftCount, err := extractInteger(ctx, r, n.Op, env.precision)
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

func (n *UnaryNode) Eval(ctx context.Context, env *Env) (*unified.Real, error) {
	val, err := n.Expr.Eval(ctx, env)
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

func (n *IdentNode) Eval(ctx context.Context, env *Env) (*unified.Real, error) {
	if env == nil {
		return nil, fmt.Errorf("%s: undefined identifier %q", n.Name.Pos, n.Name.Value)
	}

	if val, ok := env.Get(n.Name.Value); ok {
		return val, nil
	}

	if strings.HasPrefix(n.Name.Value, "$") {
		return nil, fmt.Errorf("%s: no result for line %s", n.Name.Pos, n.Name.Value[1:])
	}

	return nil, fmt.Errorf("%s: undefined identifier %q", n.Name.Pos, n.Name.Value)
}

type CallNode struct {
	Func tokens.Token
	Args []Node

	// catalog is the table the parser checked this call against. Nil means the shipping functions.
	catalog map[string]function
}

func (n *CallNode) Eval(ctx context.Context, env *Env) (*unified.Real, error) {
	// The parser has already checked the name and arity. Only a hand-built tree can fail here.
	table := n.catalog
	if table == nil {
		table = functions
	}

	fn, ok := table[n.Func.Value]
	if !ok {
		return nil, fmt.Errorf("%s: unknown function %q", n.Func.Pos, n.Func.Value)
	}

	if fn.binder != nil {
		return n.evalBinder(ctx, env, fn)
	}

	args := make([]*unified.Real, len(n.Args))
	for i, a := range n.Args {
		val, err := a.Eval(ctx, env)
		if err != nil {
			return nil, err
		}
		args[i] = val
	}

	result, err := fn.fn(ctx, env, args)
	if err != nil {
		if isStop(err) {
			return nil, err
		}
		return nil, fmt.Errorf("%s: %w", n.Func.Pos, domainError(ctx, n.Func.Value, args, env.precision, err))
	}
	return result, nil
}

func (n *CallNode) evalBinder(ctx context.Context, env *Env, fn function) (*unified.Real, error) {
	last := len(n.Args) - 1
	var b *BindingNode
	if last >= 0 {
		b, _ = n.Args[last].(*BindingNode)
	}
	if b == nil || fn.binder.eval == nil {
		return nil, fmt.Errorf("%s: binder %s cannot be evaluated", n.Func.Pos, n.Func.Value)
	}

	// Bounds are ordinary expressions in the caller's scope, so nested binders see the outer binding.
	bind, err := evalBinding(ctx, env, b)
	if err != nil {
		return nil, err
	}

	result, err := fn.binder.eval(ctx, env, n.Args[:last], bind)
	if err != nil {
		if isStop(err) {
			return nil, err
		}
		return nil, fmt.Errorf("%s: %w", n.Func.Pos, err)
	}
	return result, nil
}

// BindingNode is a binding argument: a point binding when Hi is nil, otherwise a range binding.
// The parser accepts it only as the last argument of a binder call.
type BindingNode struct {
	Name tokens.Token
	Lo   Node
	Hi   Node
}

func (n *BindingNode) Eval(context.Context, *Env) (*unified.Real, error) {
	return nil, fmt.Errorf("%s: binding %s is only valid as an argument to a binder", n.Name.Pos, n.Name.Value)
}

// InfNode is the reserved word inf. The parser accepts it only as a range bound.
type InfNode struct {
	Tok tokens.Token
}

func (n *InfNode) Eval(context.Context, *Env) (*unified.Real, error) {
	return nil, fmt.Errorf("%s: inf is only valid as a range bound", n.Tok.Pos)
}

type AssignNode struct {
	Name  tokens.Token
	Value Node
}

func (n *AssignNode) Eval(ctx context.Context, env *Env) (*unified.Real, error) {
	if env == nil {
		env = NewEnv()
	}

	val, err := n.Value.Eval(ctx, env)
	if err != nil {
		return nil, err
	}

	if err := env.Set(n.Name.Value, val); err != nil {
		return nil, fmt.Errorf("%s: %w", n.Name.Pos, err)
	}
	return val, nil
}

type CommentNode struct {
	Text string
	Tok  tokens.Token
	Expr Node
}

func (n *CommentNode) Eval(ctx context.Context, env *Env) (*unified.Real, error) {
	if env.trace && env.traceOut != nil {
		fmt.Fprintf(env.traceOut, "# %s\n", n.Text)
	}
	return n.Expr.Eval(ctx, env)
}

// modulo computes a % b = a - b * floor(a/b) for real numbers
func modulo(ctx context.Context, a, b *unified.Real, precision int) (*unified.Real, error) {
	// scale = 2^(-precision)
	scale := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(-precision)), nil)

	// Approximate a
	aApproxInt, err := constructive.Approximate(ctx, a.Constructive(), precision)
	if err != nil {
		return nil, fmt.Errorf("failed to approximate dividend for modulo: %w", err)
	}
	aApproxRat := new(big.Rat).SetFrac(aApproxInt, scale)

	// Approximate b
	bApproxInt, err := constructive.Approximate(ctx, b.Constructive(), precision)
	if err != nil {
		return nil, fmt.Errorf("failed to approximate divisor for modulo: %w", err)
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
func extractInteger(ctx context.Context, r *unified.Real, op tokens.Token, precision int) (int, error) {
	// scale = 2^(-precision)
	scale := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(-precision)), nil)

	// Approximate r
	approxInt, err := constructive.Approximate(ctx, r.Constructive(), precision)
	if err != nil {
		return 0, fmt.Errorf("%s: failed to approximate shift count: %w", op.Pos, err)
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

func power(ctx context.Context, l, r *unified.Real, precision int) (*unified.Real, error) {
	// Approximate both operands to check for special cases
	scale := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(-precision)), nil)

	// Approximate left (base
	lApprox, err := constructive.Approximate(ctx, l.Constructive(), precision)
	if err != nil {
		return nil, fmt.Errorf("failed to approximate base: %w", err)
	}
	lRat := new(big.Rat).SetFrac(lApprox, scale)

	// Approximate right (exponent)
	rApprox, err := constructive.Approximate(ctx, r.Constructive(), precision)
	if err != nil {
		return nil, fmt.Errorf("failed to approximate exponent: %w", err)
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
		lc, err := newLoopCheck(ctx)
		if err != nil {
			return nil, err
		}
		for i := new(big.Int).Set(exp); i.Sign() > 0; i.Sub(i, big.NewInt(1)) {
			if err := lc.tick(); err != nil {
				return nil, err
			}
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
		lc, err := newLoopCheck(ctx)
		if err != nil {
			return nil, err
		}
		for i := new(big.Int).Set(exp); i.Sign() > 0; i.Sub(i, big.NewInt(1)) {
			if err := lc.tick(); err != nil {
				return nil, err
			}
			result.Mul(result, base)
		}

		return unified.New(constructive.One(), rational.FromRational(result)), nil
	}

	// Exponent is negative integer or non-integer: use constructive.Pow
	// This handles fractional powers, irrational powers, and negative powers
	cr := constructive.Pow(l.Constructive(), r.Constructive())
	return unified.New(cr, rational.One()), nil
}
