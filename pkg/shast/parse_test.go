package shast

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// cobraCommandForTest returns a bare command with stderr discarded, enough
// for resolveInput's cmd.ErrOrStderr() calls without cluttering test output.
func cobraCommandForTest() *cobra.Command {
	c := &cobra.Command{}
	c.SetErr(io.Discard)
	return c
}

func runParse(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()

	var buf bytes.Buffer
	c := NewParseCommand()
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs(args)

	err = c.Execute()
	return buf.String(), err
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	return exitErr.Code
}

func decodeJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("unmarshalling %q: %v", s, err)
	}
	return v
}

// at walks v through a sequence of map keys (string) and slice indices (int),
// failing the test if any step is missing or the wrong shape. It keeps the
// deeply-nested assertions below readable, similar to a jq path.
func at(t *testing.T, v any, path ...any) any {
	t.Helper()
	cur := v
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				t.Fatalf("path %v: expected object at %q, got %T (%v)", path, k, cur, cur)
			}
			cur, ok = m[k]
			if !ok {
				t.Fatalf("path %v: missing key %q in %v", path, k, m)
			}
		case int:
			s, ok := cur.([]any)
			if !ok {
				t.Fatalf("path %v: expected array at index %d, got %T (%v)", path, k, cur, cur)
			}
			if k < 0 || k >= len(s) {
				t.Fatalf("path %v: index %d out of range (len %d)", path, k, len(s))
			}
			cur = s[k]
		default:
			t.Fatalf("path %v: unsupported path element %T", path, p)
		}
	}
	return cur
}

type parseTest struct {
	Name   string
	Script string
	Check  func(t *testing.T, root any)
}

