package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// commandScript answers each approval form by the commands it lists. The first listed command with
// an entry decides the answer, and a form with none is declined. OnShow runs for every form before
// it is answered.
type commandScript struct {
	Answers map[string]*mcpsdk.ElicitResult
	OnShow  func(message string)

	mu    sync.Mutex
	shown []string
}

func (c *commandScript) handle(_ context.Context, req *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
	c.mu.Lock()
	c.shown = append(c.shown, req.Params.Message)
	c.mu.Unlock()

	if c.OnShow != nil {
		c.OnShow(req.Params.Message)
	}

	for _, command := range listedCommands(req.Params.Message) {
		if res, ok := c.Answers[command]; ok {
			return res, nil
		}
	}

	return decline(), nil
}

func (c *commandScript) messages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]string(nil), c.shown...)
}

func echoCommands(names ...string) [][]string {
	commands := make([][]string, len(names))
	for i, name := range names {
		commands[i] = []string{"echo", name}
	}

	return commands
}

// mixedCommands is three commands that suggest three different rules, so each gets its own form.
func mixedCommands() [][]string {
	return [][]string{{"echo", "a"}, {"printf", "b"}, {"true", "c"}}
}

func TestRunManyBatchesOneHundredCommandsInOneRound(t *testing.T) {
	for _, version := range approvalProtocols {
		t.Run(version, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			names := make([]string, maxPoolCommands)
			answers := make(map[string]*mcpsdk.ElicitResult, maxPoolCommands)
			for i := range names {
				names[i] = fmt.Sprintf("c%03d", i)
				answers["echo "+names[i]] = accept(map[string]any{"remember": false})
			}

			g := newTestGate(t, "version: 1\n", false)
			script := &commandScript{Answers: answers}
			cs := connectGate(t, g, version, script.handle)

			out, refusal, err := callGated(t, cs, "cg_run_many", map[string]any{
				"commands":    echoCommands(names...),
				"parallelism": maxPoolParallelism,
			})
			if err != nil || refusal != "" {
				t.Fatalf("cg_run_many: err=%v refusal=%q", err, refusal)
			}

			if got, ok := out["succeeded"].(float64); !ok || got != maxPoolCommands {
				t.Errorf("succeeded = %v, want %d", out["succeeded"], maxPoolCommands)
			}

			wantForms := (maxPoolCommands + maxFormCommands - 1) / maxFormCommands
			messages := script.messages()
			if len(messages) != wantForms {
				t.Fatalf("forms shown = %d, want %d in a single round", len(messages), wantForms)
			}

			listed := 0
			positions := make(map[string]bool, len(messages))
			for _, m := range messages {
				positions[strings.SplitN(m, "\n", 2)[0]] = true

				n := len(listedCommands(m))
				if n > maxFormCommands {
					t.Errorf("a form lists %d commands, want at most %d", n, maxFormCommands)
				}
				listed += n
			}
			if listed != maxPoolCommands {
				t.Errorf("forms list %d commands, want %d", listed, maxPoolCommands)
			}
			for i := 1; i <= wantForms; i++ {
				if want := fmt.Sprintf("Approval %d of %d", i, wantForms); !positions[want] {
					t.Errorf("no form started with %q", want)
				}
			}
		})
	}
}

type positionTest struct {
	Name     string
	Tool     string
	Args     map[string]any
	Project  string
	Edit     string
	Approved []string

	WantPositions []string
	WantNone      []string
}

var positionTests = []positionTest{
	{
		Name:          "run_many with several forms",
		Tool:          "cg_run_many",
		Args:          map[string]any{"commands": mixedCommands()},
		Project:       "version: 1\n",
		Approved:      []string{"echo a", "printf b", "true c"},
		WantPositions: []string{"Approval 1 of 3", "Approval 2 of 3", "Approval 3 of 3"},
	},
	{
		Name:          "positions count only the forms",
		Tool:          "cg_run_many",
		Args:          map[string]any{"commands": mixedCommands()},
		Project:       "version: 1\nallow:\n  - prefix: [printf, b]\n",
		Approved:      []string{"echo a", "true c"},
		WantPositions: []string{"Approval 1 of 2", "Approval 2 of 2"},
	},
	{
		Name:     "run_many with one form carries no position",
		Tool:     "cg_run_many",
		Args:     map[string]any{"commands": echoCommands("a", "a")},
		Project:  "version: 1\n",
		Approved: []string{"echo a"},
		WantNone: []string{"Approval "},
	},
	{
		Name:     "run with one form carries no position",
		Tool:     "cg_run",
		Args:     map[string]any{"command": []string{"echo", "a"}},
		Project:  "version: 1\n",
		Approved: []string{"echo a"},
		WantNone: []string{"Approval "},
	},
	{
		Name:     "the divergence form is not counted and carries no position",
		Tool:     "cg_run_many",
		Args:     map[string]any{"commands": echoCommands("a")},
		Project:  baseRules,
		Edit:     editRules,
		Approved: []string{"echo a"},
		WantNone: []string{"Approval "},
	},
	{
		Name:          "the divergence form does not raise the count of a multi-form call",
		Tool:          "cg_run_many",
		Args:          map[string]any{"commands": [][]string{{"echo", "a"}, {"printf", "b"}}},
		Project:       baseRules,
		Edit:          editRules,
		Approved:      []string{"echo a", "printf b"},
		WantPositions: []string{"Approval 1 of 2", "Approval 2 of 2"},
	},
}

