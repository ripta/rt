package parser

import (
	"context"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/tokens"
)

const eulerGamma = 0.5772156649015329

func TestDiffValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want float64
	}{
		{"diff(sin(x), x = PI)", -1},
		{"diff(x*x*x, x = 2)", 12},
		{"diff(x*x, x = 3)", 6},
		{"diff(5, x = 1)", 0},
		{"diff(PI*x, x = 1)", math.Pi},
		{"diff(1/x, x = 2)", -0.25},
		{"diff(-x, x = 2)", -1},
		{"diff(x - x, x = 2)", 0},
		{"diff(x/x, x = 2)", 0},
		{"diff(floor(3)*x, x = 5)", 3},
		{"diff(2**3 + x, x = 1)", 1},
		{"diff(exp(x), x = 1)", math.E},
		{"diff(ln(x), x = 2)", 0.5},
		{"diff(sqrt(x), x = 4)", 0.25},
		{"diff(√x, x = 4)", 0.25},
		{"diff(cbrt(x), x = 8)", 1.0 / 12},
		{"diff(cbrt(x), x = -8)", 1.0 / 12},
		{"diff(tan(x), x = 0)", 1},
		{"diff(cos(x), x = PI/2)", -1},
		{"diff(asin(x), x = 0)", 1},
		{"diff(acos(x), x = 0)", -1},
		{"diff(atan(x), x = 1)", 0.5},
		{"diff(atan2(x, 1), x = 1)", 0.5},
		{"diff(atan2(1, x), x = 1)", -0.5},
		{"diff(sinh(x), x = 0)", 1},
		{"diff(cosh(x), x = 0)", 0},
		{"diff(tanh(x), x = 0)", 1},
		{"diff(asinh(x), x = 0)", 1},
		{"diff(acosh(x), x = 2)", 1 / math.Sqrt(3)},
		{"diff(atanh(x), x = 0)", 1},
		{"diff(log10(x), x = 1)", 1 / math.Ln10},
		{"diff(log2(x), x = 1)", 1 / math.Ln2},
		{"diff(log(x, 2), x = 1)", 1 / math.Ln2},
		{"diff(hypot(x, 4), x = 3)", 0.6},
		{"diff(hypot(3, x), x = 4)", 0.8},
		{"diff(dist(0, 0, x, 4), x = 3)", 0.6},
		{"diff(dist(x, 4, 0, 0), x = 3)", 0.6},
		{"diff(norm(x, 4, 12), x = 3)", 3.0 / 13},
		{"diff(deg2rad(x), x = 7)", math.Pi / 180},
		{"diff(rad2deg(x), x = 7)", 180 / math.Pi},
		{"diff(gamma(x), x = 1)", -eulerGamma},
		{"diff(gamma(x), x = 2)", 1 - eulerGamma},
		{"diff(lgamma(x), x = 1)", -eulerGamma},
		{"diff(lgamma(x), x = 3)", 1.5 - eulerGamma},
		{"diff(sin(x)*cos(x), x = 0)", 1},
		{"diff(sin(x*x), x = 1)", 2 * math.Cos(1)},
		{"diff(exp(sin(x)), x = 0)", 1},
		{"diff(sin(x)*exp(x), x = 1)", math.E * (math.Sin(1) + math.Cos(1))},
		{"diff(x*x + 3*x, x = 1/3)", 2.0/3 + 3},
		{"diff(x**0.5, x = 4)", 0.25},
		{"diff(x**(1/3), x = 8)", 1.0 / 12},
		{"diff(x**2.5, x = 4)", 20},
		{"diff(x**PI, x = 2)", math.Pi * math.Pow(2, math.Pi-1)},
		{"diff(2**x, x = 3)", 8 * math.Ln2},
		{"diff(x**x, x = 2)", 4 * (math.Ln2 + 1)},
		{"diff(x**sin(x), x = 1)", math.Sin(1)},
		{"diff(sqrt(x**2 + 1), x = 0)", 0},
		{"diff(0**2 + x, x = 1)", 1},
	}
	for _, tt := range tests {
		src, want := tt.src, tt.want
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got, err := evalFloat(t, context.Background(), src, NewEnv())
			if err != nil {
				t.Fatal(err)
			}
			if d := got - want; d > 1e-12 || d < -1e-12 {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestDiffExactRationals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
	}{
		{"diff(x*x*x, x = 2)", "12"},
		{"diff(x**3, x = 2)", "12"},
		{"diff(x**3, x = -2)", "12"},
		{"diff(x**3, x = 0)", "0"},
		{"diff(x**2, x = 0)", "0"},
		{"diff(x**1, x = 0)", "1"},
		{"diff(x**0, x = 0)", "0"},
		{"diff(x**(-2), x = 2)", "-1/4"},
		{"diff(x**(-1), x = -4)", "-1/16"},
		{"diff(x**4, x = -1)", "-4"},
		{"diff((x + 1)**5, x = 0)", "5"},
		{"diff(x**(4/2), x = 3)", "6"},
		{"diff(1/x, x = 4)", "-1/16"},
		{"diff(x/(1 + x), x = 1)", "1/4"},
		{"diff(7, x = 1)", "0"},
	}
	for _, tt := range tests {
		src, want := tt.src, tt.want
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got, err := evalShipping(context.Background(), src, NewEnv())
			if err != nil || got != want {
				t.Errorf("got %s, %v, want %s", got, err, want)
			}
		})
	}
}

