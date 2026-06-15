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
	{name: "signum", group: "Basic", signature: "signum(x)", summary: "sign of x as -1, 0, or 1", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return signum(a[0], e.precision) }},

	{name: "sin", group: "Trigonometric (radians)", signature: "sin(x)", summary: "sine", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sin(), nil }},
	{name: "cos", group: "Trigonometric (radians)", signature: "cos(x)", summary: "cosine", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cos(), nil }},
	{name: "tan", group: "Trigonometric (radians)", signature: "tan(x)", summary: "tangent", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Tan(), nil }},
	{name: "asin", group: "Trigonometric (radians)", signature: "asin(x)", summary: "inverse sine, returns radians", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Asin() }},
	{name: "acos", group: "Trigonometric (radians)", signature: "acos(x)", summary: "inverse cosine, returns radians", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Acos() }},
	{name: "atan", group: "Trigonometric (radians)", signature: "atan(x)", summary: "inverse tangent, returns radians", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Atan(), nil }},
	{name: "atan2", group: "Trigonometric (radians)", signature: "atan2(y, x)", summary: "angle of the point (x, y), returns radians", minArgs: 2, maxArgs: 2, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Atan2(a[1]) }},
	{name: "deg2rad", group: "Trigonometric (radians)", signature: "deg2rad(x)", summary: "convert degrees to radians", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return deg2rad(a[0]), nil }},
	{name: "rad2deg", group: "Trigonometric (radians)", signature: "rad2deg(x)", summary: "convert radians to degrees", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return rad2deg(a[0]), nil }},

	{name: "exp", group: "Exponential and logarithmic", signature: "exp(x)", summary: "e raised to x", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Exp(), nil }},
	{name: "ln", group: "Exponential and logarithmic", signature: "ln(x)", summary: "natural logarithm", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Ln() }},
	{name: "log10", group: "Exponential and logarithmic", signature: "log10(x)", summary: "base-10 logarithm", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Log10() }},
	{name: "log2", group: "Exponential and logarithmic", signature: "log2(x)", summary: "base-2 logarithm", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Log2() }},
	{name: "log", group: "Exponential and logarithmic", signature: "log(x, base)", summary: "logarithm of x in the given base", minArgs: 2, maxArgs: 2, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Log(a[1]) }},

	{name: "sqrt", group: "Roots", signature: "sqrt(x)", summary: "square root", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sqrt() }},
	{name: "cbrt", group: "Roots", signature: "cbrt(x)", summary: "cube root, defined for negatives", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cbrt(), nil }},

	{name: "hypot", group: "Geometry", signature: "hypot(x, y)", summary: "length of the hypotenuse, sqrt(x^2 + y^2)", minArgs: 2, maxArgs: 2, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return hypot(a[0], a[1]) }},
	{name: "dist", group: "Geometry", signature: "dist(x1, y1, x2, y2)", summary: "Euclidean distance between two points", minArgs: 4, maxArgs: 4, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return dist(a[0], a[1], a[2], a[3]) }},
	{name: "norm", group: "Geometry", signature: "norm(x, ...)", summary: "Euclidean norm, sqrt of the sum of squares", minArgs: 1, maxArgs: -1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return norm(a) }},

	{name: "floor", group: "Rounding", signature: "floor(x)", summary: "round down to an integer", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Floor(e.precision), nil }},
	{name: "ceil", group: "Rounding", signature: "ceil(x)", summary: "round up to an integer", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Ceil(e.precision), nil }},
	{name: "round", group: "Rounding", signature: "round(x)", summary: "round half away from zero", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Round(e.precision), nil }},
	{name: "trunc", group: "Rounding", signature: "trunc(x)", summary: "round toward zero", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return trunc(a[0], e.precision) }},

	{name: "min", group: "Comparison", signature: "min(x, ...)", summary: "smallest argument", minArgs: 1, maxArgs: -1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return foldMinMax(a, e.precision, false), nil }},
	{name: "max", group: "Comparison", signature: "max(x, ...)", summary: "largest argument", minArgs: 1, maxArgs: -1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return foldMinMax(a, e.precision, true), nil }},

	{name: "sinh", group: "Hyperbolic", signature: "sinh(x)", summary: "hyperbolic sine", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sinh(), nil }},
	{name: "cosh", group: "Hyperbolic", signature: "cosh(x)", summary: "hyperbolic cosine", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cosh(), nil }},
	{name: "tanh", group: "Hyperbolic", signature: "tanh(x)", summary: "hyperbolic tangent", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Tanh(), nil }},
	{name: "asinh", group: "Hyperbolic", signature: "asinh(x)", summary: "inverse hyperbolic sine", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return asinh(a[0]) }},
	{name: "acosh", group: "Hyperbolic", signature: "acosh(x)", summary: "inverse hyperbolic cosine", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return acosh(a[0], e.precision) }},
	{name: "atanh", group: "Hyperbolic", signature: "atanh(x)", summary: "inverse hyperbolic tangent", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return atanh(a[0], e.precision) }},

	{name: "factorial", group: "Combinatorial", signature: "factorial(n)", summary: "factorial of a non-negative integer", minArgs: 1, maxArgs: 1, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return factorial(a[0], e.precision) }},
	{name: "gamma", group: "Combinatorial", signature: "gamma(x)", summary: "gamma function, the continuous factorial", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Gamma() }},
	{name: "lgamma", group: "Combinatorial", signature: "lgamma(x)", summary: "natural log of the absolute value of gamma", minArgs: 1, maxArgs: 1, fn: func(_ *Env, a []*unified.Real) (*unified.Real, error) { return lgamma(a[0]) }},
	{name: "choose", group: "Combinatorial", signature: "choose(n, k)", summary: "binomial coefficient, n choose k", minArgs: 2, maxArgs: 2, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return choose(a[0], a[1], e.precision) }},
	{name: "perm", group: "Combinatorial", signature: "perm(n, k)", summary: "number of k-permutations of n", minArgs: 2, maxArgs: 2, fn: func(e *Env, a []*unified.Real) (*unified.Real, error) { return perm(a[0], a[1], e.precision) }},
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

