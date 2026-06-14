package parser

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/rational"
	"github.com/ripta/reals/pkg/unified"
)

// function is a registry entry. minArgs and maxArgs bound the accepted argument
// count; maxArgs of -1 means unbounded variadic. fn receives the environment so
// implementations that decide irrational operands at a binary precision can read
// env.precision.
type function struct {
	minArgs int
	maxArgs int
	fn      func(env *Env, args []*unified.Real) (*unified.Real, error)
}

// functions is the registry consulted by CallNode. Names share the identifier
// namespace with variables and constants but are only looked up in call
// position, so a variable named sin and the sin function coexist.
var functions = map[string]function{
	"abs":   {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Abs(), nil }},
	"sin":   {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sin(), nil }},
	"cos":   {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cos(), nil }},
	"tan":   {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Tan(), nil }},
	"asin":  {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Asin() }},
	"acos":  {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Acos() }},
	"atan":  {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Atan(), nil }},
	"atan2": {2, 2, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Atan2(a[1]) }},
	"exp":   {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Exp(), nil }},
	"ln":    {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Ln() }},
	"log10": {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Log10() }},
	"log2":  {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Log2() }},
	"log":   {2, 2, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Log(a[1]) }},
	"sqrt":  {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sqrt() }},
	"cbrt":  {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cbrt(), nil }},

	"floor":     {1, 1, func(e *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Floor(e.precision), nil }},
	"ceil":      {1, 1, func(e *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Ceil(e.precision), nil }},
	"round":     {1, 1, func(e *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Round(e.precision), nil }},
	"min":       {1, -1, func(e *Env, a []*unified.Real) (*unified.Real, error) { return foldMinMax(a, e.precision, false), nil }},
	"max":       {1, -1, func(e *Env, a []*unified.Real) (*unified.Real, error) { return foldMinMax(a, e.precision, true), nil }},
	"sinh":      {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sinh(), nil }},
	"cosh":      {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cosh(), nil }},
	"tanh":      {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Tanh(), nil }},
	"factorial": {1, 1, func(e *Env, a []*unified.Real) (*unified.Real, error) { return factorial(a[0], e.precision) }},
}

// errFactorialDomain reports an argument to factorial that is negative or not an
// integer. It carries no sentinel match in domainError, so its own message
// becomes the reason text: factorial(-1): argument must be a non-negative integer.
var errFactorialDomain = errors.New("argument must be a non-negative integer")

// foldMinMax reduces args to their minimum or maximum with a left-to-right
// pairwise fold. Operands equal within precision resolve to the leftmost, which
// is the library's own tie behavior.
func foldMinMax(args []*unified.Real, precision int, max bool) *unified.Real {
	acc := args[0]
	for _, a := range args[1:] {
		if max {
			acc = acc.Max(a, precision)
		} else {
			acc = acc.Min(a, precision)
		}
	}
	return acc
}

// factorial computes the exact factorial of a non-negative integer. The argument
// is decided at precision; a non-integer or negative value is a domain error.
// The product is exact over big.Int, so the result is a rational Real.
func factorial(r *unified.Real, precision int) (*unified.Real, error) {
	scale := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(-precision)), nil)

	approx := constructive.Approximate(r.Constructive(), precision)
	if approx == nil {
		return nil, errFactorialDomain
	}

	rat := new(big.Rat).SetFrac(approx, scale)
	if !rat.IsInt() || rat.Sign() < 0 {
		return nil, errFactorialDomain
	}

	n := rat.Num()
	result := big.NewInt(1)
	for i := big.NewInt(2); i.Cmp(n) <= 0; i.Add(i, big.NewInt(1)) {
		result.Mul(result, i)
	}

	return unified.New(constructive.One(), rational.FromRational(new(big.Rat).SetInt(result))), nil
}

// arityError formats a message describing the accepted argument count against
// the count actually supplied.
func arityError(f function, got int) string {
	switch {
	case f.maxArgs < 0:
		return fmt.Sprintf("expects at least %s, got %d", plural(f.minArgs), got)
	case f.minArgs == f.maxArgs:
		return fmt.Sprintf("expects %s, got %d", plural(f.minArgs), got)
	default:
		return fmt.Sprintf("expects %d to %d arguments, got %d", f.minArgs, f.maxArgs, got)
	}
}

func plural(n int) string {
	if n == 1 {
		return "1 argument"
	}
	return fmt.Sprintf("%d arguments", n)
}

// domainError formats a function error in call form, naming the function and
// its rendered arguments followed by the reason: sqrt(-1): argument must be
// non-negative. Recognized upstream sentinels map to calc-owned reason text;
// any other error falls back to its own message so non-domain failures still
// surface.
func domainError(name string, args []*unified.Real, precision int, err error) error {
	rendered := make([]string, len(args))
	for i, a := range args {
		rendered[i] = formatArg(a, precision)
	}
	call := fmt.Sprintf("%s(%s)", name, strings.Join(rendered, ", "))

	var reason string
	switch {
	case errors.Is(err, unified.ErrNonPositive):
		reason = "argument must be positive"
	case errors.Is(err, unified.ErrNegative):
		reason = "argument must be non-negative"
	case errors.Is(err, unified.ErrOutsideUnitInterval):
		reason = "argument must be in [-1, 1]"
	case errors.Is(err, unified.ErrUndefinedAtOrigin):
		reason = "undefined at the origin"
	case errors.Is(err, unified.ErrInvalidBase):
		reason = "base must not be equal to one"
	default:
		reason = err.Error()
	}

	return fmt.Errorf("%s: %s", call, reason)
}

// formatArg renders a Real for an error message, approximating it to the active
// precision. Integers print as plain digits; other values print as a decimal
// with trailing zeros trimmed.
func formatArg(r *unified.Real, precision int) string {
	approx := constructive.Approximate(r.Constructive(), precision)
	if approx == nil {
		return "?"
	}

	denom := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(-precision)), nil)
	rat := new(big.Rat).SetFrac(approx, denom)
	if rat.IsInt() {
		return rat.Num().String()
	}

	s := rat.FloatString(-precision)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}
