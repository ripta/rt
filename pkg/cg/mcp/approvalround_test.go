package mcp

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
)

const (
	baseRules = "version: 1\nallow:\n  - prefix: [make]\n"
	editRules = "version: 1\nallow:\n  - prefix: [make]\n  - prefix: [npm, ci]\n"
)

func decline() *mcpsdk.ElicitResult {
	return &mcpsdk.ElicitResult{Action: "decline"}
}

func cancel() *mcpsdk.ElicitResult {
	return &mcpsdk.ElicitResult{Action: "cancel"}
}

func testCall(argvs ...[]string) approvalCall {
	resolutions := make([]*model.Resolution, len(argvs))
	for i, argv := range argvs {
		resolutions[i], _ = model.ResolveCommand(argv, "")
	}

	return approvalCall{Tool: "cg_run", Argvs: argvs, Resolutions: resolutions, CanPrompt: true}
}

func retryCall(call approvalCall, pending *mcpsdk.CallToolResult, answers mcpsdk.InputResponseMap) approvalCall {
	call.State = pending.RequestState
	call.Responses = answers

	return call
}

func writeProject(t *testing.T, root, content string) {
	t.Helper()

	if err := os.WriteFile(projectFile(root), []byte(content), 0o600); err != nil {
		t.Fatalf("writing project file: %v", err)
	}
}

func readProjectIfPresent(root string) (string, error) {
	raw, err := os.ReadFile(projectFile(root))

	return string(raw), err
}

// roundTest drives one two-call exchange against a real gate. EditFirst rewrites the project file
// after load and before the first call. EditSecond rewrites it between the two calls.
// AllowBetween remembers an allow prefix through the store between the two calls, as another call
// in the same session could.
//
// WantErrWarning is a remember warning that a refused retry must carry in its error, since a
// refusal returns no Warning of its own.
type roundTest struct {
	Name         string
	Project      string
	EditFirst    string
	EditSecond   string
	AllowBetween []string
	Argvs        [][]string
	Answers      mcpsdk.InputResponseMap

	WantForms      int
	WantDiverge    bool
	WantErr        string
	WantErrWarning string
	WantWarning    string
	WantFile       []string
	AbsentFile     []string

	// WantNoAdvice asserts the warning does not tell the user to call the tool again, which fits
	// only a rule whose commands still need a prompt.
	WantNoAdvice bool
}

var echoHi = []string{"echo", "hi"}

