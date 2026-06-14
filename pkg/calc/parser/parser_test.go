package parser

import (
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/rational"
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
		{
			name:  "basic exponentiation",
			exprs: []string{"2 ** 3"},
			want:  8,
		},
		{
			name:  "exponentiation to zero",
			exprs: []string{"5 ** 0"},
			want:  1,
		},
		{
			name:  "exponentiation to one",
			exprs: []string{"7 ** 1"},
			want:  7,
		},
		{
			name:  "negative exponent",
			exprs: []string{"2 ** -1"},
			want:  0.5,
		},
		{
			name:  "fractional exponent (square root)",
			exprs: []string{"4 ** 0.5"},
			want:  2,
		},
		{
			name:  "fractional exponent (cube root)",
			exprs: []string{"8 ** (1/3)"},
			want:  2,
		},
		{
			name:  "fractional",
			exprs: []string{"1/2"},
			want:  0.5,
		},
		{
			name:  "fractional additions",
			exprs: []string{"1/3 + 1/6"},
			want:  0.5,
		},
		{
			name:  "fractional multiplication",
			exprs: []string{"2/3 * 3/4"},
			want:  0.5,
		},
		{
			name:  "right associativity",
			exprs: []string{"2 ** 3 ** 2"},
			want:  512,
		},
		{
			name:  "precedence with addition",
			exprs: []string{"2 + 3 ** 2"},
			want:  11,
		},
		{
			name:  "precedence with multiplication",
			exprs: []string{"2 * 3 ** 2"},
			want:  18,
		},
		{
			name:  "precedence with division",
			exprs: []string{"18 / 3 ** 2"},
			want:  2,
		},
		{
			name:  "exponentiation with parentheses",
			exprs: []string{"(2 + 3) ** 2"},
			want:  25,
		},
		{
			name:  "exponentiation with unary minus in exponent",
			exprs: []string{"4 ** -2"},
			want:  0.0625,
		},
		{
			name:  "complex exponentiation expression",
			exprs: []string{"a = 3", "b = 2", "a ** b + 1"},
			want:  10,
		},
		{
			name:  "exponentiation with square root",
			exprs: []string{"√4 ** 2"},
			want:  4,
		},
		{
			name:  "large exponent",
			exprs: []string{"2 ** 10"},
			want:  1024,
		},
		{
			name:  "zero to positive power",
			exprs: []string{"0 ** 5"},
			want:  0,
		},
		{
			name:  "one to any power",
			exprs: []string{"1 ** 100"},
			want:  1,
		},
		{
			name:  "negative base to even integer power",
			exprs: []string{"-2 ** 2"},
			want:  4,
		},
		{
			name:  "negative base to odd integer power",
			exprs: []string{"-2 ** 3"},
			want:  -8,
		},
		{
			name:  "negative base to zero power",
			exprs: []string{"-5 ** 0"},
			want:  1,
		},
		{
			name:  "negative base to negative integer power",
			exprs: []string{"-2 ** -2"},
			want:  0.25,
		},
		{
			name:  "leading comment",
			exprs: []string{`"note" 3 + 4`},
			want:  7,
		},
		{
			name:  "inline comments",
			exprs: []string{`3 "first" + 4 "second" * 5 "third"`},
			want:  23,
		},
		{
			name:  "raw string comment",
			exprs: []string{"3 `note` + 4"},
			want:  7,
		},
		{
			name:  "multiple leading comments",
			exprs: []string{`"note1" "note2" 5`},
			want:  5,
		},
		{
			name:  "comment in parentheses",
			exprs: []string{`("note" 2 + 3) * 4`},
			want:  20,
		},
		{
			name:  "comment in assignment",
			exprs: []string{`a "assign" = 5`, `a * 2`},
			want:  10,
		},
		{
			name:  "sqrt call",
			exprs: []string{"sqrt(4)"},
			want:  2,
		},
		{
			name:  "abs call",
			exprs: []string{"abs(-3)"},
			want:  3,
		},
		{
			name:  "sin call",
			exprs: []string{"sin(0)"},
			want:  0,
		},
		{
			name:  "call with grouped argument expression",
			exprs: []string{"sqrt(2 + 2)"},
			want:  2,
		},
		{
			name:  "call nested in expression",
			exprs: []string{"sqrt(9) + abs(-1)"},
			want:  4,
		},
		{
			name:  "call with whitespace around argument",
			exprs: []string{"sqrt( 16 )"},
			want:  4,
		},
		{
			name:  "variable shares name with function",
			exprs: []string{"sin = 5", "sin"},
			want:  5,
		},
		{
			name:  "variable and function coexist",
			exprs: []string{"sin = 5", "sin(0) + sin"},
			want:  5,
		},
		{name: "cos call", exprs: []string{"cos(0)"}, want: 1},
		{name: "tan call", exprs: []string{"tan(0)"}, want: 0},
		{name: "exp call", exprs: []string{"exp(1)"}, want: math.E},
		{name: "ln call", exprs: []string{"ln(1)"}, want: 0},
		{name: "ln of e", exprs: []string{"ln(E)"}, want: 1},
		{name: "log10 call", exprs: []string{"log10(1000)"}, want: 3},
		{name: "log2 call", exprs: []string{"log2(8)"}, want: 3},
		{name: "log base call", exprs: []string{"log(8, 2)"}, want: 3},
		{name: "atan call", exprs: []string{"atan(0)"}, want: 0},
		{name: "atan2 call", exprs: []string{"atan2(1, 1)"}, want: math.Pi / 4},
		{name: "asin call", exprs: []string{"asin(1)"}, want: math.Pi / 2},
		{name: "acos call", exprs: []string{"acos(1)"}, want: 0},
		{name: "cbrt of negative", exprs: []string{"cbrt(-8)"}, want: -2},
		{name: "cbrt of positive", exprs: []string{"cbrt(27)"}, want: 3},
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
			name:    "bare dollar sign",
			expr:    "$",
			wantErr: "expected digits after '$'",
		},
		{
			name:    "missing comma between arguments",
			expr:    "max(1 2)",
			wantErr: "expected ',' or ')'",
		},
		{
			name:    "unterminated argument list",
			expr:    "sqrt(4",
			wantErr: "expected ',' or ')', got EOF",
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

func TestCallEvalErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{
			name:    "too few arguments",
			expr:    "sin()",
			wantErr: "sin expects 1 argument, got 0",
		},
		{
			name:    "too many arguments",
			expr:    "sin(1, 2)",
			wantErr: "sin expects 1 argument, got 2",
		},
		{
			name:    "unknown function",
			expr:    "nope(1)",
			wantErr: `unknown function "nope"`,
		},
		{
			name:    "domain error",
			expr:    "sqrt(-1)",
			wantErr: "sqrt(-1): argument must be non-negative",
		},
		{
			name:    "ln of non-positive",
			expr:    "ln(-1)",
			wantErr: "ln(-1): argument must be positive",
		},
		{
			name:    "log10 of zero",
			expr:    "log10(0)",
			wantErr: "log10(0): argument must be positive",
		},
		{
			name:    "asin outside unit interval",
			expr:    "asin(2)",
			wantErr: "asin(2): argument must be in [-1, 1]",
		},
		{
			name:    "acos outside unit interval",
			expr:    "acos(2)",
			wantErr: "acos(2): argument must be in [-1, 1]",
		},
		{
			name:    "atan2 at origin",
			expr:    "atan2(0, 0)",
			wantErr: "atan2(0, 0): undefined at the origin",
		},
		{
			name:    "log base one",
			expr:    "log(8, 1)",
			wantErr: "log(8, 1): base must not be equal to one",
		},
		{
			name:    "log with one argument",
			expr:    "log(8)",
			wantErr: "log expects 2 arguments, got 1",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseAndEval(t, tt.expr, NewEnv())
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

func TestExponentiationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{
			name:    "zero to negative power",
			expr:    "0 ** -5",
			wantErr: "zero to negative power is undefined",
		},
		{
			name:    "zero to negative fractional power",
			expr:    "0 ** -0.5",
			wantErr: "zero to negative power is undefined",
		},
		{
			name:    "negative base to fractional power",
			expr:    "-4 ** 0.5",
			wantErr: "negative base to non-integer power is non-real",
		},
		{
			name:    "negative base to decimal power",
			expr:    "-2 ** 2.5",
			wantErr: "negative base to non-integer power is non-real",
		},
		{
			name:    "negative base to irrational power",
			expr:    "-3 ** √2",
			wantErr: "negative base to non-integer power is non-real",
		},
		{
			name:    "negative base to transcendental power",
			expr:    "-2 ** PI",
			wantErr: "negative base to non-integer power is non-real",
		},
		{
			name:    "negative base via unary minus to fractional power",
			expr:    "-2 ** 0.5",
			wantErr: "negative base to non-integer power is non-real",
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

func makeReal(n int64) *unified.Real {
	rat := new(big.Rat).SetInt64(n)
	return unified.New(constructive.One(), rational.FromRational(rat))
}

func TestResultHistoryVariable(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	env.SetConstant("$0", makeReal(42))

	result, err := parseAndEval(t, "$0 + 1", env)
	if err != nil {
		t.Fatalf("eval $0 + 1: %v", err)
	}

	got := realToFloat(t, result)
	if diff := math.Abs(got - 43); diff > 1e-9 {
		t.Fatalf("result mismatch: got %v, want 43", got)
	}
}

func TestResultHistoryUndefined(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	_, err := parseAndEval(t, "$5", env)
	if err == nil {
		t.Fatalf("expected error for undefined $5")
	}
	if !strings.Contains(err.Error(), "no result for line 5") {
		t.Fatalf("error mismatch: got %v, want substring %q", err, "no result for line 5")
	}
}

func TestResultHistoryImmutable(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	env.SetConstant("$0", makeReal(7))

	_, err := parseAndEval(t, "$0 = 99", env)
	if err == nil {
		t.Fatalf("expected error when assigning to $0")
	}
	if !strings.Contains(err.Error(), "cannot assign to constant") {
		t.Fatalf("error mismatch: got %v, want substring %q", err, "cannot assign to constant")
	}
}
