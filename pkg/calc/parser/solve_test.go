package parser

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ripta/reals/pkg/unified"
)

func evalFloat(t *testing.T, ctx context.Context, src string, env *Env) (float64, error) {
	t.Helper()

	node, err := New("test", src).Parse()
	if err != nil {
		return 0, err
	}
	v, err := node.Eval(ctx, env)
	if err != nil {
		return 0, err
	}
	rat, err := approxRat(ctx, v, env.precision)
	if err != nil {
		return 0, err
	}
	f, _ := rat.Float64()
	return f, nil
}

func TestSolveRoots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want float64
	}{
		{"solve(x**2 - 2, x = 0..2)", 1.4142135623730951},
		{"solve(x**2 - 2, x = 2..0)", 1.4142135623730951},
		{"solve(cos(x) - x, x = 0..1)", 0.7390851332151607},
		{"solve(x**3, x = -1..2)", 0},
		{"solve(x - 1, x = 1..2)", 1},
		{"solve(x - 1, x = 0..1)", 1},
		{"solve(x - 1/3, x = 1/3..1)", 1.0 / 3},
		{"solve(sum(i*x, i = 1..3) - 6, x = 0..3)", 1},
		{"solve(solve(y - x, y = 0..4) - 1, x = 0..3)", 1},
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

func TestSolveExactZeroIsExact(t *testing.T) {
	t.Parallel()

	for src, want := range map[string]string{
		"solve(x - 1, x = 1..2)":     "1",
		"solve(x - 2, x = 1..2)":     "2",
		"solve(x - 1, x = 0..3)":     "1",
		"solve(x*3 - 3, x = 0..2.5)": "1",
	} {
		got, err := evalShipping(context.Background(), src, NewEnv())
		if err != nil || got != want {
			t.Errorf("%s = %s, %v, want %s", src, got, err, want)
		}
	}
}

func TestSolveErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"near-zero endpoint", "solve(sin(x), x = 0..1)", []string{"too close to zero", "x = 0", "Move the bound"}},
		{"no sign change with hints", "solve(x**2 - 1, x = -2..2)", []string{"no sign change on x = -2..2", "is 3 at -2 and 3 at 2", "Sign changes near x = -1 and x = 1", "Try x = -1.25..-0.75"}},
		{"even multiplicity", "solve(x**2, x = -1..1)", []string{"is 1 at -1 and 1 at 1", "even multiplicity"}},
		{"no root at all", "solve(x**2 + 1, x = -1..1)", []string{"even multiplicity"}},
		{"tan pole", "solve(tan(x), x = 1..2)", []string{"pole or jump"}},
		{"reciprocal pole", "solve(1/x, x = -1..1)", []string{"pole or jump"}},
		{"jump", "solve(floor(x) - 0.5, x = 0..2)", []string{"pole or jump"}},
		{"small jump", "solve(floor(x) + x - 1.5, x = 0..2)", []string{"pole or jump"}},
		{"infinite bound", "solve(x, x = 0..inf)", []string{"infinite bound"}},
		{"infinite low bound", "solve(x, x = -inf..1)", []string{"infinite bound"}},
		{"point form", "solve(x - 1, x = 1)", []string{"range binding"}},
		{"assignment in body", "solve((a = 2) + x, x = 0..1)", []string{"assignment"}},
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

func TestSolveSnapshotIsolation(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	for _, src := range []string{"a = 4", "r = solve(x**2 - a, x = 0..10)", "a = 9"} {
		if _, err := evalShipping(context.Background(), src, env); err != nil {
			t.Fatal(err)
		}
	}

	// r is stored as a lazy root. A finer read must still see a = 4.
	r, ok := env.Get("r")
	if !ok {
		t.Fatal("r undefined")
	}
	rat, err := approxRat(context.Background(), r, -400)
	if err != nil {
		t.Fatal(err)
	}
	two := new(big.Rat).SetInt64(2)
	diff := new(big.Rat).Sub(rat, two)
	if diff.Abs(diff).Cmp(ratPow2(-390)) > 0 {
		t.Errorf("r = %s after a = 9, want 2", fmtRat(rat))
	}
}

