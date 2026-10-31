package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
)

// multiRound marks a call as coming from a client that takes more than one input-required round.
func multiRound(call approvalCall) approvalCall {
	call.MultiRound = true

	return call
}

// firstRound runs the first call and fails unless it ends with input requests.
func firstRound(t *testing.T, g *gate, call approvalCall) *mcpsdk.CallToolResult {
	t.Helper()

	first, err := g.check(call)
	if err != nil || first.Pending == nil {
		t.Fatalf("first call = %+v, %v; want input requests", first, err)
	}

	return first.Pending
}

func TestSecondRoundAsksOnlyTheFailedForms(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	call := multiRound(testCall(echoHi, []string{"printf", "x"}))
	pending := firstRound(t, g, call)

	answers := mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
		"approval-2": accept(map[string]any{"remember": true, "rule": "[printf, y]", "allow_extra_args": true}),
	}

	second, err := g.check(retryCall(call, pending, answers))
	if err != nil {
		t.Fatalf("round 1 retry: %v", err)
	}
	if second.Pending == nil {
		t.Fatalf("a failed edit on a multi-round client should ask again")
	}
	if second.Pending.RequestState == "" || second.Pending.RequestState == pending.RequestState {
		t.Errorf("round 2 nonce = %q, want a fresh one", second.Pending.RequestState)
	}
	if len(second.Pending.InputRequests) != 1 {
		t.Fatalf("round 2 forms = %d, want only the failed one", len(second.Pending.InputRequests))
	}

	form := formJSON(t, second.Pending, "approval-2")
	for _, want := range []string{
		"Approval 2 of 2",
		"needs another look",
		"The rule [printf, y] does not match the command `printf x`",
		"The accepted edits are leaving the rule unchanged",
		"The field holds the edit that failed",
		`"default":"[printf, y]"`,
		`"allow_extra_args":{"type":"boolean","title":"Allow extra arguments","description":"Allow extra arguments after the unchanged command. A trimmed rule always allows them.","default":true}`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("round 2 form lacks %q:\n%s", want, form)
		}
	}

	if file, _ := readProjectIfPresent(root); file != "version: 1\n" {
		t.Errorf("a remember was written before round 2 resolved:\n%s", file)
	}

	if _, err := g.check(retryCall(call, pending, answers)); !errors.Is(err, ErrPendingApproval) {
		t.Errorf("reusing the round 1 nonce: err = %v, want ErrPendingApproval", err)
	}

	final, err := g.check(retryCall(call, second.Pending, mcpsdk.InputResponseMap{
		"approval-2": accept(map[string]any{"remember": true, "rule": "[printf, x]"}),
	}))
	if err != nil || final.Pending != nil || final.Warning != "" {
		t.Fatalf("round 2 retry = %+v, %v; want a clean run", final, err)
	}

	file, _ := readProjectIfPresent(root)
	for _, want := range []string{"prefix: [echo]", "exact: ["} {
		if !strings.Contains(file, want) {
			t.Errorf("project file lacks %q after round 2:\n%s", want, file)
		}
	}
	if strings.Count(file, "printf, x]") != 1 {
		t.Errorf("project file should hold one printf rule:\n%s", file)
	}

	again, err := g.check(testCall(echoHi, []string{"printf", "x"}))
	if err != nil || again.Pending != nil {
		t.Errorf("remembered rules should allow both commands now: pending=%v err=%v", again.Pending != nil, err)
	}
}

type secondRoundAnswerTest struct {
	Name   string
	Answer *mcpsdk.ElicitResult

	WantPending bool
	WantErr     string
	WantFile    string
	AbsentFile  string
}

var secondRoundAnswerTests = []secondRoundAnswerTest{
	{Name: "a matching edit saves", Answer: accept(map[string]any{"remember": true, "rule": "[echo]"}), WantFile: "prefix: [echo]"},
	{Name: "an unchecked remember runs without saving", Answer: accept(map[string]any{"remember": false}), AbsentFile: "echo"},
	{Name: "a decline refuses", Answer: decline(), WantErr: "declined at the approval prompt", AbsentFile: "echo"},
	{Name: "a cancel refuses", Answer: cancel(), WantErr: "declined at the approval prompt", AbsentFile: "echo"},
	{Name: "a further failure asks again", Answer: accept(map[string]any{"remember": true, "rule": "[echo, nope]"}), WantPending: true},
	{Name: "an unreadable field asks again", Answer: accept(map[string]any{"remember": true, "rule": "not a list"}), WantPending: true},
}

