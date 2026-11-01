package parser

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ripta/reals/pkg/unified"
)

func TestDiffThroughSumAndProduct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
	}{
		{"diff(sum(i*x**2, i = 1..3), x = 2)", "24"},
		{"diff(product(x + i, i = 1..3), x = 0)", "11"},
		{"diff(sum(x**i, i = 1..4), 2, x = 1)", "20"},
		{"diff(sum(i*x, i = 1..1000), x = 1)", "500500"},
		{"diff(product(x, i = 1..5), x = 2)", "80"},
		{"diff(diff(sum(x*y*i, i = 1..3), x = 1), y = 2)", "6"},
		{"diff(diff(sum(x*i, i = 1..3), 0, y = 1), x = 1)", "6"},
		{"diff(sum(x, x = 1..3), x = 2)", "0"},
		{"diff(sum(x, x = 1..3) * x, x = 2)", "6"},
		{"diff(sum(i, i = 3..1) + x, x = 2)", "1"},
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

func TestDiffThroughIntegrate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
	}{
		{"diff(integrate(sin(t), t = 0..x), x = 1)", "sin(1)"},
		{"diff(integrate(t**2, t = 0..x), x = 2)", "4"},
		{"diff(integrate(x*t, t = 0..1), x = 3)", "1/2"},
		{"diff(integrate(sin(x*t), t = 0..1), x = 2)", "sin(2)/2 + cos(2)/4 - 1/4"},
		{"diff(integrate(t, t = x..x**2), x = 2)", "14"},
		{"diff(integrate(t**2 + x, t = 0..x), x = 1)", "3"},
		{"diff(integrate(exp(-t), t = x..inf), x = 1)", "-exp(-1)"},
		{"diff(integrate(exp(-t*x), t = 0..inf), x = 2)", "-1/4"},
		{"diff(integrate(exp(t), t = -inf..x), x = 1)", "E"},
		{"diff(integrate(exp(-t), t = inf..x), x = 1)", "exp(-1)"},
		{"diff(integrate(exp(t), t = x..-inf), x = 0)", "-1"},
		{"diff(integrate(exp(-(t**2)), t = -inf..inf) * x, x = 1)", "sqrt(PI)"},
		{"diff(integrate(1/(1 + t*t), t = x..x + 1), x = 0)", "-1/2"},
		{"diff(integrate(sin(t), t = 0..x), 2, x = 1)", "cos(1)"},
		{"diff(integrate(sin(t), t = 0..x), 3, x = 1)", "-sin(1)"},
		{"diff(integrate(t**3, t = 0..x**2), 2, x = 1)", "14"},
		{"diff(diff(integrate(x*y*t, t = 0..1), x = 1), y = 2)", "1/2"},
		{"diff(diff(integrate(t, t = 0..x*y), x = 1), y = 2)", "4"},
		{"diff(integrate(x, t = 2..2), x = 1)", "0"},
		{"diff(integrate(x/t, t = 0..0), x = 1)", "0"},
		{"diff(integrate(x/t, t = PI..PI), x = 1)", "0"},
	}
	for _, tt := range tests {
		src, want := tt.src, tt.want
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got, err := evalFloat(t, context.Background(), src, NewEnv())
			if err != nil {
				t.Fatal(err)
			}
			w, _ := exactRat(t, want, -60).Float64()
			if d := got - w; d > 1e-10 || d < -1e-10 {
				t.Errorf("got %v, want %v", got, w)
			}
		})
	}
}

// A width that is zero in value but not in derivative must still give the derivative of the
// integral from a moving bound to a fixed one.
func TestDiffThroughIntegrateWithVanishingWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
	}{
		{"diff(integrate(t**2 + 1, t = x..2), x = 2)", "-5"},
		{"diff(integrate(t**2 + 1, t = 2..x), x = 2)", "5"},
		{"diff(integrate(exp(-t), t = x..inf), 2, x = 0)", "1"},
	}
	for _, tt := range tests {
		got, err := evalFloat(t, context.Background(), tt.src, NewEnv())
		w, _ := exactRat(t, tt.want, -60).Float64()
		if err != nil || got-w > 1e-10 || w-got > 1e-10 {
			t.Errorf("%s = %v, %v, want %v", tt.src, got, err, w)
		}
	}
}

