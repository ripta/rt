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
// env.precision. name, group, signature, and summary drive the discoverability
// listing; group orders the catalog and signature/summary are human-facing.
type function struct {
	name      string
	group     string
	signature string
	summary   string
	minArgs   int
	maxArgs   int
	fn        func(env *Env, args []*unified.Real) (*unified.Real, error)
}

// functionCatalog is the source of truth for the function registry. Entries are
// ordered by group so the discoverability listing iterates it directly and emits
// a header whenever the group changes, with no separate ordering table. The
// dispatch map below is derived from it.
var functionCatalog = []function{
	{name: "abs", group: "Basic", signature: "abs(x)", summary: "absolute value", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Abs(), nil }},

	{name: "sin", group: "Trigonometric (radians)", signature: "sin(x)", summary: "sine", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sin(), nil }},
	{name: "cos", group: "Trigonometric (radians)", signature: "cos(x)", summary: "cosine", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cos(), nil }},
	{name: "tan", group: "Trigonometric (radians)", signature: "tan(x)", summary: "tangent", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Tan(), nil }},
	{name: "asin", group: "Trigonometric (radians)", signature: "asin(x)", summary: "inverse sine, returns radians", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Asin() }},
	{name: "acos", group: "Trigonometric (radians)", signature: "acos(x)", summary: "inverse cosine, returns radians", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Acos() }},
	{name: "atan", group: "Trigonometric (radians)", signature: "atan(x)", summary: "inverse tangent, returns radians", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Atan(), nil }},
	{name: "atan2", group: "Trigonometric (radians)", signature: "atan2(y, x)", summary: "angle of the point (x, y), returns radians", minArgs: 2, maxArgs: 2, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Atan2(a[1]) }},

	{name: "exp", group: "Exponential and logarithmic", signature: "exp(x)", summary: "e raised to x", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Exp(), nil }},
	{name: "ln", group: "Exponential and logarithmic", signature: "ln(x)", summary: "natural logarithm", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Ln() }},
	{name: "log10", group: "Exponential and logarithmic", signature: "log10(x)", summary: "base-10 logarithm", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Log10() }},
	{name: "log2", group: "Exponential and logarithmic", signature: "log2(x)", summary: "base-2 logarithm", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Log2() }},
	{name: "log", group: "Exponential and logarithmic", signature: "log(x, base)", summary: "logarithm of x in the given base", minArgs: 2, maxArgs: 2, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Log(a[1]) }},

	{name: "sqrt", group: "Roots", signature: "sqrt(x)", summary: "square root", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sqrt() }},
	{name: "cbrt", group: "Roots", signature: "cbrt(x)", summary: "cube root, defined for negatives", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cbrt(), nil }},

	{name: "floor", group: "Rounding", signature: "floor(x)", summary: "round down to an integer", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Floor(e.precision), nil }},
	{name: "ceil", group: "Rounding", signature: "ceil(x)", summary: "round up to an integer", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Ceil(e.precision), nil }},
	{name: "round", group: "Rounding", signature: "round(x)", summary: "round half away from zero", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Round(e.precision), nil }},

	{name: "min", group: "Comparison", signature: "min(x, ...)", summary: "smallest argument", minArgs: 1, maxArgs: -1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return foldMinMax(a, e.precision, false), nil }},
	{name: "max", group: "Comparison", signature: "max(x, ...)", summary: "largest argument", minArgs: 1, maxArgs: -1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return foldMinMax(a, e.precision, true), nil }},

	{name: "sinh", group: "Hyperbolic", signature: "sinh(x)", summary: "hyperbolic sine", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sinh(), nil }},
	{name: "cosh", group: "Hyperbolic", signature: "cosh(x)", summary: "hyperbolic cosine", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cosh(), nil }},
	{name: "tanh", group: "Hyperbolic", signature: "tanh(x)", summary: "hyperbolic tangent", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Tanh(), nil }},

	{name: "factorial", group: "Combinatorial", signature: "factorial(n)", summary: "factorial of a non-negative integer", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return factorial(a[0], e.precision) }},
}

// functions is the registry consulted by CallNode, derived from functionCatalog.
// Names share the identifier namespace with variables and constants but are only
// looked up in call position, so a variable named sin and the sin function coexist.
var functions = func() map[string]function {
	m := make(map[string]function, len(functionCatalog))
	for _, f := range functionCatalog {
		m[f.name] = f
	}
	return m
}()

// FunctionInfo describes a registered function for discoverability output.
type FunctionInfo struct {
	Name      string
	Group     string
	Signature string
	Summary   string
}

// Functions returns the registered functions in catalog order, grouped by
// category, for the calculator's discoverability listing.
func Functions() []FunctionInfo {
	infos := make([]FunctionInfo, len(functionCatalog))
	for i, f := range functionCatalog {
		infos[i] = FunctionInfo{
			Name:      f.name,
			Group:     f.group,
			Signature: f.signature,
			Summary:   f.summary,
		}
	}
	return infos
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