func TestSecondRoundAnswers(t *testing.T) {
	for _, test := range secondRoundAnswerTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			root := t.TempDir()
			g := newTestGateAt(t, root, "version: 1\n", false)

			call := multiRound(testCall(echoHi))
			pending := firstRound(t, g, call)

			second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
				"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, bye]"}),
			}))
			if err != nil || second.Pending == nil {
				t.Fatalf("round 1 retry = %+v, %v; want a second round", second, err)
			}

			final, err := g.check(retryCall(call, second.Pending, mcpsdk.InputResponseMap{"approval-1": test.Answer}))
			if test.WantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.WantErr) {
					t.Fatalf("err = %v, want %q", err, test.WantErr)
				}
			} else if err != nil {
				t.Fatalf("round 2 retry: %v", err)
			}

			if (final.Pending != nil) != test.WantPending {
				t.Fatalf("pending = %v, want %v", final.Pending != nil, test.WantPending)
			}
			if test.WantPending {
				if final.Pending.RequestState == second.Pending.RequestState {
					t.Errorf("round 3 reused the round 2 nonce")
				}
				if form := formJSON(t, final.Pending, "approval-1"); !strings.Contains(form, "needs another look") {
					t.Errorf("a repeated round should say so:\n%s", form)
				}
			}

			file, _ := readProjectIfPresent(root)
			if test.WantFile != "" && !strings.Contains(file, test.WantFile) {
				t.Errorf("project file lacks %q:\n%s", test.WantFile, file)
			}
			if test.AbsentFile != "" && strings.Contains(file, test.AbsentFile) {
				t.Errorf("project file should not hold %q:\n%s", test.AbsentFile, file)
			}
		})
	}
}

func TestUnreadableFieldAsksAgainWithTheOriginalCommand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

	call := multiRound(testCall(echoHi))
	pending := firstRound(t, g, call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "not a list"}),
	}))
	if err != nil || second.Pending == nil {
		t.Fatalf("round 1 retry = %+v, %v; want a second round", second, err)
	}

	form := formJSON(t, second.Pending, "approval-1")
	for _, want := range []string{"rule must be a YAML list", "The field holds the original command"} {
		if !strings.Contains(form, want) {
			t.Errorf("form lacks %q:\n%s", want, form)
		}
	}
}

func TestSecondRoundDivergenceAnswerCarries(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, baseRules, false)
	writeProject(t, root, editRules)

	call := multiRound(testCall(echoHi))
	pending := firstRound(t, g, call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, bye]"}),
		"divergence": accept(map[string]any{"choice": divergeOverwrite}),
	}))
	if err != nil || second.Pending == nil {
		t.Fatalf("round 1 retry = %+v, %v; want a second round", second, err)
	}
	if _, has := second.Pending.InputRequests[divergenceID]; has {
		t.Errorf("round 2 asks the divergence question again")
	}

	final, err := g.check(retryCall(call, second.Pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
	}))
	if err != nil || final.Pending != nil || final.Warning != "" {
		t.Fatalf("round 2 retry = %+v, %v", final, err)
	}

	file, _ := readProjectIfPresent(root)
	if !strings.Contains(file, "prefix: [echo]") || strings.Contains(file, "npm") {
		t.Errorf("the carried overwrite choice should drop the disk change and add the rule:\n%s", file)
	}
}

type noSecondRoundTest struct {
	Name         string
	Project      string
	EditFirst    string
	AllowBetween []string
	Argvs        [][]string
	Answers      mcpsdk.InputResponseMap

	WantErr     string
	WantWarning string
	AbsentFile  string
}