func TestApprovalPositionText(t *testing.T) {
	for _, version := range approvalProtocols {
		for _, test := range positionTests {
			t.Run(version+"/"+test.Name, func(t *testing.T) {
				t.Setenv("TMPDIR", t.TempDir())

				root := t.TempDir()
				g := newTestGateAt(t, root, test.Project, false)
				if test.Edit != "" {
					writeProject(t, root, test.Edit)
				}

				script := &commandScript{Answers: map[string]*mcpsdk.ElicitResult{}}
				for _, command := range test.Approved {
					script.Answers[command] = accept(map[string]any{"remember": false})
				}

				cs := connectGate(t, g, version, script.handle)

				_, refusal, err := callGated(t, cs, test.Tool, test.Args)
				if err != nil || refusal != "" {
					t.Fatalf("%s: err=%v refusal=%q", test.Tool, err, refusal)
				}

				messages := script.messages()
				for _, want := range test.WantPositions {
					if !slicesAnyPrefix(messages, want+"\n\nAllow this command?") {
						t.Errorf("no form started with %q in %q", want, messages)
					}
				}
				for _, bad := range test.WantNone {
					for _, m := range messages {
						if strings.HasPrefix(m, bad) {
							t.Errorf("form %q should not start with %q", m, bad)
						}
					}
				}
			})
		}
	}
}

func slicesAnyPrefix(items []string, prefix string) bool {
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}

	return false
}

type poolDeclineTest struct {
	Name    string
	Answers map[string]*mcpsdk.ElicitResult

	WantRefusal string
	WantFile    []string
}

var poolDeclineTests = []poolDeclineTest{
	{
		Name: "one decline refuses the pool with the single wording",
		Answers: map[string]*mcpsdk.ElicitResult{
			"echo a":   accept(map[string]any{"remember": false}),
			"printf b": decline(),
			"true c":   accept(map[string]any{"remember": false}),
		},
		WantRefusal: "cg_run_many refused: command was declined at the approval prompt\n  printf b",
	},
	{
		Name: "several declines name every command in command order",
		Answers: map[string]*mcpsdk.ElicitResult{
			"echo a":   decline(),
			"printf b": accept(map[string]any{"remember": false}),
			"true c":   cancel(),
		},
		WantRefusal: "cg_run_many refused: 2 commands were declined at the approval prompt\n  echo a\n  true c",
	},
	{
		Name: "an accepted remember is persisted despite a decline",
		Answers: map[string]*mcpsdk.ElicitResult{
			"echo a":   accept(map[string]any{"remember": true, "rule": "[echo, a]"}),
			"printf b": decline(),
			"true c":   accept(map[string]any{"remember": false}),
		},
		WantRefusal: "cg_run_many refused: command was declined at the approval prompt\n  printf b",
		WantFile:    []string{"prefix: [echo, a]"},
	},
}

func TestRunManyDeclineRefusesPool(t *testing.T) {
	for _, version := range approvalProtocols {
		for _, test := range poolDeclineTests {
			t.Run(version+"/"+test.Name, func(t *testing.T) {
				t.Setenv("TMPDIR", t.TempDir())

				root := t.TempDir()
				g := newTestGateAt(t, root, "version: 1\n", false)
				script := &commandScript{Answers: test.Answers}
				cs := connectGate(t, g, version, script.handle)

				out, refusal, err := callGated(t, cs, "cg_run_many", map[string]any{"commands": mixedCommands()})
				if err != nil {
					t.Fatalf("CallTool: %v", err)
				}
				if out != nil {
					t.Fatalf("pool ran: %v", out)
				}
				if refusal != test.WantRefusal {
					t.Errorf("refusal = %q, want %q", refusal, test.WantRefusal)
				}

				if got := len(script.messages()); got != 3 {
					t.Errorf("forms shown = %d, want 3", got)
				}

				assertCaptureRootEmpty(t)

				file, _ := readProjectIfPresent(root)
				for _, want := range test.WantFile {
					if !strings.Contains(file, want) {
						t.Errorf("project file missing %q:\n%s", want, file)
					}
				}
			})
		}
	}
}

