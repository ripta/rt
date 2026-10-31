package parser

import (
	"context"
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
//
// A non-nil binder marks a binder: its last argument is a binding, and it
// evaluates its other arguments itself. Arity counts the binding argument.
type function struct {
	name      string
	group     string
	signature string
	summary   string
	minArgs   int
	maxArgs   int
	binder    *binderSpec
	fn        func(ctx context.Context, env *Env, args []*unified.Real) (*unified.Real, error)
}

// bindForm is a set of binding argument forms.
type bindForm int

const (
	bindPoint bindForm = 1 << iota
	bindRange
)

// binderSpec describes what a binder accepts as its binding argument.
type binderSpec struct {
	forms    bindForm
	infinite bool
	eval     binderFunc
}

// functionCatalog is the source of truth for the function registry. Entries are
// ordered by group so the discoverability listing iterates it directly and emits
// a header whenever the group changes, with no separate ordering table. The
// dispatch map below is derived from it.
var functionCatalog = []function{
	{name: "abs", group: "Basic", signature: "abs(x)", summary: "absolute value", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Abs(), nil }},
	{name: "signum", group: "Basic", signature: "signum(x)", summary: "sign of x as -1, 0, or 1", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return signum(ctx, a[0], e.precision)
	}},

	{name: "sin", group: "Trigonometric (radians)", signature: "sin(x)", summary: "sine", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sin(), nil }},
	{name: "cos", group: "Trigonometric (radians)", signature: "cos(x)", summary: "cosine", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cos(), nil }},
	{name: "tan", group: "Trigonometric (radians)", signature: "tan(x)", summary: "tangent", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Tan(), nil }},
	{name: "asin", group: "Trigonometric (radians)", signature: "asin(x)", summary: "inverse sine, returns radians", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Asin(ctx) }},
	{name: "acos", group: "Trigonometric (radians)", signature: "acos(x)", summary: "inverse cosine, returns radians", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Acos(ctx) }},
	{name: "atan", group: "Trigonometric (radians)", signature: "atan(x)", summary: "inverse tangent, returns radians", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Atan(), nil }},
	{name: "atan2", group: "Trigonometric (radians)", signature: "atan2(y, x)", summary: "angle of the point (x, y), returns radians", minArgs: 2, maxArgs: 2, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) {
		return a[0].Atan2(ctx, a[1])
	}},
	{name: "deg2rad", group: "Trigonometric (radians)", signature: "deg2rad(x)", summary: "convert degrees to radians", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return deg2rad(a[0]), nil }},
	{name: "rad2deg", group: "Trigonometric (radians)", signature: "rad2deg(x)", summary: "convert radians to degrees", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return rad2deg(a[0]), nil }},

	{name: "exp", group: "Exponential and logarithmic", signature: "exp(x)", summary: "e raised to x", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Exp(), nil }},
	{name: "ln", group: "Exponential and logarithmic", signature: "ln(x)", summary: "natural logarithm", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Ln(ctx) }},
	{name: "log10", group: "Exponential and logarithmic", signature: "log10(x)", summary: "base-10 logarithm", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Log10(ctx) }},
	{name: "log2", group: "Exponential and logarithmic", signature: "log2(x)", summary: "base-2 logarithm", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Log2(ctx) }},
	{name: "log", group: "Exponential and logarithmic", signature: "log(x, base)", summary: "logarithm of x in the given base", minArgs: 2, maxArgs: 2, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) {
		return a[0].Log(ctx, a[1])
	}},

	{name: "sqrt", group: "Roots", signature: "sqrt(x)", summary: "square root", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sqrt(ctx) }},
	{name: "cbrt", group: "Roots", signature: "cbrt(x)", summary: "cube root, defined for negatives", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cbrt(), nil }},

	{name: "hypot", group: "Geometry", signature: "hypot(x, y)", summary: "length of the hypotenuse, sqrt(x^2 + y^2)", minArgs: 2, maxArgs: 2, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) {
		return hypot(ctx, a[0], a[1])
	}},
	{name: "dist", group: "Geometry", signature: "dist(x1, y1, x2, y2)", summary: "Euclidean distance between two points", minArgs: 4, maxArgs: 4, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) {
		return dist(ctx, a[0], a[1], a[2], a[3])
	}},
	{name: "norm", group: "Geometry", signature: "norm(x, ...)", summary: "Euclidean norm, sqrt of the sum of squares", minArgs: 1, maxArgs: -1, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return norm(ctx, a) }},

	{name: "floor", group: "Rounding", signature: "floor(x)", summary: "round down to an integer", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return a[0].Floor(ctx, e.precision)
	}},
	{name: "ceil", group: "Rounding", signature: "ceil(x)", summary: "round up to an integer", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return a[0].Ceil(ctx, e.precision)
	}},
	{name: "round", group: "Rounding", signature: "round(x)", summary: "round half away from zero", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return a[0].Round(ctx, e.precision)
	}},
	{name: "trunc", group: "Rounding", signature: "trunc(x)", summary: "round toward zero", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return trunc(ctx, a[0], e.precision)
	}},

	{name: "min", group: "Comparison", signature: "min(x, ...)", summary: "smallest argument", minArgs: 1, maxArgs: -1, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return foldMinMax(ctx, a, e.precision, false)
	}},
	{name: "max", group: "Comparison", signature: "max(x, ...)", summary: "largest argument", minArgs: 1, maxArgs: -1, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return foldMinMax(ctx, a, e.precision, true)
	}},

	{name: "sinh", group: "Hyperbolic", signature: "sinh(x)", summary: "hyperbolic sine", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sinh(), nil }},
	{name: "cosh", group: "Hyperbolic", signature: "cosh(x)", summary: "hyperbolic cosine", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Cosh(), nil }},
	{name: "tanh", group: "Hyperbolic", signature: "tanh(x)", summary: "hyperbolic tangent", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Tanh(), nil }},
	{name: "asinh", group: "Hyperbolic", signature: "asinh(x)", summary: "inverse hyperbolic sine", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return asinh(ctx, a[0]) }},
	{name: "acosh", group: "Hyperbolic", signature: "acosh(x)", summary: "inverse hyperbolic cosine", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return acosh(ctx, a[0], e.precision)
	}},
	{name: "atanh", group: "Hyperbolic", signature: "atanh(x)", summary: "inverse hyperbolic tangent", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return atanh(ctx, a[0], e.precision)
	}},

	{name: "factorial", group: "Combinatorial", signature: "factorial(n)", summary: "factorial of a non-negative integer", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return factorial(ctx, a[0], e.precision)
	}},
	{name: "gamma", group: "Combinatorial", signature: "gamma(x)", summary: "gamma function, the continuous factorial", minArgs: 1, maxArgs: 1, fn: func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Gamma() }},
	{name: "lgamma", group: "Combinatorial", signature: "lgamma(x)", summary: "natural log of the absolute value of gamma", minArgs: 1, maxArgs: 1, fn: func(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return lgamma(ctx, a[0]) }},
	{name: "choose", group: "Combinatorial", signature: "choose(n, k)", summary: "binomial coefficient, n choose k", minArgs: 2, maxArgs: 2, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return choose(ctx, a[0], a[1], e.precision)
	}},
	{name: "perm", group: "Combinatorial", signature: "perm(n, k)", summary: "number of k-permutations of n", minArgs: 2, maxArgs: 2, fn: func(ctx context.Context, e *Env, a []*unified.Real) (*unified.Real, error) {
		return perm(ctx, a[0], a[1], e.precision)
	}},

	{name: "sum", group: "Binders", signature: "sum(expr, i = a..b)", summary: "sum of expr for integer i from a to b", minArgs: 2, maxArgs: 2, binder: &binderSpec{forms: bindRange, eval: sumBinder}},
	{name: "product", group: "Binders", signature: "product(expr, i = a..b)", summary: "product of expr for integer i from a to b", minArgs: 2, maxArgs: 2, binder: &binderSpec{forms: bindRange, eval: productBinder}},
	{name: "solve", group: "Binders", signature: "solve(expr, x = a..b)", summary: "a root of expr in the bracket a..b; expr must be continuous and change sign there, and which root is returned is unspecified", minArgs: 2, maxArgs: 2, binder: &binderSpec{forms: bindRange, eval: solveBinder}},
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
func foldMinMax(ctx context.Context, args []*unified.Real, precision int, max bool) (*unified.Real, error) {
	acc := args[0]
	for _, a := range args[1:] {
		var err error
		if max {
			acc, err = acc.Max(ctx, a, precision)
		} else {
			acc, err = acc.Min(ctx, a, precision)
		}
		if err != nil {
			return nil, err
		}
	}
	return acc, nil
}

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

