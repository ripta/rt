package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func evalShipping(ctx context.Context, src string, env *Env) (string, error) {
	node, err := New("test", src).Parse()
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

func TestSumProduct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
	}{
		{"sum(i**2, i = 1..10)", "385"},
		{"product(i, i = 1..5)", "120"},
		{"sum(sum(i*j, j = 1..i), i = 1..3)", "25"},
		{"sum(sum(j, j = 1..i), i = 1..3)", "10"},
		{"sum(i, i = 5..1)", "0"},
		{"product(i, i = 5..1)", "1"},
		{"sum(i, i = 1..0)", "0"},
		{"product(i, i = 1..0)", "1"},
		{"sum(i, i = -2..2)", "0"},
		{"sum(i, i = 1..2+1)", "6"},
		{"sum(i, i = 1..(n = 3))", "6"},
		{"sum(i, i = 1..3) + product(i, i = 1..3)", "12"},
	}
	for _, tt := range tests {
		got, err := evalShipping(context.Background(), tt.src, NewEnv())
		if err != nil {
			t.Errorf("%s: %v", tt.src, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s = %s, want %s", tt.src, got, tt.want)
		}
	}
}

func TestSumShadowsAndDoesNotLeak(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	for _, c := range []struct{ src, want string }{
		{"i = 7", "7"},
		{"sum(i, i = 1..3)", "6"},
		{"i", "7"},
		{"sum(i, i = 1..i)", "28"},
	} {
		src, want := c.src, c.want
		got, err := evalShipping(context.Background(), src, env)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if got != want {
			t.Errorf("%s = %s, want %s", src, got, want)
		}
	}
}

func TestSumProductErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
	}{
		{"sum(i, i = 1..2.5)", "integers"},
		{"product(i, i = 0.5..3)", "integers"},
		{"sum(i, i = 1..PI)", "integers"},
		{"sum(i, i = 1..inf)", "infinite bound"},
		{"sum(i, i = -inf..3)", "infinite bound"},
		{"sum(i, i = 3)", "range"},
		{"sin(y = 3)", "sin((y = ...))"},
		{"sum(i)", "binding"},
		{"sum(i = 1..3, 1)", "last"},
	}
	for _, tt := range tests {
		_, err := evalShipping(context.Background(), tt.src, NewEnv())
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want containing %q", tt.src, err, tt.want)
		}
	}
}

func TestSumProductBudgetAndCancel(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"sum(i, i = 1..2000000)",
		"product(1, i = 1..2000000)",
		"sum(sum(1, j = 1..1500), i = 1..1500)",
	} {
		_, err := evalShipping(WithBudget(context.Background()), src, NewEnv())
		if !errors.Is(err, ErrEvaluationBudget) {
			t.Errorf("%s: err = %v, want budget error", src, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, src := range []string{"sum(i, i = 1..10)", "product(i, i = 1..10)"} {
		_, err := evalShipping(ctx, src, NewEnv())
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: err = %v, want cancellation", src, err)
		}
	}
}

func TestBindersListedTogether(t *testing.T) {
	t.Parallel()

	var names []string
	for _, f := range Functions() {
		if f.Group == "Binders" {
			names = append(names, f.Name)
		}
	}
	if got := strings.Join(names, ","); got != "sum,product,solve" {
		t.Errorf("Binders group = %q, want sum,product,solve", got)
	}

	infos := Functions()
	if last := infos[len(infos)-1].Group; last != "Binders" {
		t.Errorf("last group = %q, want Binders", last)
	}
}

func TestSumProductUnevenCounts(t *testing.T) {
	t.Parallel()

	for src, want := range map[string]string{
		"sum(i, i = 1..7)":     "28",
		"sum(i, i = 1..100)":   "5050",
		"product(i, i = 1..7)": "5040",
		"sum(i, i = 3..3)":     "3",
	} {
		got, err := evalShipping(context.Background(), src, NewEnv())
		if err != nil || got != want {
			t.Errorf("%s = %s, %v, want %s", src, got, err, want)
		}
	}
}

// A left-deep chain of irrational terms made the final approximation cost grow roughly with the
// cube of the term count.
func TestSumIrrationalTermsFinishes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	node, err := New("test", "sum(sin(i), i = 1..2000)").Parse()
	if err != nil {
		t.Fatal(err)
	}
	env := NewEnv()
	v, err := node.Eval(ctx, env)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := approxRat(ctx, v, env.precision); err != nil {
		t.Fatalf("approximating a 2000-term irrational sum: %v", err)
	}
}
