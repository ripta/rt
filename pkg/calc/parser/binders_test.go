package parser

import (
	"strings"
	"testing"
)

// binderCatalog returns the shipping catalog plus test-only binders. They cover
// the point form and infinite bounds, which the shipped binders reject.
// Each parser carries its own catalog, so parallel tests share nothing.
func binderCatalog() map[string]function {
	m := make(map[string]function, len(functions)+4)
	for k, v := range functions {
		m[k] = v
	}
	m["tsum"] = function{name: "tsum", signature: "tsum(expr, i = a..b)", minArgs: 2, maxArgs: 2, binder: &binderSpec{forms: bindRange, eval: testSum}}
	m["tint"] = function{name: "tint", signature: "tint(expr, x = a..b)", minArgs: 2, maxArgs: 2, binder: &binderSpec{forms: bindRange, infinite: true}}
	m["tdiff"] = function{name: "tdiff", signature: "tdiff(expr, x = a)", minArgs: 2, maxArgs: 2, binder: &binderSpec{forms: bindPoint, eval: testPoint}}
	m["tsolve"] = function{name: "tsolve", signature: "tsolve(expr, x = a)", minArgs: 2, maxArgs: 2, binder: &binderSpec{forms: bindPoint | bindRange, infinite: true}}
	return m
}

func parseWithBinders(src string) (Node, error) {
	p := New("test", src)
	p.catalog = binderCatalog()
	return p.Parse()
}

func TestBinderParseOK(t *testing.T) {
	t.Parallel()

	tests := []string{
		"tsum(i**2, i = 1..10)",
		"tsum(i, i = 1..n+1)",
		"tsum(i, i = 1..(n = 3))",
		"tsum(tsum(i*j, j = 1..i), i = 1..3)",
		"tint(x, x = -inf..inf)",
		"tint(x, x = 0..inf)",
		"tdiff(x**2, x = 3)",
		"tdiff(x**2, x = -1.5 + 2)",
		"tsolve(x - 1, x = 0)",
		"tsolve(x - 1, x = -inf..2)",
		"sin((y = 3))",
		"tsum(i, i = 1..3) + 1",
	}
	for _, src := range tests {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			if _, err := parseWithBinders(src); err != nil {
				t.Fatalf("parse %q: %v", src, err)
			}
		})
	}
}