var parseTests = []parseTest{
	{
		Name:   "simple call",
		Script: "echo hi",
		Check: func(t *testing.T, root any) {
			if got := at(t, root, "statements", 0, "command", "kind"); got != "call" {
				t.Errorf("command kind = %v, want call", got)
			}
			if got := at(t, root, "statements", 0, "command", "args", 0, "text"); got != "echo" {
				t.Errorf("args[0].text = %v, want echo", got)
			}
			if got := at(t, root, "statements", 0, "command", "args", 1, "text"); got != "hi" {
				t.Errorf("args[1].text = %v, want hi", got)
			}
		},
	},
	{
		Name:   "binary &&",
		Script: "true && false",
		Check: func(t *testing.T, root any) {
			cmd := at(t, root, "statements", 0, "command")
			if got := at(t, cmd, "kind"); got != "binary" {
				t.Fatalf("kind = %v, want binary", got)
			}
			if got := at(t, cmd, "op"); got != "&&" {
				t.Errorf("op = %v, want literal &&, not an escaped form", got)
			}
		},
	},
	{
		Name:   "if/elif/else",
		Script: "if true; then echo a; elif false; then echo b; else echo c; fi",
		Check: func(t *testing.T, root any) {
			ifNode := at(t, root, "statements", 0, "command").(map[string]any)
			if got := ifNode["kind"]; got != "if" {
				t.Fatalf("kind = %v, want if", got)
			}
			if _, ok := ifNode["is_else"]; ok {
				t.Errorf("top-level if unexpectedly has is_else set: %v", ifNode["is_else"])
			}

			elif := ifNode["else"].(map[string]any)
			if _, ok := elif["is_else"]; ok {
				t.Errorf("elif branch unexpectedly has is_else set: %v", elif["is_else"])
			}

			els := elif["else"].(map[string]any)
			if got, ok := els["is_else"]; !ok || got != true {
				t.Errorf("terminal else: is_else = %v (present = %v), want true", got, ok)
			}
		},
	},
	{
		Name:   "for loop",
		Script: "for i in a b; do echo $i; done",
		Check: func(t *testing.T, root any) {
			forNode := at(t, root, "statements", 0, "command")
			if got := at(t, forNode, "kind"); got != "for" {
				t.Fatalf("kind = %v, want for", got)
			}
			loop := at(t, forNode, "loop")
			if got := at(t, loop, "kind"); got != "word_iter" {
				t.Fatalf("loop.kind = %v, want word_iter", got)
			}
			if got := at(t, loop, "has_in"); got != true {
				t.Errorf("has_in = %v, want true", got)
			}
			items := at(t, loop, "items").([]any)
			if len(items) != 2 {
				t.Errorf("items = %v, want 2 entries", items)
			}
		},
	},
	{
		Name:   "for loop without in",
		Script: "for i; do echo $i; done",
		Check: func(t *testing.T, root any) {
			loop := at(t, root, "statements", 0, "command", "loop").(map[string]any)
			if _, ok := loop["has_in"]; ok {
				t.Errorf("has_in unexpectedly present for a bare positional-params loop: %v", loop["has_in"])
			}
		},
	},
	{
		Name:   "c-style for loop",
		Script: "for ((i=0; i<3; i++)); do echo $i; done",
		Check: func(t *testing.T, root any) {
			loop := at(t, root, "statements", 0, "command", "loop")
			if got := at(t, loop, "kind"); got != "c_style_loop" {
				t.Fatalf("loop.kind = %v, want c_style_loop", got)
			}
			if got := at(t, loop, "cond", "kind"); got != "arithmetic_binary" {
				t.Errorf("cond.kind = %v, want arithmetic_binary", got)
			}
		},
	},
	{
		Name:   "while loop",
		Script: "while true; do echo hi; done",
		Check: func(t *testing.T, root any) {
			w := at(t, root, "statements", 0, "command").(map[string]any)
			if got := w["kind"]; got != "while" {
				t.Fatalf("kind = %v, want while", got)
			}
			if _, ok := w["until"]; ok {
				t.Errorf("while loop unexpectedly has until set: %v", w["until"])
			}
		},
	},
	{
		Name:   "until loop",
		Script: "until true; do echo hi; done",
		Check: func(t *testing.T, root any) {
			w := at(t, root, "statements", 0, "command")
			if got := at(t, w, "until"); got != true {
				t.Errorf("until = %v, want true", got)
			}
		},
	},
	{
		Name:   "case",
		Script: "case $x in a) echo A;; *) echo other;; esac",
		Check: func(t *testing.T, root any) {
			c := at(t, root, "statements", 0, "command")
			if got := at(t, c, "kind"); got != "case" {
				t.Fatalf("kind = %v, want case", got)
			}
			items := at(t, c, "items").([]any)
			if len(items) != 2 {
				t.Fatalf("items = %v, want 2 entries", items)
			}
			if got := at(t, items[0], "op"); got != ";;" {
				t.Errorf("items[0].op = %v, want ;;", got)
			}
		},
	},
	{
		Name:   "function decl",
		Script: "f() { echo hi; }",
		Check: func(t *testing.T, root any) {
			fn := at(t, root, "statements", 0, "command")
			if got := at(t, fn, "kind"); got != "function" {
				t.Fatalf("kind = %v, want function", got)
			}
			if got := at(t, fn, "name", "value"); got != "f" {
				t.Errorf("name.value = %v, want f", got)
			}
			if got := at(t, fn, "body", "command", "kind"); got != "block" {
				t.Errorf("body.command.kind = %v, want block", got)
			}
		},
	},
	{
		Name:   "array assign",
		Script: `arr=(1 2 "three")`,
		Check: func(t *testing.T, root any) {
			assign := at(t, root, "statements", 0, "command", "assigns", 0)
			arr := at(t, assign, "array")
			if got := at(t, arr, "kind"); got != "array" {
				t.Fatalf("array.kind = %v, want array", got)
			}
			elems := at(t, arr, "elems").([]any)
			if len(elems) != 3 {
				t.Fatalf("elems = %v, want 3 entries", elems)
			}
		},
	},
	{
		Name:   "param default expansion",
		Script: `echo ${x:-def}`,
		Check: func(t *testing.T, root any) {
			part := at(t, root, "statements", 0, "command", "args", 1, "parts", 0)
			if got := at(t, part, "kind"); got != "param_expansion" {
				t.Fatalf("kind = %v, want param_expansion", got)
			}
			exp := at(t, part, "expansion")
			if got := at(t, exp, "op"); got != ":-" {
				t.Errorf("expansion.op = %v, want :-", got)
			}
		},
	},
	{
		Name:   "param slice expansion",
		Script: `echo ${x:1:2}`,
		Check: func(t *testing.T, root any) {
			part := at(t, root, "statements", 0, "command", "args", 1, "parts", 0)
			slice := at(t, part, "slice")
			if got := at(t, slice, "kind"); got != "param_slice" {
				t.Fatalf("slice.kind = %v, want param_slice", got)
			}
		},
	},
	{
		Name:   "param replace expansion",
		Script: `echo ${x/a/b}`,
		Check: func(t *testing.T, root any) {
			part := at(t, root, "statements", 0, "command", "args", 1, "parts", 0)
			repl := at(t, part, "replace")
			if got := at(t, repl, "kind"); got != "param_replace" {
				t.Fatalf("replace.kind = %v, want param_replace", got)
			}
		},
	},
	{
		Name:   "command substitution",
		Script: `echo $(true)`,
		Check: func(t *testing.T, root any) {
			part := at(t, root, "statements", 0, "command", "args", 1, "parts", 0)
			if got := at(t, part, "kind"); got != "command_substitution" {
				t.Fatalf("kind = %v, want command_substitution", got)
			}
		},
	},
	{
		Name:   "process substitution",
		Script: `diff <(echo a) <(echo b)`,
		Check: func(t *testing.T, root any) {
			part := at(t, root, "statements", 0, "command", "args", 1, "parts", 0)
			if got := at(t, part, "kind"); got != "process_substitution" {
				t.Fatalf("kind = %v, want process_substitution", got)
			}
			if got := at(t, part, "op"); got != "<(" {
				t.Errorf("op = %v, want <(", got)
			}
		},
	},
	{
		Name:   "arithmetic expansion",
		Script: `echo $((1+2*3))`,
		Check: func(t *testing.T, root any) {
			part := at(t, root, "statements", 0, "command", "args", 1, "parts", 0)
			if got := at(t, part, "kind"); got != "arithmetic_expansion" {
				t.Fatalf("kind = %v, want arithmetic_expansion", got)
			}
			expr := at(t, part, "expr")
			if got := at(t, expr, "kind"); got != "arithmetic_binary" {
				t.Fatalf("expr.kind = %v, want arithmetic_binary", got)
			}
			if got := at(t, expr, "op"); got != "+" {
				t.Errorf("expr.op = %v, want +", got)
			}
		},
	},
	{
		Name:   "extended test clause",
		Script: `[[ -f x ]]`,
		Check: func(t *testing.T, root any) {
			cmd := at(t, root, "statements", 0, "command")
			if got := at(t, cmd, "kind"); got != "test_command" {
				t.Fatalf("kind = %v, want test_command", got)
			}
			expr := at(t, cmd, "expr")
			if got := at(t, expr, "kind"); got != "test_unary" {
				t.Fatalf("expr.kind = %v, want test_unary", got)
			}
			if got := at(t, expr, "op"); got != "-f" {
				t.Errorf("expr.op = %v, want -f", got)
			}
		},
	},
	{
		Name:   "brace expansion stays literal",
		Script: `echo {a,b,c}`,
		Check: func(t *testing.T, root any) {
			word := at(t, root, "statements", 0, "command", "args", 1)
			if got := at(t, word, "text"); got != "{a,b,c}" {
				t.Errorf("text = %v, want {a,b,c}", got)
			}
			if got := at(t, word, "parts", 0, "kind"); got != "literal" {
				t.Errorf("parts[0].kind = %v, want literal (brace expansion is left unsplit)", got)
			}
		},
	},
}

