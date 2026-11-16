package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
)

const nestedStderr = "sandbox-exec: sandbox_apply: Operation not permitted"

// nestedFixture is a gate whose load probe found cg inside an outer sandbox. The probe is faked, so
// none of these tests need a working sandbox-exec. The sandbox go covers the probe script run on the
// project root, and echo is allowed with no sandbox.
type nestedFixture struct {
	*sandboxFixture

	Gate   *gate
	Script string
}

func newNestedFixture(t *testing.T, class model.SandboxClass, already string, blindly bool) *nestedFixture {
	t.Helper()

	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("NO_PROXY", "")

	f := newSandboxFixture(t)
	f.Probe = fakeProbe(class, nestedStderr)
	f.write("p.sb", "(version 1)\n(allow default)\n", 0o644)

	script := f.write(filepath.Join(f.Scripts, "probe"), probeScript, 0o755)
	rules := "version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: p.sb, already_sandboxed: " + already + ", proxy: 'http://localhost:3128'}\n" +
		"allow:\n  - prefix: [" + script + ", " + f.Root + "]\n    sandbox: go\n  - prefix: [echo]\n"

	return &nestedFixture{sandboxFixture: f, Gate: f.load(rules, blindly), Script: script}
}

func (n *nestedFixture) command() []string { return []string{n.Script, n.Root} }

func (n *nestedFixture) rulesFile() string {
	n.t.Helper()

	data, err := os.ReadFile(filepath.Join(n.Root, ".cg.yaml"))
	if err != nil {
		n.t.Fatal(err)
	}

	return string(data)
}

func nestedCall(argvs ...[]string) approvalCall {
	call := testCall(argvs...)
	call.MultiRound = true

	return call
}

type nestedPolicyTest struct {
	Name      string
	Class     model.SandboxClass
	Already   string
	CanPrompt bool

	WantErr     []string
	WantAbsent  []string
	WantSkipped bool
	WantApplied bool
	WantPending bool
}

var nestedPolicyTests = []nestedPolicyTest{
	{
		Name: "nested refuse refuses and names the outer sandbox", Class: model.SandboxNested, Already: "refuse",
		WantErr: []string{`sandbox "go"`, "already inside another sandbox", "already_sandboxed is refuse", nestedStderr},
	},
	{
		Name: "nested run runs without the profile", Class: model.SandboxNested, Already: "run",
		WantSkipped: true,
	},
	{
		Name: "nested ask prompts when the client can", Class: model.SandboxNested, Already: "ask", CanPrompt: true,
		WantPending: true,
	},
	{
		Name: "nested ask fails closed without a prompt and does not recommend blindly-allow", Class: model.SandboxNested, Already: "ask",
		WantErr:    []string{`sandbox "go"`, "already inside another sandbox", "cannot prompt", "already_sandboxed"},
		WantAbsent: []string{"blindly-allow"},
	},
	{Name: "applies with run applies the profile", Class: model.SandboxApplies, Already: "run", WantApplied: true},
	{Name: "applies with refuse applies the profile", Class: model.SandboxApplies, Already: "refuse", WantApplied: true},
	{Name: "applies with ask applies the profile", Class: model.SandboxApplies, Already: "ask", WantApplied: true},
	{
		Name: "unknown refuses with run", Class: model.SandboxUnknown, Already: "run",
		WantErr: []string{"classified it as unknown: " + nestedStderr},
	},
	{
		Name: "unknown refuses with refuse", Class: model.SandboxUnknown, Already: "refuse",
		WantErr: []string{"classified it as unknown: " + nestedStderr},
	},
	{
		Name: "unknown refuses with ask", Class: model.SandboxUnknown, Already: "ask", CanPrompt: true,
		WantErr: []string{"classified it as unknown: " + nestedStderr},
	},
}

