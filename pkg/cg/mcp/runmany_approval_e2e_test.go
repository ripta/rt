package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// commandScript answers each approval form by the command it shows. A command with no entry is
// declined. OnShow runs for every form before it is answered.
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

	for command, res := range c.Answers {
		if strings.Contains(req.Params.Message, "\n  "+command+"\n") {
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

			messages := script.messages()
			if len(messages) != maxPoolCommands {
				t.Fatalf("forms shown = %d, want %d in a single round", len(messages), maxPoolCommands)
			}

			positions := make(map[string]bool, len(messages))
			for _, m := range messages {
				positions[strings.SplitN(m, "\n", 2)[0]] = true
			}
			for i := 1; i <= maxPoolCommands; i++ {
				if want := fmt.Sprintf("Approval %d of %d", i, maxPoolCommands); !positions[want] {
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
		Args:          map[string]any{"commands": echoCommands("a", "b", "c")},
		Project:       "version: 1\n",
		Approved:      []string{"echo a", "echo b", "echo c"},
		WantPositions: []string{"Approval 1 of 3", "Approval 2 of 3", "Approval 3 of 3"},
	},
	{
		Name:          "positions count only the commands that need a form",
		Tool:          "cg_run_many",
		Args:          map[string]any{"commands": echoCommands("a", "b", "c")},
		Project:       "version: 1\nallow:\n  - prefix: [echo, b]\n",
		Approved:      []string{"echo a", "echo c"},
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
		Args:          map[string]any{"commands": echoCommands("a", "b")},
		Project:       baseRules,
		Edit:          editRules,
		Approved:      []string{"echo a", "echo b"},
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
			"echo a": accept(map[string]any{"remember": false}),
			"echo b": decline(),
			"echo c": accept(map[string]any{"remember": false}),
		},
		WantRefusal: "cg_run_many refused: command was declined at the approval prompt\n  echo b",
	},
	{
		Name: "several declines name every command in command order",
		Answers: map[string]*mcpsdk.ElicitResult{
			"echo a": decline(),
			"echo b": accept(map[string]any{"remember": false}),
			"echo c": cancel(),
		},
		WantRefusal: "cg_run_many refused: 2 commands were declined at the approval prompt\n  echo a\n  echo c",
	},
	{
		Name: "an accepted remember is persisted despite a decline",
		Answers: map[string]*mcpsdk.ElicitResult{
			"echo a": accept(map[string]any{"remember": true, "rule": "[echo, a]"}),
			"echo b": decline(),
			"echo c": accept(map[string]any{"remember": false}),
		},
		WantRefusal: "cg_run_many refused: command was declined at the approval prompt\n  echo b",
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

				out, refusal, err := callGated(t, cs, "cg_run_many", map[string]any{"commands": echoCommands("a", "b", "c")})
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
					"echo a": accept(map[string]any{"remember": true, "rule": "[echo, a]"}),
					"echo b": decline(),
				},
			}

			var once sync.Once
			script.OnShow = func(string) {
				once.Do(func() { writeProject(t, root, baseRules) })
			}

			cs := connectGate(t, g, version, script.handle)

			_, refusal, err := callGated(t, cs, "cg_run_many", map[string]any{"commands": echoCommands("a", "b")})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}

			want := "cg_run_many refused: command was declined at the approval prompt\n  echo b\nskipping remember: "
			if !strings.HasPrefix(refusal, want) || !strings.Contains(refusal, "changed on disk") {
				t.Errorf("refusal = %q, want it to start with %q and name the changed file", refusal, want)
			}

			assertCaptureRootEmpty(t)
		})
	}
}