var noSecondRoundTests = []noSecondRoundTest{
	{
		Name:    "a rejected edit beside a declined form refuses",
		Project: "version: 1\n",
		Argvs:   [][]string{echoHi, {"printf", "x"}},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, bye]"}),
			"approval-2": decline(),
		},
		WantErr:    "declined at the approval prompt",
		AbsentFile: "echo",
	},
	{
		Name:         "a rejected edit on a form whose commands are all allowed warns",
		Project:      "version: 1\n",
		AllowBetween: []string{"echo", "hi"},
		Argvs:        [][]string{echoHi},
		Answers:      mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, bye]"})},
		WantWarning:  "does not match the command",
		AbsentFile:   "bye",
	},
	{
		Name:      "a rejected edit when the divergence answer is skip runs with a note",
		Project:   baseRules,
		EditFirst: editRules,
		Argvs:     [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, bye]"}),
			"divergence": accept(map[string]any{"choice": divergeSkip}),
		},
		WantWarning: "does not match the command",
		AbsentFile:  "bye",
	},
	{
		Name:      "a rejected edit when the divergence form went unanswered runs with a note",
		Project:   baseRules,
		EditFirst: editRules,
		Argvs:     [][]string{echoHi},
		Answers:   mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, bye]"})},

		WantWarning: "does not match the command",
		AbsentFile:  "bye",
	},
}

func TestNoSecondRound(t *testing.T) {
	for _, test := range noSecondRoundTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			root := t.TempDir()
			g := newTestGateAt(t, root, test.Project, false)
			if test.EditFirst != "" {
				writeProject(t, root, test.EditFirst)
			}

			call := multiRound(testCall(test.Argvs...))
			pending := firstRound(t, g, call)

			if test.AllowBetween != nil {
				if err := g.store.AppendProjectAllowPrefix(test.AllowBetween, approve.WriteDirect); err != nil {
					t.Fatalf("adding the allow rule: %v", err)
				}
			}

			second, err := g.check(retryCall(call, pending, test.Answers))
			if test.WantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.WantErr) || !strings.Contains(err.Error(), "skipping remember:") {
					t.Fatalf("err = %v, want %q with a skipped-remember note", err, test.WantErr)
				}
			} else if err != nil {
				t.Fatalf("retry: %v", err)
			}

			if second.Pending != nil {
				t.Fatalf("the retry asked again")
			}
			if !strings.Contains(second.Warning, test.WantWarning) {
				t.Errorf("Warning = %q, want %q", second.Warning, test.WantWarning)
			}

			file, _ := readProjectIfPresent(root)
			if test.AbsentFile != "" && strings.Contains(file, test.AbsentFile) {
				t.Errorf("project file should not hold %q:\n%s", test.AbsentFile, file)
			}
		})
	}
}

func TestNoSecondRoundWhenTheProjectFileIsUnreadable(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	if err := os.Chmod(projectFile(root), 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(projectFile(root), 0o600) })

	if _, err := os.ReadFile(projectFile(root)); err == nil {
		t.Skip("the file is still readable, as for root")
	}

	call := multiRound(testCall(echoHi))
	pending := firstRound(t, g, call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, bye]"}),
	}))
	if err != nil || second.Pending != nil {
		t.Fatalf("retry = %+v, %v; want a run", second, err)
	}
	if !strings.Contains(second.Warning, "does not match the command") {
		t.Errorf("Warning = %q, want the failed edit named", second.Warning)
	}
}

func TestSecondRoundRefusesASwappedBinary(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

	call := multiRound(testCall(echoHi))
	pending := firstRound(t, g, call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, bye]"}),
	}))
	if err != nil || second.Pending == nil {
		t.Fatalf("round 1 retry = %+v, %v; want a second round", second, err)
	}

	swapped := retryCall(call, second.Pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
	})
	swapped.Resolutions = []*model.Resolution{{Argv: echoHi, Resolved: "/elsewhere/echo", Canonical: "/elsewhere/echo"}}

	_, err = g.check(swapped)
	if err == nil || !strings.Contains(err.Error(), "now resolves to a different executable") || !errors.Is(err, ErrPendingApproval) {
		t.Fatalf("err = %v, want a stale resolution refusal", err)
	}
}

func TestSecondRoundKeepsEachFormsOwnPosition(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

	call := multiRound(testCall(echoHi, []string{"printf", "x"}, []string{"cat", "y"}))
	pending := firstRound(t, g, call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, bye]"}),
		"approval-2": accept(map[string]any{"remember": false}),
		"approval-3": accept(map[string]any{"remember": true, "rule": "[cat, z]"}),
	}))
	if err != nil || second.Pending == nil {
		t.Fatalf("round 1 retry = %+v, %v; want a second round", second, err)
	}

	for id, want := range map[string]string{"approval-1": "Approval 1 of 3", "approval-3": "Approval 3 of 3"} {
		params, ok := second.Pending.InputRequests[id].(*mcpsdk.ElicitParams)
		if !ok || !strings.HasPrefix(params.Message, want) {
			t.Errorf("%s message = %+v, want it to start with %q", id, second.Pending.InputRequests[id], want)
		}
	}
	if _, has := second.Pending.InputRequests["approval-2"]; has {
		t.Errorf("round 2 asks the form that already passed")
	}
}