var roundTests = []roundTest{
	{
		Name:    "accept without remember writes nothing",
		Project: "",
		Argvs:   [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": false})},

		WantForms: 1,
	},
	{
		Name:    "decline refuses",
		Project: "version: 1\n",
		Argvs:   [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{"approval-1": decline()},

		WantForms: 1,
		WantErr:   "declined at the approval prompt",
	},
	{
		Name:    "cancel refuses",
		Project: "version: 1\n",
		Argvs:   [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{"approval-1": cancel()},

		WantForms: 1,
		WantErr:   "declined at the approval prompt",
	},
	{
		Name:    "remember writes the suggested rule",
		Project: "version: 1\n",
		Argvs:   [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"})},

		WantForms: 1,
		WantFile:  []string{"prefix: [echo]"},
	},
	{
		Name:    "remember keeps an edited rule",
		Project: "version: 1\n",
		Argvs:   [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, hi]"})},

		WantForms: 1,
		WantFile:  []string{"prefix: [echo, hi]"},
	},
	{
		Name:    "a bad rule field refuses and writes nothing",
		Project: "version: 1\n",
		Argvs:   [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true, "rule": "not a list"})},

		WantForms:  1,
		WantErr:    "rule must be a YAML list",
		AbsentFile: []string{"prefix:"},
	},
	{
		Name:    "a missing answer refuses and says to call again",
		Project: "version: 1\n",
		Argvs:   [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{},

		WantForms: 1,
		WantErr:   "call the tool again",
	},
	{
		Name:         "an allow rule added between the calls runs whatever the answer says",
		Project:      "version: 1\n",
		AllowBetween: []string{"echo"},
		Argvs:        [][]string{echoHi},
		Answers:      mcpsdk.InputResponseMap{"approval-1": decline()},

		WantForms: 1,
	},
	{
		Name:      "divergence reload and merge keeps the disk change",
		Project:   baseRules,
		EditFirst: editRules,
		Argvs:     [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"divergence": accept(map[string]any{"choice": divergeReloadMerge}),
		},

		WantForms:   1,
		WantDiverge: true,
		WantFile:    []string{"prefix: [make]", "prefix: [npm, ci]", "prefix: [echo]"},
	},
	{
		Name:      "divergence overwrite drops the disk change",
		Project:   baseRules,
		EditFirst: editRules,
		Argvs:     [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"divergence": accept(map[string]any{"choice": divergeOverwrite}),
		},

		WantForms:   1,
		WantDiverge: true,
		WantFile:    []string{"prefix: [make]", "prefix: [echo]"},
		AbsentFile:  []string{"npm"},
	},
	{
		Name:      "divergence skip leaves the file alone without a warning",
		Project:   baseRules,
		EditFirst: editRules,
		Argvs:     [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"divergence": accept(map[string]any{"choice": divergeSkip}),
		},

		WantForms:   1,
		WantDiverge: true,
		WantFile:    []string{"prefix: [npm, ci]"},
		AbsentFile:  []string{"prefix: [echo]"},
	},
	{
		Name:      "a declined divergence form skips the remember",
		Project:   baseRules,
		EditFirst: editRules,
		Argvs:     [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"divergence": decline(),
		},

		WantForms:   1,
		WantDiverge: true,
		AbsentFile:  []string{"prefix: [echo]"},
	},
	{
		Name:      "a missing divergence answer skips the remember with a warning",
		Project:   baseRules,
		EditFirst: editRules,
		Argvs:     [][]string{echoHi},
		Answers:   mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"})},

		WantForms:   1,
		WantDiverge: true,
		WantWarning: "did not answer the divergence prompt",
		AbsentFile:  []string{"prefix: [echo]"},
	},
	{
		Name:      "the divergence choice is ignored when nothing is remembered",
		Project:   baseRules,
		EditFirst: editRules,
		Argvs:     [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": false}),
			"divergence": accept(map[string]any{"choice": divergeOverwrite}),
		},

		WantForms:   1,
		WantDiverge: true,
		WantFile:    []string{"prefix: [npm, ci]"},
		AbsentFile:  []string{"prefix: [echo]"},
	},
	{
		Name:       "a change after an undiverged first call skips the remember",
		Project:    baseRules,
		EditSecond: editRules,
		Argvs:      [][]string{echoHi},
		Answers:    mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"})},

		WantForms:   1,
		WantWarning: "changed on disk since the approval was requested",
		WantFile:    []string{"prefix: [npm, ci]"},
		AbsentFile:  []string{"prefix: [echo]"},
	},
	{
		Name:       "a change after a diverged first call skips the remember",
		Project:    baseRules,
		EditFirst:  editRules,
		EditSecond: editRules + "  - prefix: [yarn]\n",
		Argvs:      [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"divergence": accept(map[string]any{"choice": divergeOverwrite}),
		},

		WantForms:   1,
		WantDiverge: true,
		WantWarning: "changed on disk since the approval was requested",
		WantFile:    []string{"prefix: [yarn]"},
		AbsentFile:  []string{"prefix: [echo]"},
	},
	{
		Name:    "two remembered rules land in one round",
		Project: "version: 1\n",
		Argvs:   [][]string{echoHi, {"printf", "x"}},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"approval-2": accept(map[string]any{"remember": true, "rule": "[printf]"}),
		},

		WantForms: 2,
		WantFile:  []string{"prefix: [echo]", "prefix: [printf]"},
	},
	{
		Name:    "a decline still persists another form's remember",
		Project: "version: 1\n",
		Argvs:   [][]string{echoHi, {"printf", "x"}},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"approval-2": decline(),
		},

		WantForms: 2,
		WantErr:   "declined at the approval prompt",
		WantFile:  []string{"prefix: [echo]"},
	},
	{
		Name:    "a declined group names every command it covers in call order",
		Project: "version: 1\n",
		Argvs:   [][]string{{"echo", "a"}, {"printf", "x"}, {"echo", "b"}},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": decline(),
			"approval-2": accept(map[string]any{}),
		},

		WantForms: 2,
		WantErr:   "2 commands were declined at the approval prompt\n  echo a\n  echo b",
	},
	{
		Name:         "a command allowed between the calls runs while the rest of its form uses the answer",
		Project:      "version: 1\n",
		AllowBetween: []string{"echo", "a"},
		Argvs:        [][]string{{"echo", "a"}, {"echo", "b"}},
		Answers:      mcpsdk.InputResponseMap{"approval-1": decline()},

		WantForms: 1,
		WantErr:   "command was declined at the approval prompt\n  echo b",
	},
	{
		Name:         "a form whose commands are all allowed saves a widened rule",
		Project:      "version: 1\n",
		AllowBetween: []string{"echo", "hi"},
		Argvs:        [][]string{echoHi},
		Answers:      mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"})},

		WantForms: 1,
		WantFile:  []string{"prefix: [echo, hi]", "prefix: [echo]"},
	},
	{
		Name:         "a declined form whose commands are all allowed refuses nothing and saves nothing",
		Project:      "version: 1\n",
		AllowBetween: []string{"echo", "hi"},
		Argvs:        [][]string{echoHi},
		Answers:      mcpsdk.InputResponseMap{"approval-1": decline()},

		WantForms:  1,
		AbsentFile: []string{"prefix: [echo]\n"},
	},
	{
		Name:         "a missing answer on a form whose commands are all allowed refuses nothing",
		Project:      "version: 1\n",
		AllowBetween: []string{"echo", "hi"},
		Argvs:        [][]string{echoHi},
		Answers:      mcpsdk.InputResponseMap{},

		WantForms: 1,
	},
	{
		Name:         "a malformed rule on a form whose commands are all allowed warns",
		Project:      "version: 1\n",
		AllowBetween: []string{"echo", "hi"},
		Argvs:        [][]string{echoHi},
		Answers:      mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true, "rule": "not a list"})},

		WantForms:   1,
		WantWarning: "rule must be a YAML list",
	},
	{
		Name:         "a rule cg wrote between the calls does not skip the remember of an undiverged entry",
		Project:      "version: 1\n",
		AllowBetween: []string{"printf"},
		Argvs:        [][]string{echoHi},
		Answers:      mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"})},

		WantForms: 1,
		WantFile:  []string{"prefix: [printf]", "prefix: [echo]"},
	},
	{
		Name:         "a diverged entry skips after a cg write",
		Project:      baseRules,
		EditFirst:    editRules,
		AllowBetween: []string{"printf"},
		Argvs:        [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"divergence": accept(map[string]any{"choice": divergeOverwrite}),
		},

		WantForms:   1,
		WantDiverge: true,
		WantWarning: "changed on disk since the approval was requested; " + pendingAdvice,
		AbsentFile:  []string{"prefix: [echo]"},
	},
	{
		Name:         "a skipped remember on a form whose commands ran does not say to call again",
		Project:      baseRules,
		EditFirst:    editRules,
		AllowBetween: []string{"echo", "hi"},
		Argvs:        [][]string{echoHi},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"divergence": accept(map[string]any{"choice": divergeOverwrite}),
		},

		WantForms:    1,
		WantDiverge:  true,
		WantWarning:  "rule [echo] was not saved because ",
		WantNoAdvice: true,
		AbsentFile:   []string{"prefix: [echo]\n"},
	},
	{
		Name:    "a bad rule field still persists another form's remember",
		Project: "version: 1\n",
		Argvs:   [][]string{echoHi, {"printf", "x"}},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"approval-2": accept(map[string]any{"remember": true, "rule": "not a list"}),
		},

		WantForms:  2,
		WantErr:    "rule must be a YAML list",
		WantFile:   []string{"prefix: [echo]"},
		AbsentFile: []string{"printf"},
	},
	{
		Name:    "a missing answer still persists another form's remember",
		Project: "version: 1\n",
		Argvs:   [][]string{echoHi, {"printf", "x"}},
		Answers: mcpsdk.InputResponseMap{
			"approval-2": accept(map[string]any{"remember": true, "rule": "[printf]"}),
		},

		WantForms: 2,
		WantErr:   "call the tool again",
		WantFile:  []string{"prefix: [printf]"},
	},
	{
		Name:       "a bad rule field still reports a skipped remember",
		Project:    baseRules,
		EditSecond: editRules,
		Argvs:      [][]string{echoHi, {"printf", "x"}},
		Answers: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"approval-2": accept(map[string]any{"remember": true, "rule": "not a list"}),
		},

		WantForms:      2,
		WantErr:        "rule must be a YAML list",
		WantErrWarning: "skipping remember: ",
		WantFile:       []string{"prefix: [npm, ci]"},
		AbsentFile:     []string{"prefix: [echo]"},
	},
	{
		Name:       "a missing answer still reports a skipped remember",
		Project:    baseRules,
		EditSecond: editRules,
		Argvs:      [][]string{echoHi, {"printf", "x"}},
		Answers: mcpsdk.InputResponseMap{
			"approval-2": accept(map[string]any{"remember": true, "rule": "[printf]"}),
		},

		WantForms:      2,
		WantErr:        `no answer for input request "approval-1"`,
		WantErrWarning: "skipping remember: ",
		WantFile:       []string{"prefix: [npm, ci]"},
		AbsentFile:     []string{"prefix: [printf]"},
	},
}