func TestGateActsOnTheStoredNestingClass(t *testing.T) {
	for _, blindly := range []bool{false, true} {
		for _, test := range nestedPolicyTests {
			name := test.Name
			if blindly {
				name += " under blindly-allow"
			}

			t.Run(name, func(t *testing.T) {
				n := newNestedFixture(t, test.Class, test.Already, blindly)

				call := nestedCall(n.command())
				call.CanPrompt = test.CanPrompt

				out, err := n.Gate.check(call)

				if len(test.WantErr) > 0 {
					if err == nil {
						t.Fatalf("check = %+v, want a refusal", out)
					}
					for _, want := range test.WantErr {
						if !strings.Contains(err.Error(), want) {
							t.Errorf("error = %q, want it to contain %q", err, want)
						}
					}
					for _, bad := range test.WantAbsent {
						if strings.Contains(err.Error(), bad) {
							t.Errorf("error = %q, want it not to contain %q", err, bad)
						}
					}

					return
				}

				if err != nil {
					t.Fatalf("check: %v", err)
				}
				if (out.Pending != nil) != test.WantPending {
					t.Fatalf("pending = %v, want %v", out.Pending != nil, test.WantPending)
				}
				if test.WantPending {
					return
				}

				sb := out.sandboxAt(0)
				if sb == nil || sb.Name != "go" {
					t.Fatalf("sandbox = %v, want go", sb)
				}
				if skipped := out.skipAt(0) != ""; skipped != test.WantSkipped {
					t.Errorf("skipped = %q, want skipped=%v", out.skipAt(0), test.WantSkipped)
				}
			})
		}
	}
}

func TestNestedRunWithoutTheProfileRecordsTheSkipAndSetsNoProxy(t *testing.T) {
	n := newNestedFixture(t, model.SandboxNested, "run", false)

	_, out, err := handleRun(context.Background(), nil, n.Gate, nil, runOrigin{}, runInput{Command: n.command()})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if want := "proxy=none noproxy=none\nwrote\n"; out.StdoutExcerpt != want {
		t.Errorf("output = %q, want %q: no proxy variables and no profile", out.StdoutExcerpt, want)
	}

	meta, err := model.ReadMeta(filepath.Join(model.CaptureRoot(), out.ID))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Sandbox != "go" || !slices.Equal(meta.Command, n.command()) {
		t.Errorf("record = command %q sandbox %q, want the typed command and the sandbox name", meta.Command, meta.Sandbox)
	}
	if !strings.Contains(meta.SandboxSkipped, "outer sandbox") || !strings.Contains(meta.SandboxSkipped, "already_sandboxed is run") {
		t.Errorf("sandbox_skipped = %q, want the reason", meta.SandboxSkipped)
	}
}

func TestNestedRunManyRunsEachMemberWithoutItsProfile(t *testing.T) {
	n := newNestedFixture(t, model.SandboxNested, "run", false)

	commands := [][]string{n.command(), {"echo", "ok"}}

	_, out, err := handleRunMany(context.Background(), nil, n.Gate, nil, runOrigin{}, runManyInput{Commands: commands})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}

	for _, run := range out.Runs {
		meta, err := model.ReadMeta(filepath.Join(model.CaptureRoot(), run.RunID))
		if err != nil {
			t.Fatal(err)
		}

		if got := run.Command == 0; got != (meta.SandboxSkipped != "") {
			t.Errorf("command %d: sandbox_skipped = %q", run.Command, meta.SandboxSkipped)
		}
		if run.Command == 0 && !strings.HasPrefix(runOutputOf(t, filepath.Join(model.CaptureRoot(), run.RunID)), "proxy=none ") {
			t.Errorf("a skipped member received proxy variables")
		}
	}
}

// schemaRecorder is a client handler that answers every form with result and keeps what it was shown.
type schemaRecorder struct {
	Result *mcpsdk.ElicitResult

	mu      sync.Mutex
	shown   []string
	schemas []string
}

func (s *schemaRecorder) handle(_ context.Context, req *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
	schema, _ := json.Marshal(req.Params.RequestedSchema)

	s.mu.Lock()
	s.shown = append(s.shown, req.Params.Message)
	s.schemas = append(s.schemas, string(schema))
	s.mu.Unlock()

	return s.Result, nil
}

func (s *schemaRecorder) forms() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.shown)
}