type wrapperEditTest struct {
	Name    string
	Answer  map[string]any
	WantAsk []string
}

var wrapperEditTests = []wrapperEditTest{
	{Name: "a trimmed field", Answer: map[string]any{"remember": true, "rule": "[WRAP, -n, \"10\"]"}, WantAsk: []string{"shorter than the command", "re-executes its arguments", "respelling the first token and unchecking remember", "write a wrapper rule"}},
	{Name: "a ticked box", Answer: map[string]any{"remember": true, "allow_extra_args": true}, WantAsk: []string{"Untick the box", "respelling the first token and unchecking remember"}},
	{Name: "a trim respelled to a bare name", Answer: map[string]any{"remember": true, "rule": "[wrap, -n]"}, WantAsk: []string{"shorter than the command"}},
}

func TestWrapperProgramEditsAreReasked(t *testing.T) {
	for _, test := range wrapperEditTests {
		t.Run(test.Name, func(t *testing.T) {
			w := newWrapperEnv(t)
			rules := "version: 1\nallow:\n  - wrapper: [" + w.Wrap + ", -n, \"19\"]\n  - prefix: [" + w.Tool + "]\n"
			argv := []string{"wrap", "-n", "10", "tool"}

			g, call, pending, root := wrappedRound(t, w, rules, argv)
			call = multiRound(call)

			form := formJSON(t, pending, "approval-1")
			if strings.Contains(form, "allow_extra_args") {
				t.Errorf("a wrapper program's form offers the box:\n%s", form)
			}

			answer := make(map[string]any)
			for k, v := range test.Answer {
				if s, ok := v.(string); ok {
					v = strings.ReplaceAll(s, "WRAP", w.Wrap)
				}
				answer[k] = v
			}

			second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{"approval-1": accept(answer)}))
			if err != nil || second.Pending == nil {
				t.Fatalf("retry = %+v, %v; want a second round", second, err)
			}

			again := formJSON(t, second.Pending, "approval-1")
			for _, want := range test.WantAsk {
				if !strings.Contains(again, want) {
					t.Errorf("round 2 form lacks %q:\n%s", want, again)
				}
			}
			if strings.Contains(again, "allow_extra_args") {
				t.Errorf("round 2 offers the box for a wrapper program:\n%s", again)
			}

			if file, _ := readProjectIfPresent(root); strings.Contains(file, "exact:") {
				t.Errorf("a rule was written before round 2 resolved:\n%s", file)
			}
		})
	}
}

func TestPlanFormsSplitsAWrapperHeadFromAPlainSpelling(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	links := newLinkedPrograms(t)
	first, second := links.program("echo@1"), links.program("echo@2")

	g := newTestGate(t, "version: 1\nallow:\n  - wrapper: ["+first+"]\n", false)

	call := testCall([]string{first, "a"}, []string{second, "a"})
	targets := map[int]promptTarget{
		0: {Argv: call.Argvs[0], Resolution: call.Resolutions[0]},
		1: {Argv: call.Argvs[1], Resolution: call.Resolutions[1]},
	}

	forms := planForms(g.store.Ruleset(), []int{0, 1}, targets)
	if len(forms) != 2 {
		t.Fatalf("forms = %d, want the wrapper-heading spelling in a form of its own", len(forms))
	}
	if forms[0].Suggestion.HeadsWrapper == forms[1].Suggestion.HeadsWrapper {
		t.Errorf("the forms disagree on nothing: %+v", forms)
	}
}

