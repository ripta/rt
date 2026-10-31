package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/model"
)

// wrapperEnv is a PATH with a wrapper program and an inner program, plus a second inner program
// the wrapper prefers. The wrapper script puts the second directory first on PATH, as `uv run`
// does, so the file it runs shows whether the gate pinned the one it judged.
type wrapperEnv struct {
	// Wrap and Tool are the canonical paths of the wrapper and the judged inner program.
	Wrap string
	Tool string

	// Bin holds both programs.
	Bin string
}

func newWrapperEnv(t *testing.T) wrapperEnv {
	t.Helper()

	t.Setenv("TMPDIR", t.TempDir())

	bin := t.TempDir()
	evil := t.TempDir()

	plantScript(t, evil, "tool", "evil")
	tool := plantScript(t, bin, "tool", "good")

	wrap := filepath.Join(bin, "wrap")
	body := "#!/bin/sh\nPATH=\"" + evil + ":$PATH\"\nexport PATH\nexec \"$@\"\n"
	if err := os.WriteFile(wrap, []byte(body), 0o755); err != nil {
		t.Fatalf("planting the wrapper: %v", err)
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	return wrapperEnv{Wrap: canonicalPath(t, wrap), Tool: canonicalPath(t, tool), Bin: bin}
}

// rules writes a project file with a path-form wrapper rule for the wrapper and the given
// allow entries.
func (w wrapperEnv) rules(entries ...string) string {
	return "version: 1\nallow:\n  - wrapper: [" + w.Wrap + "]\n" + strings.Join(entries, "")
}

var wrapperPinTests = []struct {
	Name  string
	Rules func(w wrapperEnv) string
	Want  string
}{
	{
		Name:  "a path-form inner rule pins the judged file",
		Rules: func(w wrapperEnv) string { return w.rules("  - prefix: [" + w.Tool + "]\n") },
		Want:  "good",
	},
	{
		Name:  "a bare-name inner rule leaves the wrapper's own lookup",
		Rules: func(w wrapperEnv) string { return w.rules("  - prefix: [tool]\n") },
		Want:  "evil",
	},
	{
		Name: "an ordinary allow on the whole argv decides, so nothing is pinned",
		Rules: func(w wrapperEnv) string {
			return w.rules("  - prefix: [" + w.Tool + "]\n  - exact: [" + w.Wrap + ", tool]\n")
		},
		Want: "evil",
	},
}

func TestWrappedInnerCommandRunsPinnedFile(t *testing.T) {
	for _, test := range wrapperPinTests {
		t.Run(test.Name, func(t *testing.T) {
			w := newWrapperEnv(t)
			g := newTestGate(t, test.Rules(w), false)

			typed := []string{"wrap", "tool"}
			_, out, err := handleRun(context.Background(), nil, g, nil, "", runInput{Command: typed})
			if err != nil {
				t.Fatalf("handleRun: %v", err)
			}
			if got := strings.TrimSpace(out.StdoutExcerpt); got != test.Want {
				t.Errorf("inner program printed %q, want %q (stderr %q)", got, test.Want, out.StderrExcerpt)
			}

			meta, err := model.ReadMeta(filepath.Join(model.CaptureRoot(), out.ID))
			if err != nil {
				t.Fatalf("ReadMeta: %v", err)
			}
			if !slices.Equal(meta.Command, typed) {
				t.Errorf("recorded command = %q, want the typed argv %q", meta.Command, typed)
			}
		})
	}
}

func TestWrappedInnerCommandRunsPinnedFileInPool(t *testing.T) {
	w := newWrapperEnv(t)
	g := newTestGate(t, w.rules("  - prefix: ["+w.Tool+"]\n"), false)

	_, out, err := handleRunMany(context.Background(), nil, g, nil, "", runManyInput{Commands: [][]string{{"wrap", "tool"}}})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].RunID == "" {
		t.Fatalf("pool runs = %+v, want one run with an id", out.Runs)
	}

	stdout, err := os.ReadFile(filepath.Join(model.CaptureRoot(), out.Runs[0].RunID, "stdout"))
	if err != nil {
		t.Fatalf("reading the run's stdout: %v", err)
	}
	if got := strings.TrimSpace(string(stdout)); got != "good" {
		t.Errorf("inner program printed %q, want good", got)
	}
}