func TestNestedAskPromptsAndAcceptingRunsWithoutTheProfile(t *testing.T) {
	for _, version := range approvalProtocols {
		for _, blindly := range []bool{false, true} {
			name := version
			if blindly {
				name += "/blindly-allow"
			}

			t.Run(name, func(t *testing.T) {
				n := newNestedFixture(t, model.SandboxNested, "ask", blindly)
				before := n.rulesFile()

				rec := &schemaRecorder{Result: accept(map[string]any{})}
				cs := connectGate(t, n.Gate, version, rec.handle)

				out, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": n.command()})
				if err != nil || refusal != "" {
					t.Fatalf("cg_run: err = %v, refusal = %q", err, refusal)
				}
				if got, _ := out["stdout_excerpt"].(string); got != "proxy=none noproxy=none\nwrote\n" {
					t.Errorf("stdout = %q, want the command run with no proxy variables", got)
				}
				if rec.forms() != 1 {
					t.Fatalf("forms shown = %d, want exactly one across both calls of the round", rec.forms())
				}

				if msg := rec.shown[0]; !strings.Contains(msg, `without sandbox "go"`) || !strings.Contains(msg, "already inside another sandbox") || strings.Contains(msg, "Approval ") {
					t.Errorf("message = %q, want it to name the sandbox and the outer one, with no position for a lone form", msg)
				}
				if schema := rec.schemas[0]; strings.Contains(schema, "remember") || strings.Contains(schema, "rule") {
					t.Errorf("schema = %s, want no remember field and no rule field", schema)
				}

				meta, err := model.ReadMeta(filepath.Join(model.CaptureRoot(), out["id"].(string)))
				if err != nil {
					t.Fatal(err)
				}
				if meta.Sandbox != "go" || !strings.Contains(meta.SandboxSkipped, "approved") {
					t.Errorf("record = sandbox %q skipped %q, want the approval as the reason", meta.Sandbox, meta.SandboxSkipped)
				}

				if _, _, err := callGated(t, cs, "cg_run", map[string]any{"command": n.command()}); err != nil {
					t.Fatal(err)
				}
				if rec.forms() != 2 {
					t.Errorf("forms shown = %d, want a second prompt: an approval is never remembered", rec.forms())
				}
				if n.rulesFile() != before {
					t.Errorf("rules file changed:\n%s", n.rulesFile())
				}
			})
		}
	}
}

func TestNestedAskDecliningRefuses(t *testing.T) {
	for _, version := range approvalProtocols {
		for _, blindly := range []bool{false, true} {
			t.Run(version, func(t *testing.T) {
				n := newNestedFixture(t, model.SandboxNested, "ask", blindly)

				for _, result := range []*mcpsdk.ElicitResult{decline(), cancel()} {
					rec := &schemaRecorder{Result: result}
					cs := connectGate(t, n.Gate, version, rec.handle)

					out, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": n.command()})
					if err != nil || out != nil {
						t.Fatalf("cg_run: out = %v, err = %v, want a tool error", out, err)
					}
					if !strings.Contains(refusal, `without sandbox "go" was declined`) {
						t.Errorf("refusal = %q, want the declined prompt named", refusal)
					}
				}
			})
		}
	}
}

func TestNestedAskWithoutAPromptingClientFailsClosed(t *testing.T) {
	for _, blindly := range []bool{false, true} {
		n := newNestedFixture(t, model.SandboxNested, "ask", blindly)
		cs := connectGate(t, n.Gate, protocolMultiRoundTrip, nil)

		out, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": n.command()})
		if err != nil || out != nil {
			t.Fatalf("blindly=%v: out = %v, err = %v, want a tool error", blindly, out, err)
		}
		if !strings.Contains(refusal, "cannot prompt") || strings.Contains(refusal, "blindly-allow") {
			t.Errorf("blindly=%v: refusal = %q, want it to say the client cannot prompt and not recommend --blindly-allow", blindly, refusal)
		}
	}
}

func TestNestedAskMixesWithRulePromptsInOneRound(t *testing.T) {
	for _, version := range approvalProtocols {
		t.Run(version, func(t *testing.T) {
			n := newNestedFixture(t, model.SandboxNested, "ask", false)

			commands := [][]string{n.command(), {"echo", "ok"}, {"printf", "b"}}
			nested, rule := visibleArgs(commands[0]), visibleArgs(commands[2])

			script := &commandScript{Answers: map[string]*mcpsdk.ElicitResult{
				nested: accept(map[string]any{}),
				rule:   accept(map[string]any{"remember": false}),
			}}
			cs := connectGate(t, n.Gate, version, script.handle)

			out, refusal, err := callGated(t, cs, "cg_run_many", map[string]any{"commands": commands})
			if err != nil || refusal != "" {
				t.Fatalf("cg_run_many: err = %v, refusal = %q", err, refusal)
			}
			if got := out["total"]; got != float64(3) {
				t.Errorf("total = %v, want all three commands run", got)
			}

			messages := script.messages()
			if len(messages) != 2 {
				t.Fatalf("forms shown = %d (%q), want one for the rule and one for the nested sandbox", len(messages), messages)
			}

			// The client answers forms concurrently, so the order they arrive in is not the form order.
			var nestedMsg, ruleMsg string
			for _, message := range messages {
				switch {
				case strings.Contains(message, `without sandbox "go"`):
					nestedMsg = message
				case strings.Contains(message, "allow this command"):
					ruleMsg = message
				}
			}

			if !strings.HasPrefix(ruleMsg, "Approval 1 of 2") || !strings.HasPrefix(nestedMsg, "Approval 2 of 2") {
				t.Errorf("messages = %q, want the rule form first and the sandbox form second, numbered of 2", messages)
			}
		})
	}
}