// errIndeterminate reports a value that cannot be decided at the active
// precision. It is reachable only when the precision itself is invalid, so it
// stands in for an internal failure rather than a user domain error.
var errIndeterminate = errors.New("cannot decide value at the current precision")

// errCombinatoricDomain reports an argument to choose or perm that is negative
// or not an integer. Like errFactorialDomain it carries no sentinel match, so
// its own message becomes the reason text.
var errCombinatoricDomain = errors.New("arguments must be non-negative integers")

// errAcoshDomain and errAtanhDomain report arguments outside the real domains of
// the inverse hyperbolic cosine and tangent.
var (
	errAcoshDomain = errors.New("argument must be at least 1")
	errAtanhDomain = errors.New("argument must be in (-1, 1)")
)

// approxRat decides r to a big.Rat at the given binary precision, returning nil
// only when the precision is invalid and the value cannot be approximated.
func approxRat(r *unified.Real, precision int) *big.Rat {
	approx := constructive.Approximate(r.Constructive(), precision)
	if approx == nil {
		return nil
	}

	scale := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(-precision)), nil)
	return new(big.Rat).SetFrac(approx, scale)
}

// intReal wraps an exact integer as a rational Real.
func intReal(n *big.Int) *unified.Real {
	return unified.New(constructive.One(), rational.FromRational(new(big.Rat).SetInt(n)))
}

// factorial computes the exact factorial of a non-negative integer. The argument
// is decided at precision; a non-integer or negative value is a domain error.
// The product is exact over big.Int, so the result is a rational Real.
func factorial(r *unified.Real, precision int) (*unified.Real, error) {
	rat := approxRat(r, precision)
	if rat == nil || !rat.IsInt() || rat.Sign() < 0 {
		return nil, errFactorialDomain
	}

	n := rat.Num()
	result := big.NewInt(1)
	for i := big.NewInt(2); i.Cmp(n) <= 0; i.Add(i, big.NewInt(1)) {
		result.Mul(result, i)
	}

	return intReal(result), nil
}

// signum returns the sign of r as -1, 0, or 1, decided at precision.
func signum(r *unified.Real, precision int) (*unified.Real, error) {
	rat := approxRat(r, precision)
	if rat == nil {
		return nil, errIndeterminate
	}
	return intReal(big.NewInt(int64(rat.Sign()))), nil
}

// trunc rounds r toward zero, decided at precision: floor for non-negative
// values, ceil for negative ones.
func trunc(r *unified.Real, precision int) (*unified.Real, error) {
	rat := approxRat(r, precision)
	if rat == nil {
		return nil, errIndeterminate
	}
	if rat.Sign() < 0 {
		return r.Ceil(precision), nil
	}
	return r.Floor(precision), nil
}

// deg2rad converts an angle in degrees to radians: x * pi / 180.
func deg2rad(r *unified.Real) *unified.Real {
	return r.Multiply(unified.Pi()).Divide(intReal(big.NewInt(180)))
}

// rad2deg converts an angle in radians to degrees: x * 180 / pi.
func rad2deg(r *unified.Real) *unified.Real {
	return r.Multiply(intReal(big.NewInt(180))).Divide(unified.Pi())
}

// hypot returns sqrt(x^2 + y^2). The radicand is never negative, so the only
// error path is an upstream failure to take the root.
func hypot(x, y *unified.Real) (*unified.Real, error) {
	return x.Multiply(x).Add(y.Multiply(y)).Sqrt()
}

