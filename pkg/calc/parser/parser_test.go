package parser

import (
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/unified"
)

func TestParserExpressions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		exprs []string
		want  float64
	}{
		{
			name:  "precedence",
			exprs: []string{"1 + 2 * 3"},
			want:  7,
		},
		{
			name:  "parentheses",
			exprs: []string{"(1 + 2) * 3"},
			want:  9,
		},
		{
			name:  "unary minus",
			exprs: []string{"-4 + 2"},
			want:  -2,
		},
		{
			name:  "assignment and reference",
			exprs: []string{"foo = 2", "foo * 5"},
			want:  10,
		},
		{
			name:  "right associative assignment",
			exprs: []string{"a = b = 3", "a + b"},
			want:  6,
		},
		{
			name:  "square root",
			exprs: []string{"√4"},
			want:  2,
		},
		{
			name:  "square root of 2",
			exprs: []string{"√2"},
			want:  math.Sqrt2,
		},
		{
			name:  "nested square root",
			exprs: []string{"√√16"},
			want:  2,
		},
		{
			name:  "complex expression",
			exprs: []string{"a = 2.8", "b = 4.5", "c = √(a*a + b*b)", "c"},
			want:  5.3,
		},
		{name: "PI", exprs: []string{"PI"}, want: math.Pi},
		{name: "E", exprs: []string{"E"}, want: math.E},
		{name: "LN2", exprs: []string{"LN2"}, want: math.Ln2},
		{name: "PHI", exprs: []string{"PHI"}, want: math.Phi},
		{name: "SQRT2 squared", exprs: []string{"SQRT2 * SQRT2"}, want: 2},
		{
			name:  "basic modulo",
			exprs: []string{"10 % 3"},
			want:  1,
		},
		{
			name:  "another basic modulo",
			exprs: []string{"17 % 5"},
			want:  2,
		},
		{
			name:  "exact division",
			exprs: []string{"15 % 5"},
			want:  0,
		},
		{
			name:  "float modulo",
			exprs: []string{"7.5 % 2"},
			want:  1.5,
		},
		{
			name:  "negative dividend, floor division",
			exprs: []string{"-10 % 3"},
			want:  2,
		},
		{
			name:  "negative divisor, floor division",
			exprs: []string{"10 % -3"},
			want:  -2,
		},
		{
			name:  "both negative, floor division",
			exprs: []string{"-10 % -3"},
			want:  -1,
		},
		{
			name:  "larger modulo",
			exprs: []string{"100 % 7"},
			want:  2,
		},
		{
			name:  "modulo with precedence",
			exprs: []string{"20 % 6 + 2"},
			want:  4,
		},
		{
			name:  "modulo with multiplication",
			exprs: []string{"5 * 3 % 7"},
			want:  1,
		},
		{
			name:  "basic left shift",
			exprs: []string{"4 << 2"},
			want:  16,
		},
		{
			name:  "basic right shift",
			exprs: []string{"16 >> 2"},
			want:  4,
		},
		{
			name:  "left shift by zero",
			exprs: []string{"7 << 0"},
			want:  7,
		},
		{
			name:  "right shift by zero",
			exprs: []string{"7 >> 0"},
			want:  7,
		},
		{
			name:  "shift with precedence same as multiplication",
			exprs: []string{"2 + 4 << 1"},
			want:  10,
		},
		{
			name:  "shift left associativity",
			exprs: []string{"64 >> 2 >> 1"},
			want:  8,
		},
		{
			name:  "mixed shift and multiplication",
			exprs: []string{"2 * 3 << 2"},
			want:  24,
		},
		{
			name:  "mixed shift and division",
			exprs: []string{"32 >> 1 / 2"},
			want:  8,
		},
		{
			name:  "shift with parentheses",
			exprs: []string{"(1 + 1) << 3"},
			want:  16,
		},
		{
			name:  "large left shift",
			exprs: []string{"1 << 20"},
			want:  1048576,
		},
		{
			name:  "large right shift",
			exprs: []string{"1048576 >> 18"},
			want:  4,
		},
		{
			name:  "non-integer first operand left shift",
			exprs: []string{"3.5 << 2"},
			want:  14,
		},
		{
			name:  "non-integer first operand right shift",
			exprs: []string{"20 >> 4.0"},
			want:  1.25,
		},
		{
			name:  "left shift non-integer left operand (sqrt)",
			exprs: []string{"√2 << 1"},
			want:  math.Sqrt2 * 2,
		},
		{
			name:  "left shift non-integer left operand (transcendental)",
			exprs: []string{"PI << 3"},
			want:  math.Pi * 8,
		},
		{
			name:  "shift with assignment",
			exprs: []string{"a = 8", "a << 2"},
			want:  32,
		},
		{
			name:  "shift both directions",
			exprs: []string{"5 << 4 >> 2"},
			want:  20,
		},
		{
			name:  "shift with modulo",
			exprs: []string{"100 >> 2 % 7"},
			want:  4,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := NewEnv()
			var result *unified.Real
			var err error
			for _, expr := range tt.exprs {
				result, err = parseAndEval(t, expr, env)
				if err != nil {
					t.Fatalf("parse/eval %q: %v", expr, err)
				}
			}

			got := realToFloat(t, result)
			if diff := math.Abs(got - tt.want); diff > 1e-9 {
				t.Fatalf("result mismatch: got %v, want %v (diff=%v)", got, tt.want, diff)
			}
		})
	}
}

func TestParserErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{
			name:    "dangling plus",
			expr:    "1 +",
			wantErr: "unexpected EOF",
		},
		{
			name:    "lonely close paren",
			expr:    ")",
			wantErr: "unexpected token RPAREN",
		},
		{
			name:    "illegal tokens",
			expr:    "$",
			wantErr: "unexpected token",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := New("test", tt.expr)
			_, err := p.Parse()
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error mismatch: got %v want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestEvalUndefinedIdentifier(t *testing.T) {
	t.Parallel()

	p := New("test", "foo + 1")
	node, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if _, err := node.Eval(NewEnv()); err == nil {
		t.Fatalf("expected undefined identifier error")
	} else if !strings.Contains(err.Error(), "undefined identifier") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParserTranscendentalConstantsImmutable(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	if _, err := parseAndEval(t, "PI = 3", env); err == nil {
		t.Fatalf("expected error when assigning to PI")
	} else if !strings.Contains(err.Error(), "constant") {
		t.Fatalf("unexpected error: %v", err)
	}

	val, err := parseAndEval(t, "PI", env)
	if err != nil {
		t.Fatalf("PI lookup failed after assignment error: %v", err)
	}

	got := realToFloat(t, val)
	if diff := math.Abs(got - math.Pi); diff > 1e-9 {
		t.Fatalf("PI changed after failed assignment: got %v diff %v", got, diff)
	}
}

func TestModuloByZero(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	_, err := parseAndEval(t, "10 % 0", env)
	if err == nil {
		t.Fatalf("expected modulo by zero error")
	}
	if !strings.Contains(err.Error(), "modulo by zero") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestShiftErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{
			name:    "shift by non-integer (decimal)",
			expr:    "8 << 2.5",
			wantErr: "shift count must be an integer",
		},
		{
			name:    "shift by non-integer (sqrt)",
			expr:    "16 >> √2",
			wantErr: "shift count must be an integer",
		},
		{
			name:    "shift by transcendental constant",
			expr:    "4 << PI",
			wantErr: "shift count must be an integer",
		},
		{
			name:    "shift by expression result that is non-integer",
			expr:    "8 << (5 / 2)",
			wantErr: "shift count must be an integer",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := NewEnv()
			_, err := parseAndEval(t, tt.expr, env)
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error mismatch: got %v want substring %q", err, tt.wantErr)
			}
		})
	}
}

func parseAndEval(t *testing.T, expr string, env *Env) (*unified.Real, error) {
	t.Helper()
	p := New("test", expr)
	node, err := p.Parse()
	if err != nil {
		return nil, err
	}
	return node.Eval(env)
}

const testPrecision = -100

func realToFloat(t *testing.T, r *unified.Real) float64 {
	t.Helper()
	rat := approximateRealForTest(t, r, testPrecision)
	f, _ := rat.Float64()
	return f
}

func approximateRealForTest(t *testing.T, r *unified.Real, precision int) *big.Rat {
	t.Helper()
	if r == nil {
		t.Fatalf("nil real result")
	}
	if !constructive.IsPrecisionValid(precision) {
		t.Fatalf("invalid precision %d", precision)
	}
	approx := constructive.Approximate(r.Constructive(), precision)
	if approx == nil {
		t.Fatalf("approximation failed for precision %d", precision)
	}

	exp := int64(-precision)
	denom := new(big.Int).Exp(big.NewInt(2), big.NewInt(exp), nil)
	return new(big.Rat).SetFrac(approx, denom)
}
