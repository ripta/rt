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
