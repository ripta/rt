package parser

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ripta/reals/pkg/unified"
)

// testSum adds the body over the integers lo..hi.
func testSum(ctx context.Context, env *Env, body []Node, bind Binding) (*unified.Real, error) {
	lo, err := approxRat(ctx, bind.Lo.Value, env.precision)
	if err != nil {
		return nil, err
	}
	hi, err := approxRat(ctx, bind.Hi.Value, env.precision)
	if err != nil {
		return nil, err
	}

	total := unified.Zero()
	for i := new(big.Int).Set(lo.Num()); i.Cmp(hi.Num()) <= 0; i.Add(i, big.NewInt(1)) {
		v, err := evalBody(ctx, env, body[0], bind.Name, intReal(i))
		if err != nil {
			return nil, err
		}
		total = total.Add(v)
	}
	return total, nil
}

// testPoint evaluates the body at the point.
func testPoint(ctx context.Context, env *Env, body []Node, bind Binding) (*unified.Real, error) {
	return evalBody(ctx, env, body[0], bind.Name, bind.Lo.Value)
}

func evalWithBinders(ctx context.Context, src string, env *Env) (string, error) {
	node, err := parseWithBinders(src)
	if err != nil {
		return "", err
	}

	v, err := node.Eval(ctx, env)
	if err != nil {
		return "", err
	}

	rat, err := approxRat(context.Background(), v, env.precision)
	if err != nil {
		return "", err
	}
	return rat.RatString(), nil
}

func TestScopedEnv(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	if err := env.Set("x", intReal(big.NewInt(10))); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		src  string
		want string
	}{
		{"tdiff(x, x = 3)", "3"},
		{"tdiff(x, x = x + 1)", "11"},
		{"tsum(i, i = 1..3)", "6"},
		{"tsum(i*x, i = 1..3)", "60"},
		{"tsum(tsum(i*j, j = 1..i), i = 1..3)", "25"},
		{"tsum(i, i = 1..3) + x", "16"},
	}
	for _, tt := range tests {
		got, err := evalWithBinders(context.Background(), tt.src, env)
		if err != nil {
			t.Errorf("%s: %v", tt.src, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s = %s, want %s", tt.src, got, tt.want)
		}
	}

	if v, ok := env.Get("x"); !ok || v == nil {
		t.Error("x was lost after a binder shadowed it")
	}
	if _, ok := env.Get("i"); ok {
		t.Error("binder variable i leaked into the caller's scope")
	}
}

func TestChildEnvSet(t *testing.T) {
	t.Parallel()

	root := NewEnv()
	c := root.child("i", intReal(big.NewInt(1)))

	if err := c.Set("i", intReal(big.NewInt(2))); err == nil {
		t.Error("assigning the bound name succeeded")
	}
	if err := c.Set("y", intReal(big.NewInt(2))); err != nil {
		t.Fatal(err)
	}
	if _, ok := root.Get("y"); !ok {
		t.Error("assignment through a child did not reach the root")
	}
}

func TestEvaluationBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src     string
		limit   int
		wantErr bool
	}{
		{"tsum(i, i = 1..5)", 5, false},
		{"tsum(i, i = 1..6)", 5, true},
		{"tsum(tsum(1, j = 1..3), i = 1..3)", 12, false},
		{"tsum(tsum(1, j = 1..3), i = 1..3)", 11, true},
	}
	for _, tt := range tests {
		ctx := withBudgetLimit(context.Background(), tt.limit)
		_, err := evalWithBinders(ctx, tt.src, NewEnv())
		if got := errors.Is(err, ErrEvaluationBudget); got != tt.wantErr {
			t.Errorf("%s with limit %d: err = %v, want budget error %v", tt.src, tt.limit, err, tt.wantErr)
		}
	}

	if _, err := evalWithBinders(context.Background(), "tsum(i, i = 1..50)", NewEnv()); err != nil {
		t.Errorf("a context without a budget counted evaluations: %v", err)
	}
}

func TestBudgetErrorNamesCap(t *testing.T) {
	t.Parallel()

	if got := ErrEvaluationBudget.Error(); got != "evaluation budget of 1000000 body evaluations exceeded" {
		t.Errorf("message = %q", got)
	}
}

func TestCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, src := range []string{
		"tsum(i, i = 1..3)",
		"tdiff(x, x = 3)",
		"factorial(5)",
		"choose(10, 3)",
		"perm(10, 3)",
		"2 ** 5",
		"(-2) ** 5",
		"sqrt(0 - 4) + 1",
	} {
		_, err := evalWithBinders(ctx, src, NewEnv())
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: err = %v, want context.Canceled", src, err)
		}
	}
}

func TestCancelInsideLoops(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"factorial(100000000)",
		"choose(100000000, 50000000)",
		"perm(100000000, 50000000)",
		"3 ** 100000000",
		"(-3) ** 100000000",
	} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			time.AfterFunc(50*time.Millisecond, cancel)

			done := make(chan error, 1)
			go func() {
				_, err := evalWithBinders(ctx, src, NewEnv())
				done <- err
			}()

			select {
			case err := <-done:
				if !errors.Is(err, ErrCancelled) {
					t.Errorf("err = %v, want ErrCancelled", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("evaluation ignored cancellation")
			}
		})
	}
}

func TestStopErrorsSurviveDomainError(t *testing.T) {
	t.Parallel()

	for _, stop := range []error{ErrEvaluationBudget, ErrCancelled, context.Canceled, context.DeadlineExceeded} {
		got := domainError(context.Background(), "sqrt", nil, -10, stop)
		if !errors.Is(got, stop) {
			t.Errorf("domainError(%v) = %v, lost the error", stop, got)
		}
	}

	if got := domainError(context.Background(), "sqrt", nil, -10, errors.New("boom")); errors.Is(got, ErrCancelled) {
		t.Error("an ordinary error matched ErrCancelled")
	}
}

func TestStopErrorsHaveNoPositionPrefix(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := evalWithBinders(ctx, "factorial(5)", NewEnv())
	if err != ErrCancelled && err != context.Canceled {
		t.Errorf("err = %q, want a bare stop error with no position prefix", err)
	}
}