func TestApprovalRound(t *testing.T) {
	for _, test := range roundTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			root := t.TempDir()
			g := newTestGateAt(t, root, test.Project, false)
			if test.EditFirst != "" {
				writeProject(t, root, test.EditFirst)
			}

			call := testCall(test.Argvs...)
			first, err := g.check(call)
			if err != nil {
				t.Fatalf("first call: %v", err)
			}
			if first.Pending == nil {
				t.Fatalf("first call returned no input requests")
			}
			if first.Pending.RequestState == "" {
				t.Errorf("RequestState is empty")
			}
			if len(first.Pending.Content) != 0 {
				t.Errorf("an input-required result carries content: %v", first.Pending.Content)
			}

			_, hasDiverge := first.Pending.InputRequests[divergenceID]
			if hasDiverge != test.WantDiverge {
				t.Errorf("divergence form present = %v, want %v", hasDiverge, test.WantDiverge)
			}
			if got := len(first.Pending.InputRequests) - btoi(hasDiverge); got != test.WantForms {
				t.Errorf("approval forms = %d, want %d", got, test.WantForms)
			}

			if test.AllowBetween != nil {
				strategy := approve.WriteDirect
				if test.EditFirst != "" {
					strategy = approve.WriteOverwrite
				}
				if err := g.store.AppendProjectAllowPrefix(test.AllowBetween, strategy); err != nil {
					t.Fatalf("adding the allow rule: %v", err)
				}
			}
			if test.EditSecond != "" {
				writeProject(t, root, test.EditSecond)
			}

			second, err := g.check(retryCall(call, first.Pending, test.Answers))
			if test.WantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.WantErr) {
					t.Fatalf("retry error = %v, want it to contain %q", err, test.WantErr)
				}
				if !strings.Contains(err.Error(), test.WantErrWarning) {
					t.Errorf("retry error = %q, want it to carry the remember warning %q", err, test.WantErrWarning)
				}
			} else if err != nil {
				t.Fatalf("retry: %v", err)
			}
			if second.Pending != nil {
				t.Errorf("the retry returned input requests again")
			}
			if test.WantWarning == "" && second.Warning != "" {
				t.Errorf("Warning = %q, want none", second.Warning)
			}
			if !strings.Contains(second.Warning, test.WantWarning) {
				t.Errorf("Warning = %q, want it to contain %q", second.Warning, test.WantWarning)
			}
			if test.WantNoAdvice && strings.Contains(second.Warning, pendingAdvice) {
				t.Errorf("Warning = %q, want no advice to call the tool again", second.Warning)
			}

			file, _ := readProjectIfPresent(root)
			for _, want := range test.WantFile {
				if !strings.Contains(file, want) {
					t.Errorf("project file missing %q:\n%s", want, file)
				}
			}
			for _, bad := range test.AbsentFile {
				if strings.Contains(file, bad) {
					t.Errorf("project file should not contain %q:\n%s", bad, file)
				}
			}
		})
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}

	return 0
}

