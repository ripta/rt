package parser

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ripta/reals/pkg/constructive"
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
