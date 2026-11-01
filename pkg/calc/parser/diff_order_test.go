package parser

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestDiffOrderParse(t *testing.T) {
	t.Parallel()

	good := []string{
		"diff(x, 0, x = 1)",
		"diff(x, 1, x = 1)",
		"diff(x, 12, x = 1)",
		"diff(x, 1_0, x = 1)",
	}
	for _, src := range good {
		if _, err := New("test", src).Parse(); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}

	bad := []struct{ src, want string }{
		{"diff(x, 13, x = 1)", "at most 12"},
		{"diff(x, 100000000000000000000, x = 1)", "at most 12"},
		{"diff(x, k, x = 1)", "integer literal"},
		{"diff(x, 1+1, x = 1)", "integer literal"},
		{"diff(x, 2.5, x = 1)", "integer literal"},
		{"diff(x, 2.0, x = 1)", "integer literal"},
		{"diff(x, -1, x = 1)", "integer literal"},
		{"diff(x, PI, x = 1)", "integer literal"},
		{"diff(x, 2, 3, x = 1)", "expects 2 arguments"},
		{"diff(x, 2)", "needs a binding"},
		{"diff(x, x = 1, 2)", "the binding argument must be the last"},
		{"sin(2, x)", "expects 1 argument"},
		{"sum(x, 2, x = 1..3)", "expects 2 arguments"},
		{"integrate(x, 2, x = 1..3)", "expects 2 arguments"},
	}
	for _, tt := range bad {
		_, err := New("test", tt.src).Parse()
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: got %v, want an error containing %q", tt.src, err, tt.want)
		}
	}
}