// dist returns the Euclidean distance between the points (x1, y1) and (x2, y2).
func dist(x1, y1, x2, y2 *unified.Real) (*unified.Real, error) {
	return hypot(x2.Subtract(x1), y2.Subtract(y1))
}

// norm returns the Euclidean norm of a vector, sqrt of the sum of squares. It
// accepts any number of components, generalizing hypot beyond two dimensions.
func norm(components []*unified.Real) (*unified.Real, error) {
	sum := components[0].Multiply(components[0])
	for _, c := range components[1:] {
		sum = sum.Add(c.Multiply(c))
	}
	return sum.Sqrt()
}

// asinh returns the inverse hyperbolic sine, ln(x + sqrt(x^2 + 1)). The
// arguments to both sqrt and ln stay positive for every real x.
func asinh(x *unified.Real) (*unified.Real, error) {
	root, err := x.Multiply(x).Add(intReal(big.NewInt(1))).Sqrt()
	if err != nil {
		return nil, err
	}
	return x.Add(root).Ln()
}

// acosh returns the inverse hyperbolic cosine, ln(x + sqrt(x^2 - 1)), defined
// for x >= 1.
func acosh(x *unified.Real, precision int) (*unified.Real, error) {
	rat := approxRat(x, precision)
	if rat == nil {
		return nil, errIndeterminate
	}
	if rat.Cmp(big.NewRat(1, 1)) < 0 {
		return nil, errAcoshDomain
	}
	root, err := x.Multiply(x).Subtract(intReal(big.NewInt(1))).Sqrt()
	if err != nil {
		return nil, err
	}
	return x.Add(root).Ln()
}

// atanh returns the inverse hyperbolic tangent, ln((1 + x) / (1 - x)) / 2,
// defined for -1 < x < 1.
func atanh(x *unified.Real, precision int) (*unified.Real, error) {
	rat := approxRat(x, precision)
	if rat == nil {
		return nil, errIndeterminate
	}
	if rat.Cmp(big.NewRat(1, 1)) >= 0 || rat.Cmp(big.NewRat(-1, 1)) <= 0 {
		return nil, errAtanhDomain
	}
	one := intReal(big.NewInt(1))
	l, err := one.Add(x).Divide(one.Subtract(x)).Ln()
	if err != nil {
		return nil, err
	}
	return l.Divide(intReal(big.NewInt(2))), nil
}

// lgamma returns the natural logarithm of the absolute value of the gamma
// function. The pole error from gamma propagates; elsewhere |gamma| is positive
// so the logarithm is defined.
func lgamma(x *unified.Real) (*unified.Real, error) {
	g, err := x.Gamma()
	if err != nil {
		return nil, err
	}
	return g.Abs().Ln()
}

// combInt decides r to a non-negative integer at precision, returning
// errCombinatoricDomain when r is negative or not an integer.
func combInt(r *unified.Real, precision int) (*big.Int, error) {
	rat := approxRat(r, precision)
	if rat == nil || !rat.IsInt() || rat.Sign() < 0 {
		return nil, errCombinatoricDomain
	}
	return rat.Num(), nil
}

// choose returns the binomial coefficient C(n, k) for non-negative integers,
// zero when k > n. The running product C(n, i) is integral at every step, so the
// division stays exact.
func choose(nr, kr *unified.Real, precision int) (*unified.Real, error) {
	n, err := combInt(nr, precision)
	if err != nil {
		return nil, err
	}
	k, err := combInt(kr, precision)
	if err != nil {
		return nil, err
	}
	if k.Cmp(n) > 0 {
		return intReal(big.NewInt(0)), nil
	}

	one := big.NewInt(1)
	result := big.NewInt(1)
	num := new(big.Int).Set(n)
	den := big.NewInt(1)
	for i := new(big.Int); i.Cmp(k) < 0; i.Add(i, one) {
		result.Mul(result, num)
		result.Quo(result, den)
		num.Sub(num, one)
		den.Add(den, one)
	}
	return intReal(result), nil
}

// perm returns the number of k-permutations of n for non-negative integers,
// zero when k > n.
func perm(nr, kr *unified.Real, precision int) (*unified.Real, error) {
	n, err := combInt(nr, precision)
	if err != nil {
		return nil, err
	}
	k, err := combInt(kr, precision)
	if err != nil {
		return nil, err
	}
	if k.Cmp(n) > 0 {
		return intReal(big.NewInt(0)), nil
	}

	one := big.NewInt(1)
	result := big.NewInt(1)
	num := new(big.Int).Set(n)
	for i := new(big.Int); i.Cmp(k) < 0; i.Add(i, one) {
		result.Mul(result, num)
		num.Sub(num, one)
	}
	return intReal(result), nil
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
	case errors.Is(err, unified.ErrGammaPole):
		reason = "argument must not be a non-positive integer"
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