func TestNestedAskDeclinedKeepsTheRememberOfAnAcceptedRuleForm(t *testing.T) {
	for _, version := range approvalProtocols {
		t.Run(version, func(t *testing.T) {
			n := newNestedFixture(t, model.SandboxNested, "ask", false)

			commands := [][]string{n.command(), {"printf", "b"}}
			script := &commandScript{Answers: map[string]*mcpsdk.ElicitResult{
				visibleArgs(commands[1]): accept(map[string]any{"remember": true, "rule": "[printf, b]"}),
			}}
			cs := connectGate(t, n.Gate, version, script.handle)

			out, refusal, err := callGated(t, cs, "cg_run_many", map[string]any{"commands": commands})
			if err != nil || out != nil {
				t.Fatalf("out = %v, err = %v, want a refusal", out, err)
			}
			if !strings.Contains(refusal, "declined") || !strings.Contains(refusal, filepath.Base(commands[0][0])) {
				t.Errorf("refusal = %q, want the declined nested command named", refusal)
			}
			if !strings.Contains(n.rulesFile(), "printf") {
				t.Errorf("rules file = %q, want the accepted form's remembered rule saved", n.rulesFile())
			}
		})
	}
}

func TestNestedAskRefusesAnApprovalThatNoLongerMatches(t *testing.T) {
	n := newNestedFixture(t, model.SandboxNested, "ask", false)

	first, err := n.Gate.check(nestedCall(n.command()))
	if err != nil || first.Pending == nil {
		t.Fatalf("first check = %+v, %v, want a pending prompt", first, err)
	}

	other := []string{n.Script, n.Root, "extra"}
	retry := nestedCall(other)
	retry.State = first.Pending.RequestState

	if _, err := n.Gate.check(retry); err == nil || !strings.Contains(err.Error(), "differs from the one that was approved") {
		t.Errorf("retry of a different call = %v, want it refused", err)
	}
}

func TestNestedApprovalSurvivesASecondRoundForAFailedRememberEdit(t *testing.T) {
	n := newNestedFixture(t, model.SandboxNested, "ask", false)

	var mu sync.Mutex
	var shown []string

	handler := func(_ context.Context, req *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
		mu.Lock()
		defer mu.Unlock()

		shown = append(shown, req.Params.Message)

		switch {
		case strings.Contains(req.Params.Message, "without sandbox"):
			return accept(map[string]any{}), nil
		case strings.Contains(req.Params.Message, "needs another look"):
			return accept(map[string]any{"remember": true, "rule": "[printf]"}), nil
		}

		return accept(map[string]any{"remember": true, "rule": "[printf, bye]"}), nil
	}
	cs := connectGate(t, n.Gate, protocolMultiRoundTrip, handler)

	commands := [][]string{n.command(), {"printf", "b"}}

	out, refusal, err := callGated(t, cs, "cg_run_many", map[string]any{"commands": commands})
	if err != nil || refusal != "" {
		t.Fatalf("cg_run_many: err = %v, refusal = %q", err, refusal)
	}
	if got := out["total"]; got != float64(2) {
		t.Errorf("total = %v, want both commands run", got)
	}

	if len(shown) != 3 {
		t.Fatalf("forms shown = %d (%q), want the two of round one and the re-asked rule form", len(shown), shown)
	}
	if !strings.HasPrefix(shown[2], "Approval 1 of 2") || !strings.Contains(shown[2], "needs another look") {
		t.Errorf("round two message = %q, want the rule form's position of both kinds", shown[2])
	}
	if !strings.Contains(n.rulesFile(), "prefix: [printf]") {
		t.Errorf("rules file = %q, want the corrected rule saved", n.rulesFile())
	}

	for _, run := range runsOf(t, out) {
		meta, err := model.ReadMeta(filepath.Join(model.CaptureRoot(), run))
		if err != nil {
			t.Fatal(err)
		}
		if got := slices.Equal(meta.Command, n.command()); got != strings.Contains(meta.SandboxSkipped, "approved") {
			t.Errorf("command %q: sandbox_skipped = %q", meta.Command, meta.SandboxSkipped)
		}
	}
}