func TestGatePinsOnlyInnerLayersOfTheDecidingChain(t *testing.T) {
	w := newWrapperEnv(t)
	g := newTestGate(t, w.rules("  - prefix: ["+w.Tool+"]\n"), false)

	outcome, err := g.check(testCall([]string{"wrap", "tool"}, []string{"wrap", "wrap", "tool"}))
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	if got := outcome.Rewrites[0]; len(got) != 1 || got[1] != w.Tool {
		t.Errorf("rewrites for wrap tool = %v, want only offset 1 pinned to %s", got, w.Tool)
	}
	if got := outcome.Rewrites[1]; len(got) != 2 || got[1] != w.Wrap || got[2] != w.Tool {
		t.Errorf("rewrites for wrap wrap tool = %v, want offsets 1 and 2 pinned", got)
	}
}

// wrappedRound runs the first call of a wrapped command that nothing allows and returns the gate,
// the call, and the pending result.
func wrappedRound(t *testing.T, w wrapperEnv, rules string, argv []string) (*gate, approvalCall, *mcpsdk.CallToolResult, string) {
	t.Helper()

	root := t.TempDir()
	g := newTestGateAt(t, root, rules, false)

	call := testCall(argv)
	first, err := g.check(call)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if first.Pending == nil {
		t.Fatalf("first call returned no input requests")
	}

	return g, call, first.Pending, root
}

func formJSON(t *testing.T, pending *mcpsdk.CallToolResult, id string) string {
	t.Helper()

	params, ok := pending.InputRequests[id].(*mcpsdk.ElicitParams)
	if !ok {
		t.Fatalf("input request %q = %T, want an elicitation", id, pending.InputRequests[id])
	}

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encoding the form: %v", err)
	}

	return string(raw)
}

var wrappedRememberTests = []struct {
	Name     string
	Content  map[string]any
	WantFile string
	// WantPin is the offset 1 path, empty when the inner command keeps the wrapper's lookup.
	WantPin func(w wrapperEnv) string
}{
	{
		Name:    "approved without remember pins the path the form showed",
		Content: map[string]any{"remember": false},
		WantPin: func(w wrapperEnv) string { return w.Tool },
	},
	{
		Name:     "a remembered path-form rule pins",
		Content:  map[string]any{"remember": true},
		WantFile: "exact: [%s, x]",
		WantPin:  func(w wrapperEnv) string { return w.Tool },
	},
	{
		Name:     "a remembered bare-name rule passes through",
		Content:  map[string]any{"remember": true, "rule": "[tool, x]"},
		WantFile: "exact: [tool, x]",
		WantPin:  func(wrapperEnv) string { return "" },
	},
	{
		Name:    "a remembered rule that does not match the layer is skipped and the shown path stays pinned",
		Content: map[string]any{"remember": true, "rule": "[other]"},
		WantPin: func(w wrapperEnv) string { return w.Tool },
	},
}

func TestWrappedCommandApprovalRound(t *testing.T) {
	for _, test := range wrappedRememberTests {
		t.Run(test.Name, func(t *testing.T) {
			w := newWrapperEnv(t)
			argv := []string{"wrap", "tool", "x"}
			g, call, pending, root := wrappedRound(t, w, w.rules(), argv)

			form := formJSON(t, pending, "approval-1")
			for _, want := range []string{"Allow rule", "An exact rule fixes every argument", "inner command", "wrap tool x"} {
				if !strings.Contains(form, want) {
					t.Errorf("form lacks %q:\n%s", want, form)
				}
			}
			if !strings.Contains(form, w.Tool+", x]") {
				t.Errorf("the pre-filled rule should be for the inner command %s:\n%s", w.Tool, form)
			}

			answers := mcpsdk.InputResponseMap{"approval-1": accept(test.Content)}
			second, err := g.check(retryCall(call, pending, answers))
			if err != nil {
				t.Fatalf("retry: %v", err)
			}

			pin := test.WantPin(w)
			got := second.Rewrites[0]
			switch {
			case pin == "" && len(got) != 0:
				t.Errorf("rewrites = %v, want none", got)
			case pin != "" && (len(got) != 1 || got[1] != pin):
				t.Errorf("rewrites = %v, want offset 1 pinned to %s", got, pin)
			}

			if test.WantFile != "" {
				written, err := readProjectIfPresent(root)
				if err != nil {
					t.Fatalf("reading the project file: %v", err)
				}

				want := test.WantFile
				if strings.Contains(want, "%s") {
					want = strings.Replace(want, "%s", w.Tool, 1)
				}
				if !strings.Contains(written, want) {
					t.Errorf("project file lacks %q:\n%s", want, written)
				}
				if strings.Contains(written, "wrapper: [tool") {
					t.Errorf("remember must never create a wrapper rule:\n%s", written)
				}
			}
		})
	}
}