func TestPrefillThatCannotMatchIsExplainedBeforeTheAnswer(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	call := multiRound(testCall([]string{"./no-such-tool", "x"}))
	pending := firstRound(t, g, call)

	form := formJSON(t, pending, "approval-1")
	for _, want := range []string{"cannot match because this executable has no canonical path", "Edit the first token to no-such-tool"} {
		if !strings.Contains(form, want) {
			t.Errorf("form lacks %q:\n%s", want, form)
		}
	}

	final, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[no-such-tool, x]"}),
	}))
	if err != nil || final.Pending != nil || final.Warning != "" {
		t.Fatalf("the bare-name edit should save: %+v, %v", final, err)
	}

	file, _ := readProjectIfPresent(root)
	if !strings.Contains(file, "exact: [no-such-tool, x]") {
		t.Errorf("project file lacks the bare-name rule:\n%s", file)
	}
}

func TestRememberedRuleMatchesTheApprovedCommand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	for _, rule := range []map[string]any{
		{"remember": true},
		{"remember": true, "allow_extra_args": true},
		{"remember": true, "rule": "[echo, hi]"},
		{"remember": true, "rule": "[echo]"},
	} {
		g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

		call := testCall(echoHi)
		pending := firstRound(t, g, call)

		if _, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{"approval-1": accept(rule)})); err != nil {
			t.Fatalf("remember %v: %v", rule, err)
		}

		again, err := g.check(testCall(echoHi))
		if err != nil || again.Pending != nil {
			t.Errorf("remember %v: the approved command prompts again: pending=%v err=%v", rule, again.Pending != nil, err)
		}
	}
}

func TestRememberedInnerSuggestionRerunsWithoutAPrompt(t *testing.T) {
	for _, content := range []map[string]any{
		{"remember": true},
		{"remember": true, "allow_extra_args": true},
		{"remember": true, "rule": "[tool]"},
	} {
		w := newWrapperEnv(t)
		argv := []string{"wrap", "tool", "x"}
		g, call, pending, _ := wrappedRound(t, w, w.rules(), argv)

		if _, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{"approval-1": accept(content)})); err != nil {
			t.Fatalf("remember %v: %v", content, err)
		}

		again, err := g.check(testCall(argv))
		if err != nil || again.Pending != nil {
			t.Errorf("remember %v: the wrapped command prompts again: pending=%v err=%v", content, again.Pending != nil, err)
		}
	}
}

func TestInnerEditIsCheckedAgainstTheInnerLayer(t *testing.T) {
	w := newWrapperEnv(t)
	argv := []string{"wrap", "tool", "x"}
	g, call, pending, _ := wrappedRound(t, w, w.rules(), argv)
	call = multiRound(call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[wrap, tool, x]"}),
	}))
	if err != nil || second.Pending == nil {
		t.Fatalf("retry = %+v, %v; want a second round", second, err)
	}

	form := formJSON(t, second.Pending, "approval-1")
	if !strings.Contains(form, "does not match the command `tool x`") {
		t.Errorf("the note should name the inner command:\n%s", form)
	}
}

func TestMultiRoundAvailableReadsThePerRequestVersion(t *testing.T) {
	if multiRoundAvailable(nil) {
		t.Errorf("a nil request cannot prompt")
	}
	if !multiRoundAvailable(&mcpsdk.CallToolRequest{}) {
		t.Errorf("a request that carries no version is treated as the newest")
	}
}

func TestApprovalSchemaBox(t *testing.T) {
	t.Parallel()

	plain := approvalSchema(formSpec{Suggestion: approve.Suggestion{Tokens: []string{"go", "test"}}, Path: "/p"})
	if box := plain.Properties[extraArgsField]; box == nil || string(box.Default) != "false" {
		t.Errorf("a plain form should offer the box, default off: %+v", box)
	}
	if !slices.Equal(plain.PropertyOrder, []string{"rule", extraArgsField, "remember"}) {
		t.Errorf("order = %v", plain.PropertyOrder)
	}

	wrapper := approvalSchema(formSpec{Suggestion: approve.Suggestion{Tokens: []string{"nice"}, HeadsWrapper: true}, Path: "/p"})
	if _, has := wrapper.Properties[extraArgsField]; has {
		t.Errorf("a wrapper program's form should omit the box")
	}

	title := plain.Properties["rule"].Title
	if strings.Contains(title, "exact") || strings.Contains(title, "prefix") {
		t.Errorf("the title names a kind: %q", title)
	}

	desc := plain.Properties["rule"].Description
	for _, want := range []string{"An exact rule fixes every argument", "first token decides which programs it covers", "tick the box", "trim it to a leading part"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description lacks %q:\n%s", want, desc)
		}
	}
}