// approxRat decides r to a big.Rat at the given binary precision. The error is
// the approximation's own, such as an invalid precision or a canceled context.
func approxRat(ctx context.Context, r *unified.Real, precision int) (*big.Rat, error) {
	approx, err := constructive.Approximate(ctx, r.Constructive(), precision)
	if err != nil {
		return nil, err
	}

	scale := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(-precision)), nil)
	return new(big.Rat).SetFrac(approx, scale), nil
}

// intReal wraps an exact integer as a rational Real.
func intReal(n *big.Int) *unified.Real {
	return unified.New(constructive.One(), rational.FromRational(new(big.Rat).SetInt(n)))
}

// factorial computes the exact factorial of a non-negative integer. The argument
// is decided at precision; a non-integer or negative value is a domain error.
// The product is exact over big.Int, so the result is a rational Real.
func factorial(ctx context.Context, r *unified.Real, precision int) (*unified.Real, error) {
	rat, err := approxRat(ctx, r, precision)
	if err != nil {
		return nil, err
	}
	if !rat.IsInt() || rat.Sign() < 0 {
		return nil, errFactorialDomain
	}

	n := rat.Num()
	result := big.NewInt(1)
	lc, err := newLoopCheck(ctx)
	if err != nil {
		return nil, err
	}
	for i := big.NewInt(2); i.Cmp(n) <= 0; i.Add(i, big.NewInt(1)) {
		if err := lc.tick(); err != nil {
			return nil, err
		}
		result.Mul(result, i)
	}

	return intReal(result), nil
}

