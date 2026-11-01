package parser

import (
	"context"
	"strings"
	"testing"
)

func TestDiffNonSmoothValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
	}{
		{"diff(abs(x), x = 3)", "1"},
		{"diff(abs(x), x = -3)", "-1"},
		{"diff(abs(x*x - 4), x = 1)", "-2"},
		{"diff(abs(x), x = 10**-200)", "1"},
		{"diff(abs(x), x = -(10**-200))", "-1"},
		{"diff(abs(x), x = SQRT2**2 - 2 + 2**-150)", "1"},
		{"diff(abs(x), x = SQRT2**2 - 2 + 2**-600)", "1"},
		{"diff(abs(0) + x, x = 1)", "1"},
		{"diff(floor(3)*x, x = 5)", "3"},
		{"diff(floor(x), x = 2.5)", "0"},
		{"diff(ceil(x), x = 2.5)", "0"},
		{"diff(round(x), x = 2.25)", "0"},
		{"diff(round(x), x = 3)", "0"},
		{"diff(round(x), x = 0)", "0"},
		{"diff(trunc(x), x = -2.5)", "0"},
		{"diff(trunc(x), x = 0)", "0"},
		{"diff(trunc(x), x = 1/2)", "0"},
		{"diff(trunc(x), x = -1/2)", "0"},
		{"diff(signum(x), x = -2)", "0"},
		{"diff(x * floor(x), x = 2.5)", "2"},
		{"diff(x % 3, x = 7)", "1"},
		{"diff(x % 3, x = 7.5)", "1"},
		{"diff(x % 3, x = -7)", "1"},
		{"diff(10 % x, x = 3)", "-3"},
		{"diff(10 % x, x = -3)", "4"},
		{"diff(x % (2*x + 1), x = 2)", "1"},
		{"diff(min(x, 2), x = 1)", "1"},
		{"diff(min(x, 2), x = 3)", "0"},
		{"diff(max(x, 2), x = 3)", "1"},
		{"diff(max(x, 2), x = 1)", "0"},
		{"diff(max(x*x, 2), x = 3)", "6"},
		{"diff(min(5, 5, x), x = 9)", "0"},
		{"diff(max(x, 1, 1), x = 5)", "1"},
		{"diff(min(x, 1, 1), x = 0)", "1"},
		{"diff(x << 3, x = 1)", "8"},
		{"diff(x >> 2, x = 1)", "1/4"},
		{"diff(abs(x)*x, x = -2)", "4"},
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

func TestDiffNonSmoothKinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
	}{
		{"diff(abs(x), x = 0)", "abs is not differentiable at 0"},
		{"diff(signum(x), x = 0)", "signum is not differentiable at 0"},
		{"diff(floor(x), x = 3)", "floor is not differentiable at 3"},
		{"diff(floor(x), x = 0)", "floor is not differentiable at 0"},
		{"diff(ceil(x), x = -2)", "ceil is not differentiable at -2"},
		{"diff(round(x), x = 1/2)", "round is not differentiable at 0.5"},
		{"diff(round(x), x = -5/2)", "round is not differentiable at -2.5"},
		{"diff(trunc(x), x = 2)", "trunc is not differentiable at 2"},
		{"diff(trunc(x), x = -1)", "trunc is not differentiable at -1"},
		{"diff(x % 3, x = 6)", "% is not differentiable where the first argument divided by the second = 2"},
		{"diff(x % 3, x = 0)", "% is not differentiable where the first argument divided by the second = 0"},
		{"diff(x % 3, x = 3)", "% is not differentiable where the first argument divided by the second = 1"},
		{"diff(10 % x, x = 5)", "% is not differentiable where the first argument divided by the second = 2"},
		{"diff(min(x, 1), x = 1)", "min is not differentiable where two arguments are equal, as at 1"},
		{"diff(max(1, x), x = 1)", "max is not differentiable where two arguments are equal"},
		{"diff(min(x, 2*x), x = 0)", "min is not differentiable where two arguments are equal"},
		{"diff(abs(x), x = SQRT2**2 - 2)", "too close to it to tell whether it is there"},
		{"diff(abs(x), x = SQRT2**2 - 2 + 2**-5000)", "too close to it to tell whether it is there"},
		{"diff(floor(x), x = SQRT2**2)", "too close to it to tell whether it is there"},
		{"diff(max(x, SQRT2**2), x = 2)", "too close to 2 to tell whether they are"},
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

func TestDiffKinkIsCheckedOnlyForLiveArguments(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	if _, err := evalShipping(context.Background(), "y = 0", env); err != nil {
		t.Fatal(err)
	}

	for src, want := range map[string]string{
		"diff(abs(y)*x, x = 1)":    "0",
		"diff(floor(y)*x, x = 1)":  "0",
		"diff(min(y, 0)*x, x = 1)": "0",
		"diff(y % 1 + x, x = 1)":   "1",
	} {
		got, err := evalShipping(context.Background(), src, env)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		if got != want {
			t.Errorf("%s = %s, want %s", src, got, want)
		}
	}
}

func TestDiffRejectsIntegerOnlyUses(t *testing.T) {
	t.Parallel()

	rejected := []string{
		"diff(factorial(x), x = 3)",
		"diff(factorial(x + 1), x = 3)",
		"diff(1 + sin(factorial(x)), x = 3)",
		"diff(choose(x, 2), x = 3)",
		"diff(choose(5, x), x = 3)",
		"diff(perm(x, 1), x = 3)",
		"diff(perm(5, x*2), x = 3)",
		"diff(2 << x, x = 1)",
		"diff(2 >> x, x = 1)",
		"diff(2 << (x + 1), x = 1)",
		"diff(sum(x, i = 1..factorial(x)), x = 3)",
		"diff(sum(factorial(x), i = 1..3), x = 3)",
		"diff(diff(factorial(x), y = 1), x = 3)",
		"diff(diff(2 << x, y = 1), x = 3)",
		"diff(diff(factorial(y), x = 1), y = 3)",
		"diff(diff(factorial(x), x = 1), x = 3)",
		"diff(sum(1, i = 1..2) + factorial(x), x = 3)",
	}
	for _, src := range rejected {
		_, err := New("test", src).Parse()
		if err == nil || !strings.Contains(err.Error(), "is the variable of diff") {
			t.Errorf("%s: got %v, want a variable-of-diff error", src, err)
		}
	}

	accepted := []string{
		"diff(x << 2, x = 1)",
		"diff(x >> 2, x = 1)",
		"diff(factorial(3) * x, x = 1)",
		"diff(factorial(y), x = 3)",
		"diff(choose(y, 2) * x, x = 3)",
		"diff(sum(factorial(x), x = 1..3), x = 2)",
		"diff(sum(2 << x, x = 1..3), x = 2)",
		"diff(product(perm(5, x), x = 1..3), x = 2)",
		"diff(diff(factorial(3), x = 1), x = 2)",
		"diff(diff(sum(factorial(x), x = 1..3), x = 1), y = 2)",
		"factorial(3) + (2 << 3)",
	}
	for _, src := range accepted {
		if _, err := New("test", src).Parse(); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}

// nestedMixed evaluates src, which names x and y, as the mixed second partial of two diffs: y
// takes the older tag and x the newer one, as diff(diff(src, x = xAt), y = yAt) would. The
// real nested calls are tested beside the order literal.
func nestedMixed(t *testing.T, src string, xAt, yAt int64) (string, error) {
	t.Helper()

	node, err := New("test", src).Parse()
	if err != nil {
		t.Fatal(err)
	}

	ty, tx := newTag(), newTag()
	env := NewEnv().
		childJet("y", jv{tag: ty, co: []jv{intJ(yAt), intJ(1)}}).
		childJet("x", jv{tag: tx, co: []jv{intJ(xAt), intJ(1)}})

	r, err := evalJet(context.Background(), env, node)
	if err != nil {
		return "", err
	}

	got, err := approxRat(context.Background(), r.coef(tx, 1).coef(ty, 1).value(), -100)
	if err != nil {
		return "", err
	}
	return got.RatString(), nil
}

func TestNonSmoothKinkInTheInnerTag(t *testing.T) {
	t.Parallel()

	values := []struct {
		src      string
		x, y     int64
		want     string
		wantKink string
	}{
		{src: "abs(x*y)", x: 1, y: 2, want: "1"},
		{src: "abs(x*y)", x: 1, y: -2, want: "-1"},
		{src: "abs(y)", x: 1, y: 3, want: "0"},
		{src: "floor(x*y + 0.5)", x: 1, y: 3, want: "0"},
		{src: "max(x*y, 1)", x: 1, y: 3, want: "1"},
		{src: "abs(y)", x: 1, y: 0, wantKink: "abs is not differentiable at 0"},
		{src: "abs(x + y)", x: 1, y: -1, wantKink: "abs is not differentiable at 0"},
		{src: "abs(x)", x: 0, y: 1, wantKink: "abs is not differentiable at 0"},
		{src: "floor(x*y)", x: 1, y: 3, wantKink: "floor is not differentiable at 3"},
		{src: "x % y", x: 6, y: 3, wantKink: "% is not differentiable where the first argument divided by the second = 2"},
		{src: "min(x, y)", x: 2, y: 2, wantKink: "min is not differentiable where two arguments are equal"},
	}
	for _, tt := range values {
		got, err := nestedMixed(t, tt.src, tt.x, tt.y)
		switch {
		case tt.wantKink != "":
			if err == nil || !strings.Contains(err.Error(), tt.wantKink) {
				t.Errorf("%s at (%d, %d): got %v, want an error containing %q", tt.src, tt.x, tt.y, err, tt.wantKink)
			}
		case err != nil:
			t.Errorf("%s at (%d, %d): %v", tt.src, tt.x, tt.y, err)
		case got != tt.want:
			t.Errorf("%s at (%d, %d) = %s, want %s", tt.src, tt.x, tt.y, got, tt.want)
		}
	}
}

func TestIntegerOnlyEntriesRejectALiveArgument(t *testing.T) {
	t.Parallel()

	// The parser rejects every reachable use of the diff variable here. A hand-built live
	// argument still reaches the entry's marker.
	for _, src := range []string{"factorial(x)", "choose(x, 2)", "choose(5, x)", "perm(x, 1)", "perm(5, x)"} {
		_, err := nestedMixed(t, src, 3, 1)
		if err == nil || !strings.Contains(err.Error(), "is not differentiable inside diff") || !strings.Contains(err.Error(), "defined only on integers") {
			t.Errorf("%s: got %v, want the integer-only marker error", src, err)
		}
	}
}