func TestParseNodeKinds(t *testing.T) {
	for _, tt := range parseTests {
		t.Run(tt.Name, func(t *testing.T) {
			out, err := runParse(t, "--", tt.Script)
			if exitCode(t, err) != 0 {
				t.Fatalf("exit code = %d, want 0; err = %v; stdout = %s", exitCode(t, err), err, out)
			}
			tt.Check(t, decodeJSON(t, out))
		})
	}
}

func TestParsePosFlagOmittedByDefault(t *testing.T) {
	out, err := runParse(t, "--", "echo hi")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; err = %v", exitCode(t, err), err)
	}
	root := decodeJSON(t, out)
	if _, ok := root.(map[string]any)["pos"]; ok {
		t.Errorf("pos present without --pos: %v", root)
	}
}

func TestParsePosFlagIncludesPosition(t *testing.T) {
	out, err := runParse(t, "--pos", "--", "echo hi")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; err = %v", exitCode(t, err), err)
	}
	root := decodeJSON(t, out)
	if got := at(t, root, "pos", "start", "offset"); got != float64(0) {
		t.Errorf("pos.start.offset = %v, want 0", got)
	}
	if got := at(t, root, "pos", "end", "offset"); got != float64(7) {
		t.Errorf("pos.end.offset = %v, want 7", got)
	}
}