// The derivative must stay within 2^(p+1) of the true value at every precision.
func TestDiffSharpens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
	}{
		{"diff(sin(x), x = PI)", "-1"},
		{"diff(atan(x), x = 1)", "1/2"},
		{"diff(sqrt(x), x = 2)", "1/(2*sqrt(2))"},
		{"diff(x**0.5, x = 2)", "1/(2*sqrt(2))"},
		{"diff(x**3, x = SQRT2)", "6"},
		{"diff(2**x, x = 1)", "2*ln(2)"},
		{"diff(x**x, x = 2)", "4*(ln(2) + 1)"},
		{"diff(x**SQRT2, x = 1)", "SQRT2"},
		{"diff(sin(x)*exp(x), x = 1)", "exp(1)*(sin(1) + cos(1))"},
		{"diff(sin(x)*sin(x) + cos(x)*cos(x), x = 1)", "0"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			node, err := New("test", tt.src).Parse()
			if err != nil {
				t.Fatal(err)
			}
			v, err := node.Eval(context.Background(), NewEnv())
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range []int{-100, -300, -1000} {
				got, err := approxRat(context.Background(), v, p)
				if err != nil {
					t.Fatalf("p=%d: %v", p, err)
				}
				diff := new(big.Rat).Sub(got, exactRat(t, tt.want, p))
				if diff.Abs(diff).Cmp(ratPow2(p+1)) > 0 {
					t.Errorf("p=%d: error %s is above 2^(p+1)", p, fmtRat(diff))
				}
			}
		})
	}
}

func TestDiffErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
	}{
		{"diff(abs(x), x = 1)", "not yet differentiable"},
		{"diff(floor(x), x = 1)", "not yet differentiable"},
		{"diff(x % 2, x = 1)", "not yet differentiable"},
		{"diff(x << 1, x = 1)", "not yet differentiable"},
		{"diff(factorial(x), x = 3)", "not yet differentiable"},
		{"diff(sum(x, i = 1..3), x = 1)", "not yet differentiable"},
		{"diff(integrate(x, t = 0..1), x = 1)", "not yet differentiable"},
		{"diff(diff(x, x = 1), x = 1)", "not yet differentiable"},
		{"diff(1/x, x = 0)", "division by zero"},
		{"diff(sqrt(x), x = 0)", "division by zero"},
		{"diff(ln(x), x = 0)", "ln(0)"},
		{"diff(asin(x), x = 1)", "division by zero"},
		{"diff(asin(x), x = -1)", "division by zero"},
		{"diff(acos(x), x = 1)", "division by zero"},
		{"diff(acos(x), x = -1)", "division by zero"},
		{"diff(√x, x = 0)", "division by zero"},
		{"diff(cbrt(x), x = 0)", "division by zero"},
		{"diff(acosh(x), x = 1)", "division by zero"},
		{"diff(hypot(x, 0), x = 0)", "division by zero"},
		{"diff(dist(x, 0, 0, 0), x = 0)", "division by zero"},
		{"diff(norm(x), x = 0)", "division by zero"},
		{"diff(norm(x, 0, 0), x = 0)", "division by zero"},
		{"diff(log10(x), x = 0)", "log10(0)"},
		{"diff(log2(x), x = 0)", "log2(0)"},
		{"diff(log(x, 3), x = 0)", "log(0, 3)"},
		{"diff(log(5, x), x = 1)", "base must not be equal to one"},
		{"diff(atanh(x), x = 1)", "atanh(1)"},
		{"diff(atanh(x), x = -1)", "atanh(-1)"},
		{"diff(acosh(x), x = 0)", "acosh(0)"},
		{"diff(lgamma(x), x = -1)", "lgamma(-1)"},
		{"diff(gamma(x), x = -2)", "gamma(-2)"},
		{"diff(gamma(x), x = 0)", "gamma(0)"},
		{"diff(x**0.5, x = 0)", "division by zero"},
		{"diff(x**0.5, x = -4)", "non-real"},
		{"diff(x**(-1), x = 0)", "zero to negative power"},
		{"diff((-2)**x, x = 1)", "ln(-2)"},
		{"diff(0**x, x = 1)", "ln(0)"},
		{"diff(x**x, x = 0)", "ln(0)"},
		{"diff(x**x, x = -1)", "ln(-1)"},
		{"diff(y, x = 1)", "undefined identifier"},
		{"diff(x, x = 1..2)", "point binding"},
		{"diff(x)", "binding"},
	}
	for _, tt := range tests {
		src, want := tt.src, tt.want
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			_, err := evalShipping(context.Background(), src, NewEnv())
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("got %v, want an error containing %q", err, want)
			}
		})
	}
}

func TestDiffChargesOneBodyEvaluation(t *testing.T) {
	t.Parallel()

	node, err := New("test", "diff(sin(x)*exp(x) + x, x = 1)").Parse()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := node.Eval(withBudgetLimit(context.Background(), 1), NewEnv()); err != nil {
		t.Errorf("a budget of one body evaluation: %v", err)
	}
	if _, err := node.Eval(withBudgetLimit(context.Background(), 0), NewEnv()); !errors.Is(err, ErrEvaluationBudget) {
		t.Errorf("a budget of zero: got %v, want ErrEvaluationBudget", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := node.Eval(ctx, NewEnv()); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled context: got %v, want cancellation", err)
	}
}

func TestDiffIsRegistered(t *testing.T) {
	t.Parallel()

	for _, info := range Functions() {
		if info.Name == "diff" {
			return
		}
	}
	t.Error("Functions() does not list diff")
}

// diffCases gives, for each catalog entry with a derivative rule, calls whose derivative at the
// point is checked against a central difference. A multi-argument entry has a call for each
// argument position.
var diffCases = []struct {
	name, src, at string
}{
	{"sin", "sin(x)", "0.7"},
	{"cos", "cos(x)", "0.7"},
	{"tan", "tan(x)", "0.7"},
	{"asin", "asin(x)", "0.3"},
	{"acos", "acos(x)", "0.3"},
	{"atan", "atan(x)", "0.7"},
	{"atan2", "atan2(x, 2)", "0.7"},
	{"atan2", "atan2(2, x)", "0.7"},
	{"deg2rad", "deg2rad(x)", "0.7"},
	{"rad2deg", "rad2deg(x)", "0.7"},
	{"exp", "exp(x)", "0.7"},
	{"ln", "ln(x)", "0.7"},
	{"log10", "log10(x)", "0.7"},
	{"log2", "log2(x)", "0.7"},
	{"log", "log(x, 3)", "5"},
	{"log", "log(5, x)", "3"},
	{"sqrt", "sqrt(x)", "0.7"},
	{"cbrt", "cbrt(x)", "-0.7"},
	{"hypot", "hypot(x, 2)", "0.7"},
	{"hypot", "hypot(2, x)", "0.7"},
	{"dist", "dist(x, 1, 4, 5)", "2"},
	{"dist", "dist(1, x, 4, 5)", "2"},
	{"dist", "dist(1, 2, x, 5)", "2"},
	{"dist", "dist(1, 2, 4, x)", "2"},
	{"norm", "norm(x)", "0.7"},
	{"norm", "norm(x, 2, 3)", "0.7"},
	{"norm", "norm(2, x, 3)", "0.7"},
	{"sinh", "sinh(x)", "0.7"},
	{"cosh", "cosh(x)", "0.7"},
	{"tanh", "tanh(x)", "0.7"},
	{"asinh", "asinh(x)", "0.7"},
	{"acosh", "acosh(x)", "2"},
	{"atanh", "atanh(x)", "0.3"},
	{"gamma", "gamma(x)", "2.5"},
	{"gamma", "gamma(x)", "-1.5"},
	{"lgamma", "lgamma(x)", "2.5"},
	{"lgamma", "lgamma(x)", "-1.5"},
}

func TestDiffRulesMatchFiniteDifferences(t *testing.T) {
	t.Parallel()

	for _, c := range diffCases {
		c := c
		t.Run(c.src+" at "+c.at, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			got, err := evalFloat(t, ctx, "diff("+c.src+", x = "+c.at+")", NewEnv())
			if err != nil {
				t.Fatal(err)
			}

			const h = "1/100000"
			f := func(at string) float64 { return evalAt(t, c.src, at) }
			want := (f(c.at+" + "+h) - f(c.at+" - "+h)) / 2e-5
			if d := got - want; d > 1e-6 || d < -1e-6 {
				t.Errorf("rule gives %v, central difference gives %v", got, want)
			}
		})
	}
}