func TestDiffOrderExact(t *testing.T) {
	t.Parallel()

	tests := []struct{ src, want string }{
		{"diff(x**3, 2, x = 2)", "12"},
		{"diff(x**3, 1, x = 2)", "12"},
		{"diff(x**3, 3, x = 2)", "6"},
		{"diff(x**3, 4, x = 2)", "0"},
		{"diff(x**5, 5, x = 2)", "120"},
		{"diff(x**12, 12, x = 2)", "479001600"},
		{"diff(x*x, 0, x = 3)", "9"},
		{"diff(5, 0, x = 3)", "5"},
		{"diff(sum(i, i = 1..3) + x, 0, x = 2)", "8"},
		{"diff(1/x, 3, x = 2)", "-3/8"},
		{"diff(x**-2, 2, x = 2)", "3/8"},
		{"diff(x**3, 2, x = -2)", "-12"},
		{"diff(x**3, 3, x = 0)", "6"},
		{"diff(x**3, 2, x = 0)", "0"},
		{"diff(x**0, 2, x = 0)", "0"},
		{"diff(x**2.5, 2, x = 4)", "15/2"},
		{"diff(x**0.5, 3, x = 4)", "3/256"},
		{"diff(sqrt(x), 2, x = 4)", "-1/32"},
		{"diff(abs(x*x), 2, x = -3)", "2"},
		{"diff(abs(x**3), 2, x = -2)", "12"},
		{"diff(abs(x), 2, x = 3)", "0"},
		{"diff(x*floor(x), 2, x = 2.5)", "0"},
		{"diff(x % 3, 2, x = 7)", "0"},
		{"diff(x*(x % 3), 2, x = 7.5)", "2"},
		{"diff(10 % x, 2, x = 3)", "0"},
		{"diff(max(x*x, 2), 2, x = 3)", "2"},
		{"diff(max(x*x, 2), 2, x = 1)", "0"},
		{"diff(min(x*x, 20), 3, x = 3)", "0"},
		{"diff((x << 3)*x, 2, x = 1)", "16"},
		{"diff(signum(x)*x*x, 2, x = -3)", "-2"},
		{"diff(x**3 + 3*x, 2, x = 1/3)", "2"},
	}
	for _, tt := range tests {
		src, want := tt.src, tt.want
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got, err := evalShipping(context.Background(), src, NewEnv())
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
}

const zeta3 = 1.2020569031595942

func TestDiffOrderKnownValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want float64
	}{
		{"diff(exp(x), 5, x = 1)", math.E},
		{"diff(exp(sin(x)), 2, x = 0)", 1},
		{"diff(sin(x), 3, x = 0)", -1},
		{"diff(sin(x), 4, x = PI/2)", 1},
		{"diff(cos(x), 2, x = 0)", -1},
		{"diff(tan(x), 3, x = 0)", 2},
		{"diff(ln(x), 3, x = 2)", 0.25},
		{"diff(asin(x), 3, x = 0)", 1},
		{"diff(acos(x), 3, x = 0)", -1},
		{"diff(atan(x), 3, x = 0)", -2},
		{"diff(atan2(x, 1), 3, x = 0)", -2},
		{"diff(atan2(1, x), 3, x = 0)", 2},
		{"diff(sinh(x), 3, x = 0)", 1},
		{"diff(cosh(x), 2, x = 0)", 1},
		{"diff(tanh(x), 3, x = 0)", -2},
		{"diff(asinh(x), 3, x = 0)", -1},
		{"diff(atanh(x), 3, x = 0)", 2},
		{"diff(acosh(x), 2, x = 2)", -2 / (3 * math.Sqrt(3))},
		{"diff(log10(x), 2, x = 1)", -1 / math.Ln10},
		{"diff(log2(x), 2, x = 1)", -1 / math.Ln2},
		{"diff(log(x, 2), 2, x = 1)", -1 / math.Ln2},
		{"diff(log(8, x), 2, x = 2)", 3 * (math.Ln2 + 2) / (4 * math.Ln2 * math.Ln2)},
		{"diff(hypot(x, 4), 2, x = 3)", 16.0 / 125},
		{"diff(hypot(3, x), 2, x = 4)", 9.0 / 125},
		{"diff(dist(0, 0, x, 4), 2, x = 3)", 16.0 / 125},
		{"diff(norm(x, 4, 12), 2, x = 3)", 160.0 / 2197},
		{"diff(deg2rad(x), 2, x = 7)", 0},
		{"diff(rad2deg(x), 2, x = 7)", 0},
		{"diff(cbrt(x), 2, x = 8)", -1.0 / 144},
		{"diff(gamma(x), 2, x = 1)", eulerGamma*eulerGamma + math.Pi*math.Pi/6},
		{"diff(gamma(x), 3, x = 1)", -(math.Pow(eulerGamma, 3) + eulerGamma*math.Pi*math.Pi/2 + 2*zeta3)},
		{"diff(lgamma(x), 2, x = 1)", math.Pi * math.Pi / 6},
		{"diff(lgamma(x), 3, x = 1)", -2 * zeta3},
		{"diff(lgamma(x), 2, x = 2)", math.Pi*math.Pi/6 - 1},
		{"diff(lgamma(x), 4, x = 1)", math.Pow(math.Pi, 4) / 15},
		{"diff(2**x, 3, x = 0)", math.Pow(math.Ln2, 3)},
		{"diff(x**x, 2, x = 1)", 2},
		{"diff(x**PI, 2, x = 2)", math.Pi * (math.Pi - 1) * math.Pow(2, math.Pi-2)},
		{"diff(x**sin(x), 2, x = 1)", math.Pow(math.Sin(1), 2) + 2*math.Cos(1) - math.Sin(1)},
	}
	for _, tt := range tests {
		src, want := tt.src, tt.want
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got, err := evalFloat(t, context.Background(), src, NewEnv())
			if err != nil {
				t.Fatal(err)
			}
			if d := math.Abs(got - want); d > 1e-9*math.Max(1, math.Abs(want)) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// Every rule with a derivative at order 2 and 3 agrees with a central difference of the next lower
// order.
func TestDiffHigherOrdersMatchFiniteDifferences(t *testing.T) {
	t.Parallel()

	for _, c := range diffCases {
		c := c
		for _, order := range []string{"2", "3"} {
			order := order
			t.Run(order+" "+c.src+" at "+c.at, func(t *testing.T) {
				t.Parallel()

				// A kink or a step has no second derivative to compare with a difference.
				switch c.name {
				case "abs", "signum", "floor", "ceil", "round", "trunc", "min", "max":
					return
				}

				got, err := evalFloat(t, context.Background(), "diff("+c.src+", "+order+", x = "+c.at+")", NewEnv())
				if err != nil {
					t.Fatal(err)
				}

				lower := "1"
				if order == "3" {
					lower = "2"
				}
				const h = "1/1000"
				f := func(at string) float64 {
					v, err := evalFloat(t, context.Background(), "diff("+c.src+", "+lower+", x = "+at+")", NewEnv())
					if err != nil {
						t.Fatal(err)
					}
					return v
				}
				want := (f(c.at+" + "+h) - f(c.at+" - "+h)) / 2e-3
				if d := math.Abs(got - want); d > 1e-4*math.Max(1, math.Abs(want)) {
					t.Errorf("rule gives %v, central difference gives %v", got, want)
				}
			})
		}
	}
}

func TestDiffNestedAndMixed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want float64
	}{
		{"diff(diff(x*y, x = 1), y = 2)", 1},
		{"diff(diff(x*y, y = 2), x = 1)", 1},
		{"diff(diff(sin(x*y), x = 1), y = 2)", math.Cos(2) - 2*math.Sin(2)},
		{"diff(diff(sin(x*y), y = 2), x = 1)", math.Cos(2) - 2*math.Sin(2)},
		{"diff(diff(x**2 * y**3, x = 1), y = 2)", 24},
		{"diff(diff(x**3 * y**2, 2, x = 1), 2, y = 1)", 12},
		{"diff(diff(diff(x*y*z, x = 1), y = 2), z = 3)", 1},
		{"diff(diff(exp(x*y), x = 1), y = 2)", math.Exp(2) * (1 + 2)},

		// Order 0 in the inner call is the body at the point.
		{"diff(diff(x*y, 0, x = 1), y = 2)", 1},
		{"diff(diff(x*y, 0, y = 2), x = 1)", 2},

		// The inner point may name the outer variable, which the inner body then shadows.
		{"diff(diff(x**2, x = x), x = 3)", 2},
		{"diff(diff(x**3, x = x), x = 2)", 12},
		{"diff(diff(sin(x), x = x), x = 1)", -math.Sin(1)},
		{"diff(diff(x**3, x = 2), x = 5)", 0},
		{"diff(diff(x*y, x = y), y = 3)", 1},

		// A tower of the same name equals the higher order.
		{"diff(diff(diff(x**4, x = x), x = x), x = 2)", 48},
		{"diff(diff(exp(sin(x)), x = x), x = 1)", secondDerivExpSin(1)},
	}
	for _, tt := range tests {
		src, want := tt.src, tt.want
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got, err := evalFloat(t, context.Background(), src, NewEnv())
			if err != nil {
				t.Fatal(err)
			}
			if d := math.Abs(got - want); d > 1e-9 {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func secondDerivExpSin(x float64) float64 {
	return math.Exp(math.Sin(x)) * (math.Cos(x)*math.Cos(x) - math.Sin(x))
}

// The classic perturbation confusion: the inner derivative is of the inner variable only, so an
// outer perturbation must not leak into it.
func TestDiffTagsPreventPerturbationConfusion(t *testing.T) {
	t.Parallel()

	tests := []struct{ src, want string }{
		{"diff(x * diff(x + y, y = 1), x = 1)", "1"},
		{"diff(x * diff(x * y, y = 2), x = 1)", "2"},
		{"diff(x * diff(x * y, y = x), x = 3)", "6"},
		{"diff(x + diff(x * x, x = 4), x = 1)", "1"},
	}
	for _, tt := range tests {
		src, want := tt.src, tt.want
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got, err := evalShipping(context.Background(), src, NewEnv())
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
}

func TestDiffNestedKinkThroughRealCalls(t *testing.T) {
	t.Parallel()

	_, err := evalShipping(context.Background(), "diff(diff(abs(y), x = 1), y = 0)", NewEnv())
	if err == nil || !strings.Contains(err.Error(), "abs is not differentiable at 0") {
		t.Errorf("got %v, want the abs kink", err)
	}

	got, err := evalShipping(context.Background(), "diff(diff(abs(y), x = 1), y = 3)", NewEnv())
	if err != nil || got != "0" {
		t.Errorf("got %s, %v, want 0", got, err)
	}

	got, err = evalShipping(context.Background(), "diff(diff(abs(x*y), x = 1), y = -2)", NewEnv())
	if err != nil || got != "-1" {
		t.Errorf("got %s, %v, want -1", got, err)
	}
}

func TestDiffNestedErrorsAndLimits(t *testing.T) {
	t.Parallel()

	// An inner binding that is not a point, or that is the integer-only argument.
	for _, src := range []string{
		"diff(diff(x, x = 1..2), x = 1)",
		"diff(diff(factorial(y), x = 1), y = 3)",
	} {
		if _, err := New("test", src).Parse(); err == nil {
			t.Errorf("%s: expected a parse error", src)
		}
	}

	// Each call, inner or outer, costs one body evaluation, whatever its order.
	node, err := New("test", "diff(diff(x*y, 3, x = 1), 2, y = 2)").Parse()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := node.Eval(withBudgetLimit(context.Background(), 2), NewEnv()); err != nil {
		t.Errorf("a budget of two body evaluations: %v", err)
	}
	if _, err := node.Eval(withBudgetLimit(context.Background(), 1), NewEnv()); err == nil {
		t.Error("a budget of one: expected the budget error")
	}
}