// editScript answers the approval form with a canned result per call, and records the messages it
// was shown.
type editScript struct {
	answers []*mcpsdk.ElicitResult

	mu      sync.Mutex
	shown   []string
	schemas []string
}

func (s *editScript) handle(_ context.Context, req *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.shown = append(s.shown, req.Params.Message)
	raw, _ := json.Marshal(req.Params.RequestedSchema)
	s.schemas = append(s.schemas, string(raw))
	if len(s.shown) > len(s.answers) {
		return nil, fmt.Errorf("unexpected form %d", len(s.shown))
	}

	return s.answers[len(s.shown)-1], nil
}

func TestEndToEndFailedEditIsReaskedOnANewClient(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	script := &editScript{answers: []*mcpsdk.ElicitResult{
		accept(map[string]any{"remember": true, "rule": "[echo, bye]"}),
		accept(map[string]any{"remember": true, "rule": "[echo]"}),
	}}
	cs := connectGate(t, g, protocolMultiRoundTrip, script.handle)

	out, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi"}})
	if err != nil || refusal != "" {
		t.Fatalf("cg_run: err=%v refusal=%q", err, refusal)
	}
	if code, ok := out["exit_code"].(float64); !ok || code != 0 {
		t.Errorf("exit_code = %v, want 0", out["exit_code"])
	}
	if _, has := out["remember_warning"]; has {
		t.Errorf("remember_warning = %v, want none after the second round saved", out["remember_warning"])
	}

	if len(script.shown) != 2 {
		t.Fatalf("forms shown = %d, want 2", len(script.shown))
	}
	if !strings.Contains(script.shown[1], "needs another look") {
		t.Errorf("round 2 message = %q", script.shown[1])
	}

	file, _ := readProjectIfPresent(root)
	if !strings.Contains(file, "prefix: [echo]") {
		t.Errorf("project file lacks the rule:\n%s", file)
	}

	_, refusal, err = callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi"}})
	if err != nil || refusal != "" || len(script.shown) != 2 {
		t.Errorf("the remembered rule should allow the command without a prompt: err=%v refusal=%q forms=%d", err, refusal, len(script.shown))
	}
}

func TestEndToEndFailedEditRunsAndWarnsOnAnOldClient(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	script := &editScript{answers: []*mcpsdk.ElicitResult{
		accept(map[string]any{"remember": true, "rule": "[echo, bye]"}),
	}}
	cs := connectGate(t, g, protocolLegacy, script.handle)

	out, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi"}})
	if err != nil || refusal != "" {
		t.Fatalf("cg_run: err=%v refusal=%q", err, refusal)
	}
	if code, ok := out["exit_code"].(float64); !ok || code != 0 {
		t.Errorf("exit_code = %v, want 0", out["exit_code"])
	}

	warning, _ := out["remember_warning"].(string)
	for _, want := range []string{"skipping remember", "[echo, bye]", "`echo hi`", "The accepted edits are"} {
		if !strings.Contains(warning, want) {
			t.Errorf("remember_warning lacks %q: %q", want, warning)
		}
	}
	if len(script.shown) != 1 {
		t.Errorf("forms shown = %d, want 1", len(script.shown))
	}

	if file, _ := readProjectIfPresent(root); strings.Contains(file, "echo") {
		t.Errorf("a rule was written:\n%s", file)
	}
}

func TestSecondRoundRefusesACommandRoundOneNeverApproved(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	allowing := newTestGateAt(t, root, "version: 1\n", false)
	strict := newTestGateAt(t, t.TempDir(), "version: 1\n", false)
	strict.pendingOnce.Do(func() { strict.pendingApps = allowing.pending() })

	call := multiRound(testCall(echoHi, []string{"printf", "x"}))
	pending := firstRound(t, allowing, call)

	if err := allowing.store.AppendProjectAllowPrefix([]string{"echo", "hi"}, approve.WriteDirect); err != nil {
		t.Fatalf("adding the allow rule: %v", err)
	}

	second, err := allowing.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": decline(),
		"approval-2": accept(map[string]any{"remember": true, "rule": "[printf, y]"}),
	}))
	if err != nil || second.Pending == nil {
		t.Fatalf("round 1 retry = %+v, %v; want a second round", second, err)
	}

	_, err = strict.check(retryCall(call, second.Pending, mcpsdk.InputResponseMap{
		"approval-2": accept(map[string]any{"remember": false}),
	}))
	if err == nil || !errors.Is(err, ErrPendingApproval) || !strings.Contains(err.Error(), "was not approved in the earlier round") {
		t.Fatalf("err = %v, want a refusal for the command nobody approved", err)
	}
}