// Every coefficient must hold to 2^(p+1) at each precision, including the ones that run the
// quadrature again.
func TestDiffThroughBindersSharpens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
		ps   []int
	}{
		{"diff(integrate(sin(t), t = 0..x), x = 1)", "sin(1)", []int{-100, -300, -1000}},
		{"diff(integrate(exp(-t*x), t = 0..inf), x = 2)", "-1/4", []int{-100, -300}},
		{"diff(integrate(t, t = x..x**2), x = 2)", "14", []int{-100, -300, -1000}},
		{"diff(solve(t**2 - x, t = 0..10), x = 2)", "1/(2*sqrt(2))", []int{-100, -300, -1000}},
		{"diff(solve(t**2 - x, t = 0..10), 3, x = 4)", "3/256", []int{-100, -300, -1000}},
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
			for _, p := range tt.ps {
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

func TestDiffThroughSolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want float64
	}{
		{"diff(solve(t**2 - x, t = 0..10), x = 4)", 0.25},
		{"diff(solve(t**2 - x, t = 0..10), 2, x = 4)", -1.0 / 32},
		{"diff(solve(t**2 - x, t = 0..10), 3, x = 4)", 3.0 / 256},
		{"diff(solve(t**2 - x, t = 1), x = 4)", 0.25},
		{"diff(solve(t**3 + t - x, t = 0..5), x = 2)", 0.25},
		{"diff(solve(t**2 - x, t = 0..10) * x, x = 4)", 3},
		{"diff(diff(solve(t**2 - x*y, t = 0..10), x = 4), y = 1)", 0.125},
		{"diff(solve(t - 1, t = 0..2) + x, x = 1)", 1},
		{"diff(integrate(t, t = 0..solve(s**2 - x, s = 0..10)), x = 4)", 0.5},
	}
	for _, tt := range tests {
		src, want := tt.src, tt.want
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got, err := evalFloat(t, context.Background(), src, NewEnv())
			if err != nil {
				t.Fatal(err)
			}
			if d := got - want; d > 1e-9 || d < -1e-9 {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestDiffThroughSolveAtAVanishingSlope(t *testing.T) {
	t.Parallel()

	tests := []string{
		"diff(solve(t**3 - x, t = -1..1), x = 0)",
		"diff(solve(t**3 - x, t = -1..1), 2, x = 0)",
		"diff(solve(x*t**3, t = -1..1), x = 1)",
	}
	for _, src := range tests {
		_, err := evalShipping(context.Background(), src, NewEnv())
		if err == nil || !strings.Contains(err.Error(), "slope of the body") {
			t.Errorf("%s: err = %v, want a slope error", src, err)
		}
	}
}

// A node of the substituted unit range is reported as the user's variable.
func TestDiffThroughBindersBodyFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want []string
	}{
		{"diff(integrate(abs(t - x), t = 0..2), x = 1)", []string{"the body failed at t = 1:", "abs is not differentiable"}},
		{"diff(integrate(x/(t - 2), t = 0..4), x = 1)", []string{"the body failed at t = 2:", "division by zero"}},
		{"diff(integrate(abs(t)*exp(-t), t = x..inf), x = 0)", []string{"the body failed at t = 0:", "abs is not differentiable"}},
	}
	for _, tt := range tests {
		_, err := evalShipping(context.Background(), tt.src, NewEnv())
		for _, w := range tt.want {
			if err == nil || !strings.Contains(err.Error(), w) {
				t.Errorf("%s: err = %v, want it to contain %q", tt.src, err, w)
			}
		}
	}
}

func TestParseRejectsBoundsOfTheDiffVariable(t *testing.T) {
	t.Parallel()

	rejected := []string{
		"diff(sum(x, x = 1..x), x = 2)",
		"diff(sum(i, i = 1..x), x = 3)",
		"diff(product(i, i = x..3), x = 3)",
		"diff(diff(sum(i, i = 1..x), y = 1), x = 2)",
		"diff(diff(sum(i, i = 1..y), y = 1), x = 2)",
		"diff(sum(sum(j, j = 1..x), i = 1..3), x = 2)",
		"diff(sum(i, i = 1..floor(x)), x = 2)",
	}
	for _, src := range rejected {
		_, err := New("test", src).Parse()
		if err == nil || !strings.Contains(err.Error(), "cannot appear in a bound of") {
			t.Errorf("%s: err = %v, want a bound error", src, err)
		}
	}

	accepted := []string{
		"diff(sum(x, x = 1..3), x = 2)",
		"diff(sum(i*x, i = 1..3), x = 2)",
		"diff(sum(x, x = 1..3) * x, x = 2)",
		"diff(integrate(t, t = 0..x), x = 2)",
		"diff(sum(i, i = 1..3), x = 2)",
	}
	for _, src := range accepted {
		if _, err := New("test", src).Parse(); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}

func TestDiffThroughBindersBudgetAndCancel(t *testing.T) {
	t.Parallel()

	srcs := []string{
		"diff(integrate(sin(t), t = 0..x), x = 1)",
		"diff(solve(t**2 - x, t = 0..10), x = 4)",
		"diff(sum(x**i, i = 1..100), x = 1)",
	}
	for _, src := range srcs {
		_, err := evalShipping(withBudgetLimit(context.Background(), 5), src, NewEnv())
		if !errors.Is(err, ErrEvaluationBudget) {
			t.Errorf("%s: err = %v, want a budget error", src, err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = evalShipping(ctx, src, NewEnv())
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: err = %v, want cancellation", src, err)
		}
	}
}

// Outputs of one integral share every sample, and each is estimated to the same tolerance.
func TestIntegralOutputsShareASample(t *testing.T) {
	t.Parallel()

	run := func(n int) (int, []*big.Int) {
		samples := 0
		q := &integral{
			env:  NewEnv(),
			name: "x",
			n:    n,
			eval: func(_ context.Context, _ *Env, x *big.Rat) ([]*unified.Real, error) {
				samples++
				sq := ratReal(x).Multiply(ratReal(x))
				return []*unified.Real{sq, sq.Multiply(ratReal(x))}[:n], nil
			},
		}
		unit := Binding{Name: "x", Range: true, Lo: Bound{Value: unified.Zero()}, Hi: Bound{Value: intReal(big.NewInt(1))}}
		if _, err := q.configure(context.Background(), -100, unit); err != nil {
			t.Fatal(err)
		}
		got, err := q.approximate(context.Background(), -100)
		if err != nil {
			t.Fatal(err)
		}
		return samples, got
	}

	one, _ := run(1)
	two, got := run(2)
	if one != two {
		t.Errorf("two outputs took %d samples, one took %d", two, one)
	}
	for i, want := range []*big.Rat{big.NewRat(1, 3), big.NewRat(1, 4)} {
		est := new(big.Rat).SetFrac(got[i], new(big.Int).Lsh(big.NewInt(1), 100))
		if d := new(big.Rat).Sub(est, want); d.Abs(d).Cmp(ratPow2(-98)) > 0 {
			t.Errorf("output %d = %s, want %s", i, fmtRat(est), fmtRat(want))
		}
	}
}

// The message names the range the user wrote, not the unit range the substitution runs over.
func TestDiffThroughIntegrateNonConvergenceNamesTheUserRange(t *testing.T) {
	t.Parallel()

	_, err := evalShipping(context.Background(), "diff(integrate(1/(t + 1), t = -1..x), x = 1/2)", NewEnv())
	if err == nil || !strings.Contains(err.Error(), "did not converge on t = -1..0.5") {
		t.Errorf("err = %v, want the user's range named", err)
	}
}

// A coefficient that converges more slowly than the value must hold the run back, both in when a
// side ends and in when the levels agree.
func TestIntegralWaitsForTheSlowestOutput(t *testing.T) {
	t.Parallel()

	node, err := New("test", "1/sqrt(x)").Parse()
	if err != nil {
		t.Fatal(err)
	}
	q := &integral{
		env:  NewEnv(),
		name: "x",
		n:    2,
		eval: func(ctx context.Context, env *Env, x *big.Rat) ([]*unified.Real, error) {
			v, err := evalBody(ctx, env, node, "x", ratReal(x))
			return []*unified.Real{unified.Zero(), v}, err
		},
	}
	unit := Binding{Name: "x", Range: true, Lo: Bound{Value: unified.Zero()}, Hi: Bound{Value: intReal(big.NewInt(1))}}
	if _, err := q.configure(context.Background(), -100, unit); err != nil {
		t.Fatal(err)
	}
	got, err := q.approximate(context.Background(), -100)
	if err != nil {
		t.Fatal(err)
	}

	for i, want := range []int64{0, 2} {
		est := new(big.Rat).SetFrac(got[i], new(big.Int).Lsh(big.NewInt(1), 100))
		if d := new(big.Rat).Sub(est, big.NewRat(want, 1)); d.Abs(d).Cmp(ratPow2(-98)) > 0 {
			t.Errorf("output %d = %s, want %d", i, fmtRat(est), want)
		}
	}
}