func TestApprovalRoundRememberedRuleGoesLive(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	call := testCall(echoHi)

	first, err := g.check(call)
	if err != nil || first.Pending == nil {
		t.Fatalf("first call = %+v, %v; want input requests", first, err)
	}

	_, err = g.check(retryCall(call, first.Pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
	}))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}

	again, err := g.check(testCall([]string{"echo", "again"}))
	if err != nil {
		t.Fatalf("second command: %v", err)
	}
	if again.Pending != nil {
		t.Errorf("the second command prompted, want the remembered rule to allow it")
	}
}

func TestApprovalRoundNonceIsSingleUse(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	call := testCall(echoHi)

	first, err := g.check(call)
	if err != nil || first.Pending == nil {
		t.Fatalf("first call = %+v, %v; want input requests", first, err)
	}

	retry := retryCall(call, first.Pending, mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{})})
	if _, err := g.check(retry); err != nil {
		t.Fatalf("first retry: %v", err)
	}

	_, err = g.check(retry)
	if !errors.Is(err, ErrPendingApproval) {
		t.Fatalf("replayed retry error = %v, want ErrPendingApproval", err)
	}
	if !strings.Contains(err.Error(), "call the tool again") {
		t.Errorf("err = %v, want it to say to call again", err)
	}
}

func TestApprovalRoundRefusals(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	approval := mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{})}

	t.Run("an unknown nonce", func(t *testing.T) {
		g := newTestGate(t, "version: 1\n", false)
		call := testCall(echoHi)
		call.State = "never-issued"

		if _, err := g.check(call); !errors.Is(err, ErrPendingApproval) {
			t.Fatalf("err = %v, want ErrPendingApproval", err)
		}
	})

	t.Run("a different command", func(t *testing.T) {
		g := newTestGate(t, "version: 1\n", false)
		first, err := g.check(testCall(echoHi))
		if err != nil || first.Pending == nil {
			t.Fatalf("first call = %+v, %v; want input requests", first, err)
		}

		retry := retryCall(testCall([]string{"echo", "other"}), first.Pending, approval)
		if _, err := g.check(retry); !errors.Is(err, ErrPendingApproval) {
			t.Fatalf("err = %v, want ErrPendingApproval", err)
		}
	})

	t.Run("a command that now resolves elsewhere", func(t *testing.T) {
		g := newTestGate(t, "version: 1\n", false)
		call := testCall(echoHi)
		first, err := g.check(call)
		if err != nil || first.Pending == nil {
			t.Fatalf("first call = %+v, %v; want input requests", first, err)
		}

		moved := retryCall(call, first.Pending, approval)
		moved.Resolutions = []*model.Resolution{{Argv: echoHi, Resolved: "/elsewhere/echo", Canonical: "/elsewhere/echo"}}
		if _, err := g.check(moved); !errors.Is(err, ErrPendingApproval) {
			t.Fatalf("err = %v, want ErrPendingApproval", err)
		}
	})

	t.Run("a client that cannot prompt", func(t *testing.T) {
		g := newTestGate(t, "version: 1\n", false)
		call := testCall(echoHi)
		call.CanPrompt = false

		_, err := g.check(call)
		if err == nil || !strings.Contains(err.Error(), "client cannot prompt") {
			t.Fatalf("err = %v, want the fail-closed refusal", err)
		}
	})
}

