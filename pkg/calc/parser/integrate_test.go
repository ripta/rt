package parser

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
)

func TestIntegrateValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want float64
	}{
		{"integrate(sin(x), x = 0..PI)", 2},
		{"integrate(1/sqrt(x), x = 0..1)", 2},
		{"integrate(sin(x), x = 0..2*PI)", 0},
		{"integrate(sin(x), x = PI..0)", -2},
		{"integrate(x**2, x = 0..3)", 9},
		{"integrate(exp(x), x = 0..1)", 1.718281828459045},
		{"integrate(1/(1 + x*x), x = 0..1)", 0.7853981633974483},
		{"integrate(sqrt(x)*ln(x), x = 0..1)", -4.0 / 9},
		{"integrate(1/sqrt(1 - x*x), x = -1..1)", 3.141592653589793},
		{"integrate(5, x = 2..2)", 0},
		{"integrate(5, x = 0..2)", 10},
		{"integrate(integrate(x*y, y = 0..1), x = 0..1)", 0.25},
		{"integrate(sum(i*x, i = 1..3), x = 0..1)", 3},
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

func TestIntegrateEqualBoundsAreExactZero(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"integrate(1/x, x = 1..1)", "integrate(1/x, x = PI..PI)", "integrate(1/x, x = sqrt(2)..sqrt(2))"} {
		got, err := evalShipping(context.Background(), src, NewEnv())
		if err != nil || got != "0" {
			t.Errorf("%s = %s, %v, want 0", src, got, err)
		}
	}
}

func exactRat(t *testing.T, src string, p int) *big.Rat {
	t.Helper()

	node, err := New("test", src).Parse()
	if err != nil {
		t.Fatal(err)
	}
	v, err := node.Eval(context.Background(), NewEnv())
	if err != nil {
		t.Fatal(err)
	}
	r, err := approxRat(context.Background(), v, p-10)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The estimate must stay within 2^(p+1) of the true value at every precision, including the
// ones that run the quadrature again.
func TestIntegrateSharpens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
		ps   []int
	}{
		{"integrate(sin(x), x = 0..PI)", "2", []int{-100, -300, -1000, -100, -300}},
		{"integrate(1/sqrt(x), x = 0..1)", "2", []int{-100, -300, -1000}},
		{"integrate(1/sqrt(x), x = 0..1)", "2", []int{-300}},
		{"integrate(1/(1 + x*x), x = 0..1)", "PI/4", []int{-100, -300}},
		{"integrate(sin(x), x = 0..PI)", "2", []int{-1000}},
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

func TestIntegrateServesFirstReadFromCache(t *testing.T) {
	t.Parallel()

	ctx := withBudgetLimit(context.Background(), 1_000_000)
	env := NewEnv()
	node, err := New("test", "integrate(sin(x), x = 0..PI)").Parse()
	if err != nil {
		t.Fatal(err)
	}
	v, err := node.Eval(ctx, env)
	if err != nil {
		t.Fatal(err)
	}

	used := ctx.Value(budgetKey{}).(*budget).used
	if used == 0 {
		t.Fatal("the eager run evaluated nothing")
	}
	for _, p := range []int{env.precision, env.precision + 20} {
		if _, err := approxRat(ctx, v, p); err != nil {
			t.Fatal(err)
		}
	}
	if got := ctx.Value(budgetKey{}).(*budget).used; got != used {
		t.Errorf("reads at or above the active precision ran %d more evaluations", got-used)
	}
}

func TestIntegrateDivergent(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"integrate(1/x, x = 0..1)",
		"integrate(1/(sqrt(x)*x**(2/5)), x = 0..1)",
	} {
		_, err := evalShipping(context.Background(), src, NewEnv())
		if err == nil || !strings.Contains(err.Error(), "did not converge") || !strings.Contains(err.Error(), "cap on t") {
			t.Errorf("%s: err = %v, want a did-not-converge error naming the cap", src, err)
		}
	}
}

func TestIntegrateIndistinguishableBoundsError(t *testing.T) {
	t.Parallel()

	_, err := evalShipping(context.Background(), "integrate(10**300*x, x = 0..10**-110)", NewEnv())
	if err == nil || !strings.Contains(err.Error(), "too close to tell apart") {
		t.Errorf("err = %v, want a bounds error", err)
	}
}

func TestIntegrateErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"interior pole", "integrate(1/(x - 1/2), x = 0..1)", []string{"x = 0.5", "Split the range"}},
		{"infinite bound", "integrate(x, x = 0..inf)", []string{"infinite bound"}},
		{"infinite low bound", "integrate(exp(x), x = -inf..0)", []string{"infinite bound"}},
		{"assignment in body", "integrate((a = 2) + x, x = 0..1)", []string{"assignment"}},
		{"nested pole", "integrate(integrate(1/(x - y), y = 0..1), x = 0..1)", []string{"Split the range"}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := evalShipping(context.Background(), tt.src, NewEnv())
			if err == nil {
				t.Fatal("no error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("err = %q, want containing %q", err, w)
				}
			}
		})
	}
}

func TestIntegrateSnapshotIsolation(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	for _, src := range []string{"a = 4", "r = integrate(a*x, x = 0..1)", "a = 9"} {
		if _, err := evalShipping(context.Background(), src, env); err != nil {
			t.Fatal(err)
		}
	}

	r, ok := env.Get("r")
	if !ok {
		t.Fatal("r undefined")
	}
	rat, err := approxRat(context.Background(), r, -400)
	if err != nil {
		t.Fatal(err)
	}
	diff := new(big.Rat).Sub(rat, big.NewRat(2, 1))
	if diff.Abs(diff).Cmp(ratPow2(-390)) > 0 {
		t.Errorf("r = %s after a = 9, want 2", fmtRat(rat))
	}
}

func TestIntegrateBudgetAndCancel(t *testing.T) {
	t.Parallel()

	_, err := evalShipping(withBudgetLimit(context.Background(), 10), "integrate(sin(x), x = 0..PI)", NewEnv())
	if !errors.Is(err, ErrEvaluationBudget) {
		t.Errorf("err = %v, want budget error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = evalShipping(ctx, "integrate(sin(x), x = 0..PI)", NewEnv())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want cancellation", err)
	}
}

// Refinement runs after integrate returns, so a budget or cancellation there surfaces from the
// approximation.
func TestIntegrateRefinementErrors(t *testing.T) {
	t.Parallel()

	t.Run("budget", func(t *testing.T) {
		t.Parallel()
		ctx := withBudgetLimit(context.Background(), 400)
		node, err := New("test", "integrate(sin(x), x = 0..PI)").Parse()
		if err != nil {
			t.Fatal(err)
		}
		v, err := node.Eval(ctx, NewEnv())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := approxRat(ctx, v, -1000); !errors.Is(err, ErrEvaluationBudget) {
			t.Errorf("err = %v, want budget error", err)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		t.Parallel()
		node, err := New("test", "integrate(sin(x), x = 0..PI)").Parse()
		if err != nil {
			t.Fatal(err)
		}
		v, err := node.Eval(context.Background(), NewEnv())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := approxRat(ctx, v, -1000); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want cancellation", err)
		}
	})
}

// A nested integral must run the inner quadrature under the outer's context, or the inner
// approximation is rejected as reentrant.
func TestIntegrateNestedRefines(t *testing.T) {
	t.Parallel()

	node, err := New("test", "integrate(integrate(x*y, y = 0..1), x = 0..1)").Parse()
	if err != nil {
		t.Fatal(err)
	}
	v, err := node.Eval(context.Background(), NewEnv())
	if err != nil {
		t.Fatal(err)
	}
	got, err := approxRat(context.Background(), v, -155)
	if err != nil {
		t.Fatal(err)
	}
	diff := new(big.Rat).Sub(got, big.NewRat(1, 4))
	if diff.Abs(diff).Cmp(ratPow2(-154)) > 0 {
		t.Errorf("got %s, want 1/4", fmtRat(got))
	}
}

func BenchmarkIntegrateNested(b *testing.B) {
	for _, src := range []string{
		"integrate(integrate(x*y, y = 0..1), x = 0..1)",
		"integrate(integrate(sin(x*y), y = 0..1), x = 0..1)",
		"integrate(integrate(exp(-x*y), y = 0..1), x = 0..1)",
		"integrate(integrate(1/sqrt(x + y), y = 0..1), x = 0..1)",
	} {
		b.Run(src, func(b *testing.B) {
			var used int
			for range b.N {
				ctx := withBudgetLimit(context.Background(), 1<<40)
				if _, err := evalShipping(ctx, src, NewEnv()); err != nil {
					b.Fatal(err)
				}
				used = ctx.Value(budgetKey{}).(*budget).used
			}
			b.ReportMetric(float64(used), "evals/op")
		})
	}
}