func TestWrapperProgramPromptSuggestsExactRule(t *testing.T) {
	w := newWrapperEnv(t)
	rules := "version: 1\nallow:\n  - wrapper: [" + w.Wrap + ", -n, \"19\"]\n  - prefix: [" + w.Tool + "]\n"
	argv := []string{"wrap", "-n", "10", "tool"}
	g, call, pending, root := wrappedRound(t, w, rules, argv)

	form := formJSON(t, pending, "approval-1")
	for _, want := range []string{"Allow rule", "heads a wrapper rule", "always exact", "allows only this whole command"} {
		if !strings.Contains(form, want) {
			t.Errorf("form lacks %q:\n%s", want, form)
		}
	}

	answers := mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true})}
	second, err := g.check(retryCall(call, pending, answers))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if second.Warning != "" {
		t.Errorf("remember warning = %q", second.Warning)
	}

	written, err := readProjectIfPresent(root)
	if err != nil {
		t.Fatalf("reading the project file: %v", err)
	}
	if !strings.Contains(written, "exact: ["+w.Wrap+", -n, \"10\", tool]") {
		t.Errorf("project file lacks the exact rule:\n%s", written)
	}
	if strings.Contains(written, "prefix: ["+w.Wrap) || strings.Count(written, "wrapper:") != 1 {
		t.Errorf("remember must write neither a prefix rule for the wrapper nor a wrapper rule:\n%s", written)
	}

	again, err := g.check(testCall(argv))
	if err != nil || again.Pending != nil {
		t.Errorf("the remembered exact rule should allow the command now: pending=%v err=%v", again.Pending != nil, err)
	}

	other, err := g.check(testCall([]string{"wrap", "-n", "10", "tool", "more"}))
	if err != nil || other.Pending == nil {
		t.Errorf("a longer command should still prompt: pending=%v err=%v", other.Pending != nil, err)
	}
}

func TestGuardCasePromptNamesTheNearMissRule(t *testing.T) {
	w := newWrapperEnv(t)
	rules := "version: 1\nallow:\n  - wrapper: [" + w.Wrap + ", -n, \"19\"]\n  - prefix: [" + w.Tool + "]\n"
	_, _, pending, _ := wrappedRound(t, w, rules, []string{"wrap", "-n", "19", "--", "tool"})

	form := formJSON(t, pending, "approval-1")
	for _, want := range []string{"Allow rule", "heads a wrapper rule", "stopped at the inner command's leading", "add a second wrapper rule ending in --", "wrapper [" + w.Wrap + ", -n, 19]"} {
		if !strings.Contains(form, want) {
			t.Errorf("form lacks %q:\n%s", want, form)
		}
	}
}

func TestRememberDoesNotWriteALeadingDashRule(t *testing.T) {
	w := newWrapperEnv(t)
	g, call, pending, root := wrappedRound(t, w, w.rules(), []string{"wrap", "tool"})

	answers := mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true, "rule": "[-n]"})}
	second, err := g.check(retryCall(call, pending, answers))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !strings.Contains(second.Warning, "does not match the command") {
		t.Errorf("warning = %q, want the rule to be rejected against the command", second.Warning)
	}

	written, _ := readProjectIfPresent(root)
	if strings.Contains(written, "-n") {
		t.Errorf("a rule starting with '-' was written:\n%s", written)
	}
}