func TestParseFlatFlag(t *testing.T) {
	out, err := runParse(t, "--flat", "--", `mkdir -p $(echo "foo")`)
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; err = %v", exitCode(t, err), err)
	}

	root := decodeJSON(t, out)
	entries, ok := root.([]any)
	if !ok {
		t.Fatalf("--flat output = %T, want a bare array", root)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %v, want 2 (outer mkdir, inner echo)", entries)
	}
	if got := at(t, entries[0], "kind"); got != "command" {
		t.Errorf("entries[0].kind = %v, want command", got)
	}
	if got := at(t, entries[0], "args", 0, "text"); got != "mkdir" {
		t.Errorf("entries[0].args[0].text = %v, want mkdir", got)
	}
	if got := at(t, entries[1], "args", 0, "text"); got != "echo" {
		t.Errorf("entries[1].args[0].text = %v, want echo (found via the command substitution)", got)
	}
}

func TestParseSyntaxErrorIsUsageError(t *testing.T) {
	_, err := runParse(t, "--", "echo && ")
	if exitCode(t, err) != 2 {
		t.Fatalf("exit code = %d, want 2; err = %v", exitCode(t, err), err)
	}
}

func TestParseUnsupportedLangIsUsageError(t *testing.T) {
	_, err := runParse(t, "--lang", "cobol", "--", "echo hi")
	if exitCode(t, err) != 2 {
		t.Fatalf("exit code = %d, want 2; err = %v", exitCode(t, err), err)
	}
}

func TestParseFileFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("echo hi"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	out, err := runParse(t, "--file", path)
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; err = %v", exitCode(t, err), err)
	}
	if got := at(t, decodeJSON(t, out), "statements", 0, "command", "args", 0, "text"); got != "echo" {
		t.Errorf("args[0].text = %v, want echo", got)
	}
}

func TestParseFileUnreadableIsGeneralError(t *testing.T) {
	_, err := runParse(t, "--file", filepath.Join(t.TempDir(), "missing.sh"))
	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; err = %v", exitCode(t, err), err)
	}
}

func TestResolveInputPositionalTakesPrecedence(t *testing.T) {
	opts := &parseOptions{}
	got, err := opts.resolveInput(cobraCommandForTest(), []string{"echo hi"}, strings.NewReader("unused"), false)
	if err != nil {
		t.Fatalf("resolveInput: %v", err)
	}
	if got != "echo hi" {
		t.Errorf("resolveInput = %q, want %q", got, "echo hi")
	}
}

func TestResolveInputReadsStdinWhenPiped(t *testing.T) {
	opts := &parseOptions{}
	got, err := opts.resolveInput(cobraCommandForTest(), nil, strings.NewReader("echo hi"), false)
	if err != nil {
		t.Fatalf("resolveInput: %v", err)
	}
	if got != "echo hi" {
		t.Errorf("resolveInput = %q, want %q", got, "echo hi")
	}
}

func TestResolveInputNoInputIsUsageError(t *testing.T) {
	opts := &parseOptions{}
	_, err := opts.resolveInput(cobraCommandForTest(), nil, strings.NewReader(""), true)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("resolveInput error = %v, want *ExitError{Code: 2}", err)
	}
}

func TestResolveInputScriptAndFileConflict(t *testing.T) {
	opts := &parseOptions{File: "somefile.sh"}
	_, err := opts.resolveInput(cobraCommandForTest(), []string{"echo hi"}, strings.NewReader(""), false)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("resolveInput error = %v, want *ExitError{Code: 2}", err)
	}
}