func runsOf(t *testing.T, out map[string]any) []string {
	t.Helper()

	var ids []string
	runs, _ := out["runs"].([]any)
	for _, run := range runs {
		if m, ok := run.(map[string]any); ok {
			ids = append(ids, m["run_id"].(string))
		}
	}

	if len(ids) == 0 {
		t.Fatalf("output = %v, want runs", out)
	}

	return ids
}

// nestedGateReport is what the helper process prints: the load class of each sandbox and what the
// gate did with a command under it.
type nestedGateReport struct {
	Sandbox string
	Class   string
	Stderr  string
	Err     string
	Skipped bool
	Pending bool
}

// nestedGateRules is the project file the helper loads. The parent writes it, since the outer
// sandbox lets the helper read files and write none.
func nestedGateRules() string {
	var rules strings.Builder

	rules.WriteString("version: 2\nsandboxes:\n")
	for _, name := range []string{"refuse", "run", "ask"} {
		rules.WriteString("  " + name + ":\n    seatbelt: {profile: p.sb, already_sandboxed: " + name + "}\n")
	}

	rules.WriteString("allow:\n")
	for _, name := range []string{"refuse", "run", "ask"} {
		rules.WriteString("  - prefix: [echo, " + name + "]\n    sandbox: " + name + "\n")
	}

	return rules.String()
}