// TestApprovalRoundRuleChanges moves a pending approval onto a gate whose rules differ from the one
// that asked, which is how a rules change between the two calls looks to the retry.
func TestApprovalRoundRuleChanges(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	answers := mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{})}

	t.Run("deny wins over an accepted form", func(t *testing.T) {
		asking := newTestGate(t, "version: 1\n", false)
		deciding := newTestGate(t, "version: 1\ndeny:\n  - prefix: [echo]\n    message: no echoes\n", false)
		deciding.pendingOnce.Do(func() { deciding.pendingApps = asking.pending() })

		call := testCall(echoHi)
		first, err := asking.check(call)
		if err != nil || first.Pending == nil {
			t.Fatalf("first call = %+v, %v; want input requests", first, err)
		}

		_, err = deciding.check(retryCall(call, first.Pending, answers))
		if err == nil || !strings.Contains(err.Error(), "matches a deny rule: no echoes") {
			t.Fatalf("err = %v, want the deny refusal", err)
		}
	})

	t.Run("a command that was not asked about is refused", func(t *testing.T) {
		asking := newTestGate(t, "version: 1\nallow:\n  - prefix: [echo]\n", false)
		deciding := newTestGate(t, "version: 1\n", false)
		deciding.pendingOnce.Do(func() { deciding.pendingApps = asking.pending() })

		call := testCall(echoHi, []string{"printf", "x"})
		first, err := asking.check(call)
		if err != nil || first.Pending == nil {
			t.Fatalf("first call = %+v, %v; want input requests", first, err)
		}
		if got := len(first.Pending.InputRequests); got != 1 {
			t.Fatalf("forms = %d, want 1 for the one unapproved command", got)
		}

		_, err = deciding.check(retryCall(call, first.Pending, mcpsdk.InputResponseMap{
			"approval-2": accept(map[string]any{}),
		}))
		if !errors.Is(err, ErrPendingApproval) {
			t.Fatalf("err = %v, want ErrPendingApproval", err)
		}
	})
}