// evalAt evaluates src with x set to the expression at.
func evalAt(t *testing.T, src, at string) float64 {
	t.Helper()

	env := NewEnv()
	if _, err := evalFloat(t, context.Background(), "x = "+at, env); err != nil {
		t.Fatal(err)
	}
	v, err := evalFloat(t, context.Background(), src, env)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestEveryCatalogEntryHasADerivativeRuleOrMarker(t *testing.T) {
	t.Parallel()

	covered := map[string]bool{}
	for _, c := range diffCases {
		covered[c.name] = true
	}

	for _, f := range functionCatalog {
		switch {
		case f.partials != nil && f.notDiff != "":
			t.Errorf("%s: has both a derivative rule and a not-differentiable marker", f.name)
		case f.partials == nil && f.notDiff == "":
			t.Errorf("%s: has neither a derivative rule nor a not-differentiable marker", f.name)
		case f.partials != nil && !covered[f.name]:
			t.Errorf("%s: has a derivative rule that diffCases does not check", f.name)
		}
	}
}

func TestEveryOperatorHasAJetCase(t *testing.T) {
	t.Parallel()

	one := &NumberNode{Value: intReal(big.NewInt(1))}
	two := &NumberNode{Value: intReal(big.NewInt(2))}
	env := NewEnv().childJet("x", jv{tag: newTag(), co: []jv{intJ(1), intJ(1)}})
	x := &IdentNode{Name: tokens.Token{Type: tokens.IDENT, Value: "x"}}

	for op := tokens.OP_PLUS; op <= tokens.OP_POW; op++ {
		tok := tokens.Token{Type: op, Value: op.String()}

		bin := &BinaryNode{Op: tok, Left: two, Right: one}
		if _, err := bin.Eval(context.Background(), env); err == nil || !strings.Contains(err.Error(), "unknown operator") {
			jetBin := &BinaryNode{Op: tok, Left: x, Right: x}
			if _, err := jetBin.evalJet(context.Background(), env); err != nil && strings.Contains(err.Error(), "unknown operator") {
				t.Errorf("BinaryNode handles %s but has no jet case", op)
			}
		}

		un := &UnaryNode{Op: tok, Expr: two}
		if _, err := un.Eval(context.Background(), env); err == nil || !strings.Contains(err.Error(), "unknown unary operator") {
			jetUn := &UnaryNode{Op: tok, Expr: x}
			if _, err := jetUn.evalJet(context.Background(), env); err != nil && strings.Contains(err.Error(), "unknown unary operator") {
				t.Errorf("UnaryNode handles %s but has no jet case", op)
			}
		}
	}
}

// jetOf builds a jet of the tag with the given integer coefficients.
func jetOf(tag int64, cs ...int64) jv {
	co := make([]jv, len(cs))
	for i, c := range cs {
		co[i] = intJ(c)
	}
	return jv{tag: tag, co: co}
}

func coefFloat(t *testing.T, v jv, tag int64, k int) float64 {
	t.Helper()

	r := v.coef(tag, k).value()
	rat, err := approxRat(context.Background(), r, -100)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := rat.Float64()
	return f
}

func checkCoefs(t *testing.T, v jv, tag int64, want []float64) {
	t.Helper()

	if got := jetLen(tag, v); got != len(want) {
		t.Fatalf("jet has %d coefficients, want %d", got, len(want))
	}
	for k, w := range want {
		if got := coefFloat(t, v, tag, k); math.Abs(got-w) > 1e-12 {
			t.Errorf("coefficient %d = %v, want %v", k, got, w)
		}
	}
}

// The rules work at every order, though diff exposes only the first.
func TestJetSeriesAtHigherOrders(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := NewEnv()
	tag := newTag()
	x := jetOf(tag, 0, 1, 0, 0, 0)
	one := jetOf(tag, 1, 0, 0, 0, 0)

	tests := []struct {
		name string
		f    func() (jv, error)
		want []float64
	}{
		{"exp", func() (jv, error) { return jetExp(ctx, env, x) }, []float64{1, 1, 0.5, 1.0 / 6, 1.0 / 24}},
		{"sin", func() (jv, error) { return jetSin(ctx, env, x) }, []float64{0, 1, 0, -1.0 / 6, 0}},
		{"cos", func() (jv, error) { return jetCos(ctx, env, x) }, []float64{1, 0, -0.5, 0, 1.0 / 24}},
		{"1/(1-x)", func() (jv, error) { return jdiv(one, jsub(one, x)) }, []float64{1, 1, 1, 1, 1}},
		{"1/(1-x)^2", func() (jv, error) {
			d := jsub(one, x)
			return jdiv(one, jmul(d, d))
		}, []float64{1, 2, 3, 4, 5}},
		{"ln(1+x)", func() (jv, error) { return jetLn(ctx, env, jadd(one, x)) }, []float64{0, 1, -0.5, 1.0 / 3, -0.25}},
		{"sqrt(1+x)", func() (jv, error) { return jetSqrt(ctx, env, jadd(one, x)) }, []float64{1, 0.5, -0.125, 0.0625, -5.0 / 128}},
		{"tan", func() (jv, error) { return jetTan(ctx, env, x) }, []float64{0, 1, 0, 1.0 / 3, 0}},
		{"atan", func() (jv, error) {
			return composeN(ctx, env, "atan", real1((*unified.Real).Atan), unaryPartials(dAtan), []jv{x})
		}, []float64{0, 1, 0, -1.0 / 3, 0}},
		{"(1+x)^0.5", func() (jv, error) { return jetPow(ctx, env, jadd(one, x), scalarJ(ratReal(big.NewRat(1, 2)))) }, []float64{1, 0.5, -0.125, 0.0625, -5.0 / 128}},
		{"(1+x)^-1", func() (jv, error) { return jetPow(ctx, env, jadd(one, x), intJ(-1)) }, []float64{1, -1, 1, -1, 1}},
		{"(1+x)^4", func() (jv, error) { return jetPow(ctx, env, jadd(one, x), intJ(4)) }, []float64{1, 4, 6, 4, 1}},
		{"x^3 at zero", func() (jv, error) { return jetPow(ctx, env, x, intJ(3)) }, []float64{0, 0, 0, 1, 0}},
		{"2^x", func() (jv, error) { return jetPow(ctx, env, intJ(2), x) }, []float64{1, math.Ln2, math.Ln2 * math.Ln2 / 2, math.Pow(math.Ln2, 3) / 6, math.Pow(math.Ln2, 4) / 24}},
		{"(1+x)^(1+x)", func() (jv, error) { return jetPow(ctx, env, jadd(one, x), jadd(one, x)) }, []float64{1, 1, 1, 0.5, 1.0 / 3}},
		{"gamma(1+x)", func() (jv, error) { return jetGamma(ctx, env, jadd(one, x)) }, []float64{1, -eulerGamma, 0.9890559953279725, -0.9074790760808862, 0.9817280868344002}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.f()
			if err != nil {
				t.Fatal(err)
			}
			checkCoefs(t, got, tag, tt.want)
		})
	}
}