// signum returns the sign of r as -1, 0, or 1, decided at precision.
func signum(ctx context.Context, r *unified.Real, precision int) (*unified.Real, error) {
	rat, err := approxRat(ctx, r, precision)
	if err != nil {
		return nil, err
	}
	return intReal(big.NewInt(int64(rat.Sign()))), nil
}

// trunc rounds r toward zero, decided at precision: floor for non-negative
// values, ceil for negative ones.
func trunc(ctx context.Context, r *unified.Real, precision int) (*unified.Real, error) {
	rat, err := approxRat(ctx, r, precision)
	if err != nil {
		return nil, err
	}
	if rat.Sign() < 0 {
		return r.Ceil(ctx, precision)
	}
	return r.Floor(ctx, precision)
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
func hypot(ctx context.Context, x, y *unified.Real) (*unified.Real, error) {
	return x.Multiply(x).Add(y.Multiply(y)).Sqrt(ctx)
}

// dist returns the Euclidean distance between the points (x1, y1) and (x2, y2).
func dist(ctx context.Context, x1, y1, x2, y2 *unified.Real) (*unified.Real, error) {
	return hypot(ctx, x2.Subtract(x1), y2.Subtract(y1))
}

// norm returns the Euclidean norm of a vector, sqrt of the sum of squares. It
// accepts any number of components, generalizing hypot beyond two dimensions.
func norm(ctx context.Context, components []*unified.Real) (*unified.Real, error) {
	sum := components[0].Multiply(components[0])
	for _, c := range components[1:] {
		sum = sum.Add(c.Multiply(c))
	}
	return sum.Sqrt(ctx)
}

// asinh returns the inverse hyperbolic sine, ln(x + sqrt(x^2 + 1)). The
// arguments to both sqrt and ln stay positive for every real x.
func asinh(ctx context.Context, x *unified.Real) (*unified.Real, error) {
	root, err := x.Multiply(x).Add(intReal(big.NewInt(1))).Sqrt(ctx)
	if err != nil {
		return nil, err
	}
	return x.Add(root).Ln(ctx)
}

// acosh returns the inverse hyperbolic cosine, ln(x + sqrt(x^2 - 1)), defined
// for x >= 1.
func acosh(ctx context.Context, x *unified.Real, precision int) (*unified.Real, error) {
	rat, err := approxRat(ctx, x, precision)
	if err != nil {
		return nil, err
	}
	if rat.Cmp(big.NewRat(1, 1)) < 0 {
		return nil, errAcoshDomain
	}
	root, err := x.Multiply(x).Subtract(intReal(big.NewInt(1))).Sqrt(ctx)
	if err != nil {
		return nil, err
	}
	return x.Add(root).Ln(ctx)
}

// atanh returns the inverse hyperbolic tangent, ln((1 + x) / (1 - x)) / 2,
// defined for -1 < x < 1.
func atanh(ctx context.Context, x *unified.Real, precision int) (*unified.Real, error) {
	rat, err := approxRat(ctx, x, precision)
	if err != nil {
		return nil, err
	}
	if rat.Cmp(big.NewRat(1, 1)) >= 0 || rat.Cmp(big.NewRat(-1, 1)) <= 0 {
		return nil, errAtanhDomain
	}
	one := intReal(big.NewInt(1))
	l, err := one.Add(x).Divide(one.Subtract(x)).Ln(ctx)
	if err != nil {
		return nil, err
	}
	return l.Divide(intReal(big.NewInt(2))), nil
}

// lgamma returns the natural logarithm of the absolute value of the gamma
// function. The pole error from gamma propagates; elsewhere |gamma| is positive
// so the logarithm is defined.
func lgamma(ctx context.Context, x *unified.Real) (*unified.Real, error) {
	g, err := x.Gamma()
	if err != nil {
		return nil, err
	}
	return g.Abs().Ln(ctx)
}

// combInt decides r to a non-negative integer at precision, returning
// errCombinatoricDomain when r is negative or not an integer.
func combInt(ctx context.Context, r *unified.Real, precision int) (*big.Int, error) {
	rat, err := approxRat(ctx, r, precision)
	if err != nil {
		return nil, err
	}
	if !rat.IsInt() || rat.Sign() < 0 {
		return nil, errCombinatoricDomain
	}
	return rat.Num(), nil
}

// choose returns the binomial coefficient C(n, k) for non-negative integers,
// zero when k > n. The running product C(n, i) is integral at every step, so the
// division stays exact.
func choose(ctx context.Context, nr, kr *unified.Real, precision int) (*unified.Real, error) {
	n, err := combInt(ctx, nr, precision)
	if err != nil {
		return nil, err
	}
	k, err := combInt(ctx, kr, precision)
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
	lc, err := newLoopCheck(ctx)
	if err != nil {
		return nil, err
	}
	for i := new(big.Int); i.Cmp(k) < 0; i.Add(i, one) {
		if err := lc.tick(); err != nil {
			return nil, err
		}
		result.Mul(result, num)
		result.Quo(result, den)
		num.Sub(num, one)
		den.Add(den, one)
	}
	return intReal(result), nil
}

// perm returns the number of k-permutations of n for non-negative integers,
// zero when k > n.
func perm(ctx context.Context, nr, kr *unified.Real, precision int) (*unified.Real, error) {
	n, err := combInt(ctx, nr, precision)
	if err != nil {
		return nil, err
	}
	k, err := combInt(ctx, kr, precision)
	if err != nil {
		return nil, err
	}
	if k.Cmp(n) > 0 {
		return intReal(big.NewInt(0)), nil
	}

	one := big.NewInt(1)
	result := big.NewInt(1)
	num := new(big.Int).Set(n)
	lc, err := newLoopCheck(ctx)
	if err != nil {
		return nil, err
	}
	for i := new(big.Int); i.Cmp(k) < 0; i.Add(i, one) {
		if err := lc.tick(); err != nil {
			return nil, err
		}
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
// surface. Budget and cancellation errors pass through unchanged.
func domainError(ctx context.Context, name string, args []*unified.Real, precision int, err error) error {
	if isStop(err) {
		return err
	}

	rendered := make([]string, len(args))
	for i, a := range args {
		rendered[i] = formatArg(ctx, a, precision)
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
	case errors.Is(err, unified.ErrNonPositiveInteger):
		reason = "argument must not be a non-positive integer"
	default:
		reason = err.Error()
	}

	return fmt.Errorf("%s: %s", call, reason)
}

// formatArg renders a Real for an error message, approximating it to the active
// precision. Integers print as plain digits; other values print as a decimal
// with trailing zeros trimmed.
func formatArg(ctx context.Context, r *unified.Real, precision int) string {
	rat, err := approxRat(ctx, r, precision)
	if err != nil {
		return "?"
	}
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