func TestApprovalRoundFormOrder(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, baseRules, false)
	writeProject(t, root, editRules)

	argvs := make([][]string, 12)
	for i := range argvs {
		argvs[i] = []string{"go", string(rune('a' + i))}
	}

	first, err := g.check(testCall(argvs...))
	if err != nil || first.Pending == nil {
		t.Fatalf("first call = %+v, %v; want input requests", first, err)
	}

	ids := make([]string, 0, len(first.Pending.InputRequests))
	for id := range first.Pending.InputRequests {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	if len(ids) != 13 {
		t.Fatalf("forms = %d, want 12 approvals and 1 divergence", len(ids))
	}
	if ids[len(ids)-1] != divergenceID {
		t.Errorf("last form = %q, want the divergence form", ids[len(ids)-1])
	}
	for i, want := range []string{"approval-01", "approval-02", "approval-03"} {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
	if ids[11] != "approval-12" {
		t.Errorf("ids[11] = %q, want approval-12 so that sorted order is command order", ids[11])
	}
}

func echoArgvs(n int) [][]string {
	argvs := make([][]string, n)
	for i := range argvs {
		argvs[i] = []string{"echo", fmt.Sprintf("c%02d", i)}
	}

	return argvs
}

func argvLines(argvs [][]string) []string {
	lines := make([]string, len(argvs))
	for i, argv := range argvs {
		lines[i] = model.EscapeArgs(argv)
	}

	return lines
}

// listedCommands reads the commands an approval form's message lists, which are every line after
// the header.
func listedCommands(message string) []string {
	_, rest, found := strings.Cut(message, "\n")
	if !found {
		return nil
	}

	return strings.Split(rest, "\n")
}

type groupTest struct {
	Name  string
	Argvs [][]string

	// WantForms lists the commands of each form in form order.
	WantForms [][]string
}

var groupTests = []groupTest{
	{
		Name:      "five commands under one prefix split into forms of at most the limit",
		Argvs:     echoArgvs(5),
		WantForms: slices.Collect(slices.Chunk(argvLines(echoArgvs(5)), maxFormCommands)),
	},
	{
		Name:      "commands up to the limit under one prefix share one form",
		Argvs:     echoArgvs(maxFormCommands),
		WantForms: [][]string{argvLines(echoArgvs(maxFormCommands))},
	},
	{
		Name:  "two prefixes give two forms in first-appearance order",
		Argvs: [][]string{{"printf", "x"}, {"echo", "a"}, {"printf", "y"}, {"echo", "b"}},
		WantForms: [][]string{
			{"printf x", "printf y"},
			{"echo a", "echo b"},
		},
	},
	{
		Name:  "one command more than the limit gives a full form and a form of one",
		Argvs: echoArgvs(maxFormCommands + 1),
		WantForms: [][]string{
			argvLines(echoArgvs(maxFormCommands)),
			argvLines(echoArgvs(maxFormCommands + 1)[maxFormCommands:]),
		},
	},
}

func TestApprovalRoundGroupsCommandsByRule(t *testing.T) {
	for _, test := range groupTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			g := newTestGate(t, "version: 1\n", false)
			first, err := g.check(testCall(test.Argvs...))
			if err != nil || first.Pending == nil {
				t.Fatalf("first call = %+v, %v; want input requests", first, err)
			}

			ids := make([]string, 0, len(first.Pending.InputRequests))
			for id := range first.Pending.InputRequests {
				ids = append(ids, id)
			}
			slices.Sort(ids)

			if len(ids) != len(test.WantForms) {
				t.Fatalf("forms = %v, want %d", ids, len(test.WantForms))
			}

			for i, id := range ids {
				form, ok := first.Pending.InputRequests[id].(*mcpsdk.ElicitParams)
				if !ok {
					t.Fatalf("form %s is %T, want *ElicitParams", id, first.Pending.InputRequests[id])
				}

				if got := listedCommands(form.Message); !slices.Equal(got, test.WantForms[i]) {
					t.Errorf("form %s lists %q, want %q", id, got, test.WantForms[i])
				}
			}
		})
	}
}