// Two tags must not confuse their perturbations: the partial in one variable of a function of
// both is read from the right coefficient.
func TestJetTagsKeepPerturbationsApart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := NewEnv()
	tx, ty := newTag(), newTag()

	// x = 1 + e_x in the outer tag. y = 2 + e_y in the inner tag, whose coefficients hold x.
	x := jetOf(tx, 1, 1)
	y := jv{tag: ty, co: []jv{intJ(2), intJ(1)}}

	t.Run("x*y", func(t *testing.T) {
		t.Parallel()
		p := jmul(x, y)
		dy := p.coef(ty, 1)
		checkCoefs(t, dy, tx, []float64{1, 1})
		dxdy := dy.coef(tx, 1)
		if got := coefFloat(t, dxdy, 0, 0); got != 1 {
			t.Errorf("d2/dxdy = %v, want 1", got)
		}
	})

	t.Run("sin(x*y)", func(t *testing.T) {
		t.Parallel()
		s, err := jetSin(ctx, env, jmul(x, y))
		if err != nil {
			t.Fatal(err)
		}

		// d/dy sin(xy) = x cos(xy), and d/dx of that is cos(xy) - xy sin(xy).
		dy := s.coef(ty, 1)
		checkCoefs(t, dy, tx, []float64{math.Cos(2), math.Cos(2) - 2*math.Sin(2)})
	})

	t.Run("constant in the inner tag", func(t *testing.T) {
		t.Parallel()
		s, err := jetSin(ctx, env, x)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.coef(ty, 1).value(); !got.IsZero() {
			t.Errorf("derivative of a function of x alone in y is nonzero")
		}
		checkCoefs(t, s, tx, []float64{math.Sin(1), math.Cos(1)})
	})

	t.Run("division by a zero in the outer coefficient", func(t *testing.T) {
		t.Parallel()
		_, err := jdiv(intJ(1), jetOf(tx, 0, 1))
		if !errors.Is(err, errDivideByZero) {
			t.Errorf("got %v, want division by zero", err)
		}
	})
}