// nestedGateMain is the helper process of TestNestedGateUnderARealOuterSandbox. It loads three
// sandboxes with the real probe and the real sandbox-exec, and reports the gate's decision for a
// command under each. Run inside an outer sandbox, the probe finds that nesting is blocked.
func nestedGateMain(args []string) int {
	root, home := args[0], args[1]

	store, err := approve.Load(approve.LoadOptions{
		GlobalPath:  filepath.Join(root, "no-global.yaml"),
		ProjectRoot: root,
		Home:        func() (string, error) { return home, nil },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		return 2
	}

	g := &gate{store: store}

	var reports []nestedGateReport
	for _, name := range []string{"refuse", "run", "ask"} {
		sb, _ := store.Ruleset().Sandbox(name)
		report := nestedGateReport{Sandbox: name, Class: string(sb.Seatbelt.Class), Stderr: sb.Seatbelt.ProbeStderr}

		argv := []string{"echo", name}
		resolved, _ := model.ResolveCommand(argv, "")

		out, err := g.check(approvalCall{Tool: "cg_run", Argvs: [][]string{argv}, Resolutions: []*model.Resolution{resolved}, CanPrompt: true, MultiRound: true})
		switch {
		case err != nil:
			report.Err = err.Error()
		default:
			report.Skipped = out.skipAt(0) != ""
			report.Pending = out.Pending != nil
		}

		reports = append(reports, report)
	}

	if err := json.NewEncoder(os.Stdout).Encode(reports); err != nil {
		return 2
	}

	return 0
}

// outerDenyDefault is an outer sandbox under which the kernel refuses to apply another one. A probe
// showed that an outer allow-default profile does not block nesting, and that this one does.
const outerDenyDefault = "(version 1)\n(deny default)\n(allow process*)\n(allow file-read*)\n(allow sysctl-read)\n(allow mach-lookup)\n(allow file-write* (literal \"/dev/null\"))\n"

// TestNestedGateUnderARealOuterSandbox proves the load probe and the gate against the kernel's own
// refusal to nest, which no faked probe can. It runs the gate in a child process, once bare and once
// inside an outer deny-default sandbox. It cannot prove how Claude Code's own sandbox words the
// refusal, only the exact line sandbox-exec prints for this one.
func TestNestedGateUnderARealOuterSandbox(t *testing.T) {
	requireSandboxExec(t)

	run := func(outer bool) []nestedGateReport {
		t.Helper()

		f := newSandboxFixture(t)
		f.write("p.sb", "(version 1)\n(allow default)\n", 0o644)
		f.write(".cg.yaml", nestedGateRules(), 0o644)

		args := []string{os.Args[0], "nested-gate", f.Root, f.Home}
		name := os.Args[0]
		if outer {
			name = model.SandboxExecPath
			args = append([]string{"-p", outerDenyDefault}, args...)
		} else {
			args = args[1:]
		}

		cmd := exec.Command(name, args...)

		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr

		if err := cmd.Run(); err != nil {
			t.Fatalf("outer=%v: %v\nstdout: %s\nstderr: %s", outer, err, stdout.String(), stderr.String())
		}

		var reports []nestedGateReport
		if err := json.Unmarshal(stdout.Bytes(), &reports); err != nil {
			t.Fatalf("outer=%v: decoding %q: %v", outer, stdout.String(), err)
		}

		return reports
	}

	bare := run(false)
	for _, r := range bare {
		if r.Class != "applies" || r.Err != "" || r.Skipped || r.Pending {
			t.Errorf("outside a sandbox, report = %+v, want every value to apply the profile", r)
		}
	}

	for _, r := range run(true) {
		if r.Class != "nested" || r.Stderr != nestedStderr {
			t.Errorf("report = %+v, want the load probe to find the exact nesting line", r)
		}

		switch r.Sandbox {
		case "refuse":
			if !strings.Contains(r.Err, "already inside another sandbox") {
				t.Errorf("refuse: report = %+v, want a refusal naming the outer sandbox", r)
			}
		case "run":
			if r.Err != "" || !r.Skipped {
				t.Errorf("run: report = %+v, want the command to run without the profile", r)
			}
		case "ask":
			if r.Err != "" || !r.Pending {
				t.Errorf("ask: report = %+v, want a prompt", r)
			}
		}
	}
}

type staleNestedTest struct {
	Name string

	// Mutate changes what the first round recorded, so the retry finds the approval no longer matches
	// the command. A changed recording stands for a changed live state, since the live side of each
	// comparison is computed afresh on the retry.
	Mutate func(entry *pendingEntry)

	WantErr string
}

var staleNestedTests = []staleNestedTest{
	{
		Name:    "a command no sandbox form covered",
		Mutate:  func(e *pendingEntry) { e.Commands[0].NestedForm = "" },
		WantErr: "was not part of the approval that was requested",
	},
	{
		Name:    "a form that named a different sandbox",
		Mutate:  func(e *pendingEntry) { e.Commands[0].NestedSandbox = "other" },
		WantErr: "was not part of the approval that was requested",
	},
	{
		Name: "a command that now resolves to a different executable",
		Mutate: func(e *pendingEntry) {
			e.Commands[0].Resolution = &model.Resolution{Resolved: "/bin/elsewhere", Canonical: "/bin/elsewhere"}
		},
		WantErr: "now resolves to a different executable than the one that was shown",
	},
	{
		Name: "an inner command that now resolves to a different executable",
		Mutate: func(e *pendingEntry) {
			e.Commands[0].Chain = []pendingLayer{{Offset: 1, Resolution: &model.Resolution{Resolved: "/bin/elsewhere", Canonical: "/bin/elsewhere"}}}
		},
		WantErr: "the inner command",
	},
	{
		Name:    "a carried round that never approved the command",
		Mutate:  func(e *pendingEntry) { e.Carry = &carriedRound{Failed: map[string]askAgain{}} },
		WantErr: "without its sandbox was not approved in the earlier round",
	},
}

func TestNestedAskRefusesAnApprovalThatNoLongerMatchesWhatWasShown(t *testing.T) {
	for _, test := range staleNestedTests {
		t.Run(test.Name, func(t *testing.T) {
			n := newNestedFixture(t, model.SandboxNested, "ask", false)

			first, err := n.Gate.check(nestedCall(n.command()))
			if err != nil || first.Pending == nil {
				t.Fatalf("first check = %+v, %v, want a pending prompt", first, err)
			}

			nonce := first.Pending.RequestState

			pending := n.Gate.pending()
			pending.mu.Lock()
			test.Mutate(pending.entries[nonce])
			pending.mu.Unlock()

			retry := nestedCall(n.command())
			retry.State = nonce
			retry.Responses = mcpsdk.InputResponseMap{}
			for id := range first.Pending.InputRequests {
				retry.Responses[id] = accept(map[string]any{})
			}

			out, err := n.Gate.check(retry)
			if err == nil || !errors.Is(err, ErrPendingApproval) || !strings.Contains(err.Error(), test.WantErr) {
				t.Errorf("retry = %+v, %v, want a pending refusal containing %q", out, err, test.WantErr)
			}
		})
	}
}