func TestApprovalRoundRememberOnChunksWritesOneRule(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	call := testCall(echoArgvs(maxFormCommands + 1)...)
	first, err := g.check(call)
	if err != nil || first.Pending == nil {
		t.Fatalf("first call = %+v, %v; want input requests", first, err)
	}
	if got := len(first.Pending.InputRequests); got != 2 {
		t.Fatalf("forms = %d, want 2", got)
	}

	remembered := accept(map[string]any{"remember": true, "rule": "[echo]"})
	_, err = g.check(retryCall(call, first.Pending, mcpsdk.InputResponseMap{"approval-1": remembered, "approval-2": remembered}))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}

	file, _ := readProjectIfPresent(root)
	if got := strings.Count(file, "prefix: [echo]"); got != 1 {
		t.Errorf("rule written %d times, want once:\n%s", got, file)
	}
}

func TestApprovalRoundFormsRecordedPerCommand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	call := testCall([]string{"echo", "a"}, []string{"printf", "x"}, []string{"echo", "b"})
	first, err := g.check(call)
	if err != nil || first.Pending == nil {
		t.Fatalf("first call = %+v, %v; want input requests", first, err)
	}

	entry, err := g.pending().take(first.Pending.RequestState, call.Tool, call.Argvs, call.Cwd, call.Env, call.Args)
	if err != nil {
		t.Fatalf("take: %v", err)
	}

	got := []string{entry.Commands[0].Form, entry.Commands[1].Form, entry.Commands[2].Form}
	if want := []string{"approval-1", "approval-2", "approval-1"}; !slices.Equal(got, want) {
		t.Errorf("forms = %q, want %q", got, want)
	}
}

func TestDivergenceFormDefaultsToSkip(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, baseRules, false)
	writeProject(t, root, editRules)

	first, err := g.check(testCall(echoHi))
	if err != nil || first.Pending == nil {
		t.Fatalf("first call = %+v, %v; want input requests", first, err)
	}

	form, ok := first.Pending.InputRequests[divergenceID].(*mcpsdk.ElicitParams)
	if !ok {
		t.Fatalf("divergence form is %T, want *ElicitParams", first.Pending.InputRequests[divergenceID])
	}
	if !strings.Contains(form.Message, "+  - prefix: [npm, ci]") {
		t.Errorf("divergence message lacks the diff:\n%s", form.Message)
	}

	schema, ok := form.RequestedSchema.(map[string]any)
	if !ok {
		t.Fatalf("schema is %T, want a map", form.RequestedSchema)
	}
	choiceField := schema["properties"].(map[string]any)["choice"].(map[string]any)
	if got := choiceField["default"]; got != divergeSkip {
		t.Errorf("choice default = %v, want %q", got, divergeSkip)
	}
}