func TestEndToEndSecondRoundDecline(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	script := &editScript{answers: []*mcpsdk.ElicitResult{
		accept(map[string]any{"remember": true, "rule": "[echo, bye]"}),
		decline(),
	}}
	cs := connectGate(t, g, protocolMultiRoundTrip, script.handle)

	_, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi"}})
	if err != nil {
		t.Fatalf("cg_run: %v", err)
	}
	if !strings.Contains(refusal, "command was declined at the approval prompt") {
		t.Errorf("refusal = %q, want the decline", refusal)
	}
	if len(script.shown) != 2 {
		t.Errorf("forms shown = %d, want 2", len(script.shown))
	}
	if file, _ := readProjectIfPresent(root); strings.Contains(file, "echo") {
		t.Errorf("a rule was written:\n%s", file)
	}
}

func TestEndToEndUnparseableFieldLeadsToASecondRound(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	script := &editScript{answers: []*mcpsdk.ElicitResult{
		accept(map[string]any{"remember": true, "rule": "not a list"}),
		accept(map[string]any{"remember": true, "rule": "[echo]"}),
	}}
	cs := connectGate(t, g, protocolMultiRoundTrip, script.handle)

	out, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi"}})
	if err != nil || refusal != "" {
		t.Fatalf("cg_run: err=%v refusal=%q", err, refusal)
	}
	if _, has := out["remember_warning"]; has {
		t.Errorf("remember_warning = %v, want none", out["remember_warning"])
	}
	if len(script.schemas) != 2 || !strings.Contains(script.schemas[1], "The field holds the original command") {
		t.Fatalf("round 2 schemas = %v", script.schemas)
	}

	if file, _ := readProjectIfPresent(root); !strings.Contains(file, "prefix: [echo]") {
		t.Errorf("project file lacks the rule:\n%s", file)
	}
}

func TestEndToEndRejectedEditBesideADeclinedFormRefuses(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	var mu sync.Mutex
	var shown int
	handler := func(_ context.Context, req *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
		mu.Lock()
		shown++
		mu.Unlock()

		if strings.Contains(req.Params.Message, "printf") {
			return decline(), nil
		}

		return accept(map[string]any{"remember": true, "rule": "[echo, bye]"}), nil
	}
	cs := connectGate(t, g, protocolMultiRoundTrip, handler)

	_, refusal, err := callGated(t, cs, "cg_run_many", map[string]any{"commands": [][]string{{"echo", "hi"}, {"printf", "x"}}})
	if err != nil {
		t.Fatalf("cg_run_many: %v", err)
	}
	for _, want := range []string{"declined at the approval prompt", "skipping remember:", "does not match the command"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("refusal lacks %q: %q", want, refusal)
		}
	}
	if shown != 2 {
		t.Errorf("forms shown = %d, want 2 and no second round", shown)
	}
	if file, _ := readProjectIfPresent(root); strings.Contains(file, "echo") {
		t.Errorf("a rule was written:\n%s", file)
	}
}

func TestEndToEndSecondRoundShowsTheBoxAsRoundOneLeftIt(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

	script := &editScript{answers: []*mcpsdk.ElicitResult{
		accept(map[string]any{"remember": true, "rule": "[echo, bye]", "allow_extra_args": true}),
		accept(map[string]any{"remember": false}),
	}}
	cs := connectGate(t, g, protocolMultiRoundTrip, script.handle)

	if _, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi"}}); err != nil || refusal != "" {
		t.Fatalf("cg_run: err=%v refusal=%q", err, refusal)
	}
	if len(script.schemas) != 2 {
		t.Fatalf("forms shown = %d, want 2", len(script.schemas))
	}

	if !strings.Contains(script.schemas[0], `"allow_extra_args":{"default":false,`) {
		t.Errorf("round 1 box should default off:\n%s", script.schemas[0])
	}
	if !strings.Contains(script.schemas[1], `"allow_extra_args":{"default":true,`) {
		t.Errorf("round 2 box should keep its round 1 value:\n%s", script.schemas[1])
	}
}