func TestJetPowNestedTags(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := NewEnv()
	tx, ty := newTag(), newTag()

	// x = 2 + e_x in the outer tag, and y = 3 + e_y in the inner tag.
	x := jetOf(tx, 2, 1)
	y := jv{tag: ty, co: []jv{intJ(3), intJ(1)}}

	tests := []struct {
		name string
		base jv
		exp  jv
		dy   []float64
	}{
		// d/dy x^y = x^y ln x, and d/dx of that is x^(y-1) (1 + y ln x).
		{"x^y", x, y, []float64{8 * math.Ln2, 4 * (1 + 3*math.Ln2)}},
		// d/dy y^x = x y^(x-1), and d/dx of that is y^(x-1) (1 + x ln y).
		{"y^x", y, x, []float64{2 * 3, 3 * (1 + 2*math.Log(3))}},
		{"x^0.5", x, scalarJ(ratReal(big.NewRat(1, 2))), nil},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := jetPow(ctx, env, tt.base, tt.exp)
			if err != nil {
				t.Fatal(err)
			}
			if tt.dy == nil {
				checkCoefs(t, got, tx, []float64{math.Sqrt2, 1 / (2 * math.Sqrt2)})
				return
			}
			checkCoefs(t, got.coef(ty, 1), tx, tt.dy)
		})
	}
}

func TestJetPowIntegerInInnerTagIsConstantInOuter(t *testing.T) {
	t.Parallel()

	tx, ty := newTag(), newTag()
	y := jv{tag: ty, co: []jv{intJ(3), intJ(1)}}

	got, err := jetPow(context.Background(), NewEnv(), y, intJ(3))
	if err != nil {
		t.Fatal(err)
	}
	checkCoefs(t, got, ty, []float64{27, 27})
	if jetLen(tx, got.coef(ty, 1)) != 0 {
		t.Error("d/dy y^3 should not depend on the outer tag")
	}
}

// The order-0 case of a power is the value through Pow, on every path.
func TestJetPowValueIsExact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := NewEnv()
	tag := newTag()
	x := jetOf(tag, 3, 1)

	got, err := jetPow(ctx, env, intJ(2), x)
	if err != nil {
		t.Fatal(err)
	}
	if v := got.coef(tag, 0).value(); !v.Subtract(intJ(8).value()).IsZero() {
		t.Errorf("2**x at 3 is not exactly 8 structurally")
	}
}