func TestApprovalRoundUnreadableFirstReadSkipsRemember(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "", false)
	if err := os.Mkdir(projectFile(root), 0o755); err != nil {
		t.Fatalf("seeding project path as dir: %v", err)
	}

	call := testCall(echoHi)
	first, err := g.check(call)
	if err != nil || first.Pending == nil {
		t.Fatalf("first call = %+v, %v; want input requests", first, err)
	}

	if err := os.Remove(projectFile(root)); err != nil {
		t.Fatalf("removing dir: %v", err)
	}
	writeProject(t, root, editRules)

	second, err := g.check(retryCall(call, first.Pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
	}))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !strings.Contains(second.Warning, "could not be read when the approval was requested") {
		t.Errorf("Warning = %q, want the unreadable-at-first-call message", second.Warning)
	}

	file, _ := readProjectIfPresent(root)
	if strings.Contains(file, "prefix: [echo]") {
		t.Errorf("remember was written:\n%s", file)
	}
}

func TestApprovalRoundStaleRefusalReadsReasonFirst(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	call := testCall(echoHi)
	first, err := g.check(call)
	if err != nil || first.Pending == nil {
		t.Fatalf("first call = %+v, %v; want input requests", first, err)
	}

	moved := retryCall(call, first.Pending, mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{})})
	moved.Resolutions = []*model.Resolution{{Argv: echoHi, Resolved: "/elsewhere/echo", Canonical: "/elsewhere/echo"}}

	_, err = g.check(moved)
	want := "cg_run refused: the command now resolves to a different executable than the one that was shown; call the tool again to be prompted afresh"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if !errors.Is(err, ErrPendingApproval) {
		t.Errorf("err does not match ErrPendingApproval")
	}
}

func TestApprovalRoundStaleResolutionRefusesWholeAndPersistsNothing(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	call := testCall(echoHi, []string{"printf", "x"}, []string{"printf", "y"})
	first, err := g.check(call)
	if err != nil || first.Pending == nil {
		t.Fatalf("first call = %+v, %v; want input requests", first, err)
	}

	moved := retryCall(call, first.Pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
		"approval-2": accept(map[string]any{"remember": true, "rule": "[printf, x]"}),
	})
	moved.Resolutions = slices.Clone(call.Resolutions)
	moved.Resolutions[2] = &model.Resolution{Argv: call.Argvs[2], Resolved: "/elsewhere/printf", Canonical: "/elsewhere/printf"}

	_, err = g.check(moved)
	if err == nil || !strings.Contains(err.Error(), "now resolves to a different executable") {
		t.Fatalf("err = %v, want a stale resolution refusal", err)
	}
	if !errors.Is(err, ErrPendingApproval) {
		t.Errorf("err does not match ErrPendingApproval")
	}

	file, _ := readProjectIfPresent(root)
	if strings.Contains(file, "prefix:") {
		t.Errorf("remember was written despite the stale refusal:\n%s", file)
	}
}

func TestRememberFailureProjectChanged(t *testing.T) {
	retryable := rememberRule{Tokens: []string{"echo"}, Retryable: true}
	got := rememberFailure("/p/.cg.yaml", retryable, fmt.Errorf("appending: %w", approve.ErrProjectChanged))
	want := "skipping remember: /p/.cg.yaml changed on disk since the approval was requested; " + pendingAdvice
	if got != want {
		t.Errorf("rememberFailure = %q, want %q", got, want)
	}

	stuck := rememberRule{Tokens: []string{"echo", "hi"}}
	got = rememberFailure("/p/.cg.yaml", stuck, fmt.Errorf("appending: %w", approve.ErrProjectChanged))
	want = "skipping remember: rule [echo, hi] was not saved because /p/.cg.yaml changed on disk since the approval was requested"
	if got != want {
		t.Errorf("rememberFailure = %q, want %q", got, want)
	}

	if got := rememberFailure("/p", retryable, errors.New("boom")); got != "remember write failed: boom" {
		t.Errorf("rememberFailure = %q", got)
	}
}