func TestSolveSharpens(t *testing.T) {
	t.Parallel()

	node, err := New("test", "solve(x**2 - 2, x = 0..2)").Parse()
	if err != nil {
		t.Fatal(err)
	}
	env := NewEnv()
	v, err := node.Eval(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}

	sqrt2, err := approxRat(context.Background(), sqrt2Real(), -1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []int{-100, -300, -1000} {
		got, err := approxRat(context.Background(), v, p)
		if err != nil {
			t.Fatal(err)
		}
		diff := new(big.Rat).Sub(got, sqrt2)
		if diff.Abs(diff).Cmp(ratPow2(p+1)) > 0 {
			t.Errorf("p=%d: error too large", p)
		}
	}
}

func TestSolveBudgetAndCancel(t *testing.T) {
	t.Parallel()

	_, err := evalShipping(withBudgetLimit(context.Background(), 10), "solve(x**2 - 2, x = 0..2)", NewEnv())
	if !errors.Is(err, ErrEvaluationBudget) {
		t.Errorf("err = %v, want budget error", err)
	}

	// A failed scan counts its samples too.
	_, err = evalShipping(withBudgetLimit(context.Background(), 5), "solve(x**2 - 1, x = -2..2)", NewEnv())
	if !errors.Is(err, ErrEvaluationBudget) {
		t.Errorf("scan: err = %v, want budget error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = evalShipping(ctx, "solve(x**2 - 2, x = 0..2)", NewEnv())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want cancellation", err)
	}
}

// Refinement runs after solve returns, so a budget or cancellation there surfaces from the
// approximation.
func TestSolveRefinementErrors(t *testing.T) {
	t.Parallel()

	t.Run("budget", func(t *testing.T) {
		t.Parallel()
		ctx := withBudgetLimit(context.Background(), 400)
		node, _ := New("test", "solve(x**2 - 2, x = 0..2)").Parse()
		v, err := node.Eval(ctx, NewEnv())
		if err != nil {
			t.Fatalf("eager narrowing: %v", err)
		}
		if _, err := approxRat(ctx, v, -5000); !errors.Is(err, ErrEvaluationBudget) {
			t.Errorf("err = %v, want budget error", err)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		t.Parallel()
		node, _ := New("test", "solve(x**2 - 2, x = 0..2)").Parse()
		v, err := node.Eval(context.Background(), NewEnv())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := approxRat(ctx, v, -5000); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want cancellation", err)
		}
	})
}

func TestWithBudgetKeepsExisting(t *testing.T) {
	t.Parallel()

	ctx := withBudgetLimit(context.Background(), 3)
	if got := WithBudget(ctx); got != ctx {
		t.Fatal("WithBudget replaced an existing budget")
	}
	_, err := evalShipping(WithBudget(ctx), "sum(i, i = 1..4)", NewEnv())
	if !errors.Is(err, ErrEvaluationBudget) {
		t.Errorf("err = %v, want budget error", err)
	}
}

func TestNearZeroSign(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	for src, want := range map[string]int{"0": 0, "-3": -1, "2**-150": 1, "-(2**-150)": -1} {
		node, err := New("test", src).Parse()
		if err != nil {
			t.Fatal(err)
		}
		v, err := node.Eval(context.Background(), env)
		if err != nil {
			t.Fatal(err)
		}
		got, err := nearZeroSign(context.Background(), v, env.precision)
		if err != nil || got != want {
			t.Errorf("%s: sign = %d, %v, want %d", src, got, err, want)
		}
	}

	node, _ := New("test", "2**-250").Parse()
	v, _ := node.Eval(context.Background(), env)
	if _, err := nearZeroSign(context.Background(), v, env.precision); !errors.Is(err, errNearZero) {
		t.Errorf("err = %v, want errNearZero", err)
	}
}

func sqrt2Real() *unified.Real { return unified.Sqrt2() }

// A flat root leaves a body far smaller than the distance to the root. The result must still be
// within 2^p of the root at every precision.
func TestSolveFlatRoots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		root *big.Rat
	}{
		{"solve(x**3, x = -1..2.5)", big.NewRat(0, 1)},
		{"solve(x**5, x = -1..3.7)", big.NewRat(0, 1)},
		{"solve((x - 1)**3, x = -1..2.5)", big.NewRat(1, 1)},
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
			for _, p := range []int{-100, -300, -600, -100} {
				got, err := approxRat(context.Background(), v, p)
				if err != nil {
					t.Fatalf("p=%d: %v", p, err)
				}
				diff := new(big.Rat).Sub(got, tt.root)
				if diff.Abs(diff).Cmp(ratPow2(p)) > 0 {
					t.Errorf("p=%d: %s is not within 2^p of %s", p, fmtRat(got), tt.root)
				}
			}
		})
	}
}

func TestSolveUndecidableSignErrors(t *testing.T) {
	t.Parallel()

	// sqrt(2)**2 - 2 is zero but not structurally, so the body is zero on -1..1 and no sign is
	// decidable there.
	src := "solve(sqrt(2)*sqrt(2) - 2 + max(x - 1, 0) + min(x + 1, 0), x = -3..4.5)"
	_, err := evalShipping(context.Background(), src, NewEnv())
	if err == nil || !strings.Contains(err.Error(), "cannot tell the sign") {
		t.Errorf("err = %v, want an undecided-sign error", err)
	}
}