func TestInnerResolutionChangeBetweenCallsRefuses(t *testing.T) {
	w := newWrapperEnv(t)
	argv := []string{"wrap", "tool"}
	g, _, pending, _ := wrappedRound(t, w, w.rules(), argv)

	other := t.TempDir()
	plantScript(t, other, "tool", "other")
	t.Setenv("PATH", other+string(os.PathListSeparator)+os.Getenv("PATH"))

	retry := retryCall(testCall(argv), pending, mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true})})

	_, err := g.check(retry)
	if err == nil || !strings.Contains(err.Error(), "inner command `tool` now resolves to a different executable") {
		t.Fatalf("retry error = %v, want a refusal naming the inner command", err)
	}
}

func TestWrappedPromptsShareFormsWithTheUnwrappedCommand(t *testing.T) {
	w := newWrapperEnv(t)
	g := newTestGate(t, w.rules(), false)

	first, err := g.check(testCall([]string{"wrap", "tool", "a"}, []string{"tool", "a"}))
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if first.Pending == nil || len(first.Pending.InputRequests) != 1 {
		t.Fatalf("want one shared form, got %+v", first.Pending)
	}

	form := formJSON(t, first.Pending, "approval-1")
	for _, want := range []string{"2 commands", "wrap tool a", "tool a"} {
		if !strings.Contains(form, want) {
			t.Errorf("form lacks %q:\n%s", want, form)
		}
	}
}

func TestPlanFormsKeysOnTheWholeArgv(t *testing.T) {
	w := newWrapperEnv(t)
	g := newTestGate(t, "version: 1\nallow:\n  - wrapper: ["+w.Wrap+", -n, \"19\"]\n", false)

	call := testCall([]string{"wrap", "-n", "10", "tool"}, []string{"wrap", "-n", "11", "tool"}, []string{"tool", "c"})
	targets := make(map[int]promptTarget)
	for i := range call.Argvs {
		targets[i] = promptTarget{Argv: call.Argvs[i], Resolution: call.Resolutions[i]}
	}

	forms := planForms(g.store.Ruleset(), []int{0, 1, 2}, targets)
	if len(forms) != 3 {
		t.Fatalf("forms = %d, want one per distinct argv", len(forms))
	}

	heads := []bool{forms[0].Suggestion.HeadsWrapper, forms[1].Suggestion.HeadsWrapper, forms[2].Suggestion.HeadsWrapper}
	if want := []bool{true, true, false}; !slices.Equal(heads, want) {
		t.Errorf("heads wrapper = %v, want %v", heads, want)
	}
}

func TestWrappedRefusalsNameTheLayerAndRule(t *testing.T) {
	w := newWrapperEnv(t)

	t.Run("a deny on the inner command", func(t *testing.T) {
		g := newTestGate(t, w.rules("")+"deny:\n  - prefix: [tool]\n", false)

		_, err := g.check(testCall([]string{"wrap", "tool", "x"}))
		if err == nil || !strings.Contains(err.Error(), "the inner command `tool x` matches the deny rule prefix [tool]") {
			t.Errorf("err = %v, want it to name the inner command and the deny rule", err)
		}
	})

	t.Run("a wrapper rule that lacks the env permit", func(t *testing.T) {
		g := newTestGate(t, w.rules("  - prefix: ["+w.Tool+"]\n    permit_unsafe_envs: [LD_PRELOAD]\n"), false)

		call := testCall([]string{"wrap", "tool"})
		call.Env = map[string]string{"LD_PRELOAD": "/x"}

		_, err := g.check(call)
		if err == nil || !strings.Contains(err.Error(), "wrapper ["+w.Wrap+"]") || !strings.Contains(err.Error(), "LD_PRELOAD") {
			t.Errorf("err = %v, want it to name the wrapper rule and the variable", err)
		}
	})

	t.Run("an inner rule that lacks the env permit", func(t *testing.T) {
		rules := "version: 1\nallow:\n  - wrapper: [" + w.Wrap + "]\n    permit_unsafe_envs: [LD_PRELOAD]\n  - prefix: [" + w.Tool + "]\n"
		g := newTestGate(t, rules, false)

		call := testCall([]string{"wrap", "tool"})
		call.Env = map[string]string{"LD_PRELOAD": "/x"}

		_, err := g.check(call)
		if err == nil || !strings.Contains(err.Error(), "the inner command `tool`") {
			t.Errorf("err = %v, want it to name the inner command", err)
		}
	})
}