func TestRunManyDeclineCarriesRememberWarning(t *testing.T) {
	for _, version := range approvalProtocols {
		t.Run(version, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			root := t.TempDir()
			g := newTestGateAt(t, root, "version: 1\n", false)

			script := &commandScript{
				Answers: map[string]*mcpsdk.ElicitResult{
					"echo a":   accept(map[string]any{"remember": true, "rule": "[echo, a]"}),
					"printf b": decline(),
				},
			}

			var once sync.Once
			script.OnShow = func(string) {
				once.Do(func() { writeProject(t, root, baseRules) })
			}

			cs := connectGate(t, g, version, script.handle)

			_, refusal, err := callGated(t, cs, "cg_run_many", map[string]any{"commands": [][]string{{"echo", "a"}, {"printf", "b"}}})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}

			want := "cg_run_many refused: command was declined at the approval prompt\n  printf b\nskipping remember: "
			if !strings.HasPrefix(refusal, want) || !strings.Contains(refusal, "changed on disk") {
				t.Errorf("refusal = %q, want it to start with %q and name the changed file", refusal, want)
			}

			assertCaptureRootEmpty(t)
		})
	}
}

// retryArgsTest asks for approval with First, then retries with the first round's request state
// and an accepting answer but the arguments in Retry. WantRun says whether the retry may run.
type retryArgsTest struct {
	Name  string
	Tool  string
	First map[string]any
	Retry map[string]any

	WantRun bool
}

var retryArgsTests = []retryArgsTest{
	{
		Name:  "pool retried unchanged runs",
		Tool:  "cg_run_many",
		First: map[string]any{"commands": echoCommands("a")},
		Retry: map[string]any{"commands": echoCommands("a")},

		WantRun: true,
	},
	{
		Name:  "pool retried with a larger repeat refuses",
		Tool:  "cg_run_many",
		First: map[string]any{"commands": echoCommands("a")},
		Retry: map[string]any{"commands": echoCommands("a"), "repeat": 50},
	},
	{
		Name:  "pool retried with a duplicated command refuses",
		Tool:  "cg_run_many",
		First: map[string]any{"commands": echoCommands("a")},
		Retry: map[string]any{"commands": echoCommands("a", "a")},
	},
	{
		Name:  "pool retried with different parallelism refuses",
		Tool:  "cg_run_many",
		First: map[string]any{"commands": echoCommands("a"), "repeat": 4, "parallelism": 1},
		Retry: map[string]any{"commands": echoCommands("a"), "repeat": 4, "parallelism": 4},
	},
	{
		Name:  "run retried with a different output option refuses",
		Tool:  "cg_run",
		First: map[string]any{"command": []string{"echo", "a"}},
		Retry: map[string]any{"command": []string{"echo", "a"}, "excerpt_bytes": 16},
	},
}

// TestApprovalRetryMustRepeatTheCall turns off the client's multi round-trip handling so the test
// can play the client and send a retry whose arguments differ from the call the user approved.
func TestApprovalRetryMustRepeatTheCall(t *testing.T) {
	for _, test := range retryArgsTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			g := newTestGate(t, "version: 1\n", false)
			cs := connectGateWith(t, g, protocolMultiRoundTrip, &mcpsdk.ClientOptions{
				ElicitationHandler: (&formScript{}).handle,
				MultiRoundTrip:     &mcpsdk.MultiRoundTripOptions{Disabled: true},
			})

			first, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: test.Tool, Arguments: test.First})
			if err != nil {
				t.Fatalf("first CallTool: %v", err)
			}
			if !first.NeedsInput() {
				t.Fatalf("first call did not ask for approval: IsError=%v, text=%q", first.IsError, resultText(first))
			}

			answers := mcpsdk.InputResponseMap{}
			for id := range first.InputRequests {
				answers[id] = accept(map[string]any{"remember": false})
			}

			retry, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
				Name:           test.Tool,
				Arguments:      test.Retry,
				InputResponses: answers,
				RequestState:   first.RequestState,
			})
			if err != nil {
				t.Fatalf("retry CallTool: %v", err)
			}

			if test.WantRun {
				if retry.IsError || retry.NeedsInput() {
					t.Fatalf("retry did not run: IsError=%v, NeedsInput=%v, text=%q", retry.IsError, retry.NeedsInput(), resultText(retry))
				}

				return
			}

			if text := resultText(retry); !retry.IsError || !strings.Contains(text, "call the tool again") {
				t.Errorf("retry result = %q, want a refusal that says to call the tool again", text)
			}
			assertCaptureRootEmpty(t)
		})
	}
}