func TestBindingNodeShape(t *testing.T) {
	t.Parallel()

	for src, wantRange := range map[string]bool{"tdiff(x, x = 3)": false, "tsum(x, x = 1..3)": true} {
		node, err := parseWithBinders(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		args := node.(*CallNode).Args
		b, ok := args[len(args)-1].(*BindingNode)
		if !ok {
			t.Fatalf("%q: last argument is %T, want *BindingNode", src, args[len(args)-1])
		}
		if (b.Hi != nil) != wantRange {
			t.Errorf("%q: range = %v, want %v", src, b.Hi != nil, wantRange)
		}
	}

	node, err := parseWithBinders("sin((y = 3))")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := node.(*CallNode).Args[0].(*AssignNode); !ok {
		t.Errorf("parenthesized assignment parsed to %T, want *AssignNode", node.(*CallNode).Args[0])
	}
}

func TestBinderParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{"binding to non-binder", "sin(y = 3)", "sin is not a binder"},
		{"escape hatch named", "sin(y = 3)", "sin((y = ...))"},
		{"binding to non-binder range", "sin(y = 1..3)", "sin is not a binder"},
		{"binder without binding", "tsum(1, 2)", "tsum needs a binding argument"},
		{"binder with no arguments", "tsum()", "tsum needs a binding argument"},
		{"parenthesized assignment is no binding", "tsum(i, (i = 1))", "tsum needs a binding argument"},
		{"point to range binder", "tsum(i, i = 3)", "tsum takes a range binding"},
		{"range to point binder", "tdiff(x, x = 1..3)", "tdiff takes a point binding"},
		{"binding not last", "tsum(i = 1..3, i)", "must be the last argument"},
		{"two bindings", "tsum(i = 1..3, i = 1..3)", "must be the last argument"},
		{"arity counts the binding", "tsum(i = 1..3)", "tsum expects 2 arguments, got 1"},
		{"too many arguments", "tsum(1, 2, i = 1..3)", "tsum expects 2 arguments, got 3"},
		{"nested assignment in bound of binding", "tsum(i, i = 1..n = 3)", "expected ',' or ')'"},
		{"chained assignment value", "tdiff(x, x = y = 2)", "expected ',' or ')'"},
		{"bare assignment in body is a second binding", "tsum(y = i, i = 1..3)", "must be the last argument"},
		{"parenthesized assignment in body", "tsum((y = i), i = 1..3)", "assignment is not allowed inside the body of tsum"},
		{"assignment nested in call in body", "tsum(sin((y = i)), i = 1..3)", "assignment is not allowed inside the body of tsum"},
		{"assignment in nested binder bound", "tsum(tsum(j, j = 1..(n = 3)), i = 1..3)", "assignment is not allowed inside the body of tsum"},
		{"binding a constant", "tsum(PI, PI = 1..3)", `cannot bind constant "PI"`},
		{"binding E", "tdiff(E, E = 1)", `cannot bind constant "E"`},
		{"binding a history name", "tsum($1, $1 = 1..3)", `cannot bind result history name "$1"`},
		{"binding inf", "tsum(1, inf = 1..3)", "inf is only valid as a range bound"},
		{"inf assignment", "inf = 5", "inf is only valid as a range bound"},
		{"inf as variable", "inf", "inf is only valid as a range bound"},
		{"inf in arithmetic", "1 + inf", "inf is only valid as a range bound"},
		{"inf in bound arithmetic", "tint(x, x = 0..inf+1)", "inf is only valid as a range bound"},
		{"inf in body", "tint(inf, x = 0..1)", "inf is only valid as a range bound"},
		{"inf as point value", "tsolve(x, x = inf)", "inf is only valid as a range bound"},
		{"inf in ordinary call", "sin(inf)", "inf is only valid as a range bound"},
		{"inf in sum", "tsum(i, i = 1..inf)", "tsum does not accept an infinite bound"},
		{"negative inf in sum", "tsum(i, i = -inf..1)", "tsum does not accept an infinite bound"},
		{"unknown function", "nope(1)", `unknown function "nope"`},
		{"unknown function wins over arguments", "nope(y = 3)", `unknown function "nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseWithBinders(tt.expr)
			if err == nil {
				t.Fatalf("parse %q: expected error containing %q", tt.expr, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parse %q: got %v, want substring %q", tt.expr, err, tt.wantErr)
			}
		})
	}
}

func TestCallParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{"too few arguments", "sin()", "sin expects 1 argument, got 0"},
		{"too many arguments", "sin(1, 2)", "sin expects 1 argument, got 2"},
		{"unknown function", "nope(1)", `unknown function "nope"`},
		{"two-argument function with one", "log(8)", "log expects 2 arguments, got 1"},
		{"variadic with none", "min()", "min expects at least 1 argument, got 0"},
		{"error precedes evaluation", "x = 1 + sin()", "sin expects 1 argument, got 0"},
		{"binding to shipping function", "abs(y = 3)", "abs is not a binder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse("test", tt.expr)
			if err == nil {
				t.Fatalf("parse %q: expected error containing %q", tt.expr, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parse %q: got %v, want substring %q", tt.expr, err, tt.wantErr)
			}
		})
	}
}

func TestConstantNamesSingleSource(t *testing.T) {
	t.Parallel()

	env := NewEnv()
	for name := range transcendentalConstants {
		if !isConstantName(name) {
			t.Errorf("isConstantName(%q) = false", name)
		}
		if _, ok := env.Get(name); !ok {
			t.Errorf("NewEnv does not seed %q", name)
		}
	}
}
