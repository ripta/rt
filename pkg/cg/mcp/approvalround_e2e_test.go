package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	protocolMultiRoundTrip = "2026-07-28"
	protocolLegacy         = "2025-11-25"
)

var approvalProtocols = []string{protocolMultiRoundTrip, protocolLegacy}

// formScript is a client-side elicitation handler that answers the approval form and the
// divergence form with canned results and records every message it was shown.
type formScript struct {
	Approval   *mcpsdk.ElicitResult
	Divergence *mcpsdk.ElicitResult

	mu    sync.Mutex
	shown []string
}

func (f *formScript) handle(_ context.Context, req *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
	f.mu.Lock()
	f.shown = append(f.shown, req.Params.Message)
	f.mu.Unlock()

	if strings.Contains(req.Params.Message, "changed on disk") {
		if f.Divergence == nil {
			return nil, fmt.Errorf("unexpected divergence form")
		}

		return f.Divergence, nil
	}

	return f.Approval, nil
}

func (f *formScript) forms() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.shown)
}

// callGated calls a gated tool and returns its decoded structured output, or the text of a tool
// error. A protocol-level failure comes back as err.
func callGated(t *testing.T, cs *mcpsdk.ClientSession, tool string, args map[string]any) (out map[string]any, refusal string, err error) {
	t.Helper()

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, "", err
	}

	if res.IsError {
		return nil, resultText(res), nil
	}

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling structured content: %v", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshaling structured content: %v", err)
	}

	return out, "", nil
}

// resultText joins the text content of a tool result, which is where a tool error's message
// lands.
func resultText(res *mcpsdk.CallToolResult) string {
	var text []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			text = append(text, tc.Text)
		}
	}

	return strings.Join(text, "\n")
}

// approvalE2ETest drives cg_run through a real SDK client. Edit rewrites the project file after
// load so the first call sees a divergence. WantLive repeats the command afterwards and expects
// no further prompt.
type approvalE2ETest struct {
	Name       string
	Project    string
	Edit       string
	Approval   *mcpsdk.ElicitResult
	Divergence *mcpsdk.ElicitResult

	WantRefusal string
	WantForms   int
	WantWarning bool
	WantLive    bool
	WantFile    []string
	AbsentFile  []string
}

var approvalE2ETests = []approvalE2ETest{
	{
		Name:     "accept runs the command",
		Project:  "version: 1\n",
		Approval: accept(map[string]any{"remember": false}),

		WantForms:  1,
		AbsentFile: []string{"prefix:"},
	},
	{
		Name:     "decline refuses the command",
		Project:  "version: 1\n",
		Approval: decline(),

		WantForms:   1,
		WantRefusal: "cg_run refused: command was declined at the approval prompt",
	},
	{
		Name:     "remember writes a rule that goes live",
		Project:  "version: 1\n",
		Approval: accept(map[string]any{"remember": true, "rule": "[echo, hi]"}),

		WantForms: 1,
		WantLive:  true,
		WantFile:  []string{"exact: [echo, hi]"},
	},
	{
		Name:       "divergence reload and merge keeps the disk change",
		Project:    baseRules,
		Edit:       editRules,
		Approval:   accept(map[string]any{"remember": true, "rule": "[echo, hi]"}),
		Divergence: accept(map[string]any{"choice": divergeReloadMerge}),

		WantForms: 2,
		WantLive:  true,
		WantFile:  []string{"prefix: [make]", "prefix: [npm, ci]", "exact: [echo, hi]"},
	},
	{
		Name:       "divergence skip runs the command and leaves the file",
		Project:    baseRules,
		Edit:       editRules,
		Approval:   accept(map[string]any{"remember": true, "rule": "[echo, hi]"}),
		Divergence: accept(map[string]any{"choice": divergeSkip}),

		WantForms:  2,
		WantFile:   []string{"prefix: [npm, ci]"},
		AbsentFile: []string{"exact: [echo, hi]"},
	},
}

func TestApprovalEndToEnd(t *testing.T) {
	for _, version := range approvalProtocols {
		for _, test := range approvalE2ETests {
			t.Run(version+"/"+test.Name, func(t *testing.T) {
				t.Setenv("TMPDIR", t.TempDir())

				root := t.TempDir()
				g := newTestGateAt(t, root, test.Project, false)
				if test.Edit != "" {
					writeProject(t, root, test.Edit)
				}

				script := &formScript{Approval: test.Approval, Divergence: test.Divergence}
				cs := connectGate(t, g, version, script.handle)

				out, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi"}})
				if err != nil {
					t.Fatalf("CallTool: %v", err)
				}

				if test.WantRefusal != "" {
					if refusal != test.WantRefusal {
						t.Errorf("refusal = %q, want %q", refusal, test.WantRefusal)
					}
				} else {
					if refusal != "" {
						t.Fatalf("unexpected refusal: %s", refusal)
					}
					if code, ok := out["exit_code"].(float64); !ok || code != 0 {
						t.Errorf("exit_code = %v, want 0", out["exit_code"])
					}
					if _, has := out["remember_warning"]; has != test.WantWarning {
						t.Errorf("remember_warning present = %v, want %v", has, test.WantWarning)
					}
				}

				if got := script.forms(); got != test.WantForms {
					t.Errorf("forms shown = %d, want %d", got, test.WantForms)
				}

				var file string
				if raw, err := readProjectIfPresent(root); err == nil {
					file = raw
				}
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

				if !test.WantLive {
					return
				}

				before := script.forms()
				_, refusal, err = callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi"}})
				if err != nil || refusal != "" {
					t.Fatalf("repeat call: err=%v refusal=%q", err, refusal)
				}
				if got := script.forms(); got != before {
					t.Errorf("repeat call showed %d more forms, want 0 once the rule is live", got-before)
				}
			})
		}
	}
}

func TestApprovalEndToEndNoElicitationFailsClosed(t *testing.T) {
	for _, version := range approvalProtocols {
		t.Run(version, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			g := newTestGate(t, "version: 1\n", false)
			cs := connectGate(t, g, version, nil)

			_, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi"}})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if !strings.Contains(refusal, "the client cannot prompt for approval") {
				t.Errorf("refusal = %q, want the fail-closed message", refusal)
			}
		})
	}
}

func TestApprovalEndToEndRunManyAsksOncePerDistinctCommand(t *testing.T) {
	for _, version := range approvalProtocols {
		t.Run(version, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			g := newTestGate(t, "version: 1\n", false)
			script := &formScript{Approval: accept(map[string]any{"remember": false})}
			cs := connectGate(t, g, version, script.handle)

			out, refusal, err := callGated(t, cs, "cg_run_many", map[string]any{
				"commands": [][]string{{"echo", "hi"}, {"echo", "hi"}},
				"repeat":   5,
			})
			if err != nil || refusal != "" {
				t.Fatalf("cg_run_many: err=%v refusal=%q", err, refusal)
			}
			if got := script.forms(); got != 1 {
				t.Errorf("forms shown = %d, want 1 (identical argvs are checked once)", got)
			}
			if got, ok := out["succeeded"].(float64); !ok || got != 10 {
				t.Errorf("succeeded = %v, want 10", out["succeeded"])
			}
		})
	}
}

// TestApprovalEndToEndGroupedRound plays the client at protocol 2026-07-28 so the raw rounds are
// visible. Three distinct commands carry three forms. The retry answers each form by its id, and
// two forms edited to the same rule save it once.
func TestApprovalEndToEndGroupedRound(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)
	cs := connectGateWith(t, g, protocolMultiRoundTrip, &mcpsdk.ClientOptions{
		ElicitationHandler: (&formScript{}).handle,
		MultiRoundTrip:     &mcpsdk.MultiRoundTripOptions{Disabled: true},
	})

	args := map[string]any{"commands": [][]string{{"echo", "a"}, {"printf", "x"}, {"echo", "b"}}}
	first, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "cg_run_many", Arguments: args})
	if err != nil {
		t.Fatalf("first CallTool: %v", err)
	}
	if !first.NeedsInput() {
		t.Fatalf("first call did not ask for approval: IsError=%v, text=%q", first.IsError, resultText(first))
	}
	if len(first.InputRequests) != 3 {
		t.Fatalf("forms = %d, want 3", len(first.InputRequests))
	}

	form, ok := first.InputRequests["approval-1"].(*mcpsdk.ElicitParams)
	if !ok {
		t.Fatalf("approval-1 is %T, want *ElicitParams", first.InputRequests["approval-1"])
	}
	if want := "Approval 1 of 3: allow this command in (server cwd)?\necho a"; form.Message != want {
		t.Errorf("approval-1 message = %q, want %q", form.Message, want)
	}

	retry, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "cg_run_many",
		Arguments: args,
		InputResponses: mcpsdk.InputResponseMap{
			"approval-1": accept(map[string]any{"remember": true, "rule": "[echo]"}),
			"approval-2": accept(map[string]any{"remember": false}),
			"approval-3": accept(map[string]any{"remember": true, "rule": "[echo]"}),
		},
		RequestState: first.RequestState,
	})
	if err != nil {
		t.Fatalf("retry CallTool: %v", err)
	}
	if retry.IsError || retry.NeedsInput() {
		t.Fatalf("retry did not run: IsError=%v, NeedsInput=%v, text=%q", retry.IsError, retry.NeedsInput(), resultText(retry))
	}

	file, _ := readProjectIfPresent(root)
	if got := strings.Count(file, "exact: [echo]"); got != 1 {
		t.Errorf("rule written %d times, want once:\n%s", got, file)
	}
}

// failedPromptTest names what a client whose elicitation handler fails produces at one protocol
// version. The failure is a JSON-RPC error from the call. It never becomes a tool result, so cg's
// refusal text never reaches the caller.
type failedPromptTest struct {
	Version string
	WantErr []string
}

var failedPromptTests = []failedPromptTest{
	{
		Version: protocolMultiRoundTrip,
		WantErr: []string{`fulfilling input request "approval-1"`, "dialog crashed"},
	},
	{
		Version: protocolLegacy,
		WantErr: []string{`calling "tools/call"`, `fulfilling input request "approval-1"`, `calling "elicitation/create"`, "dialog crashed"},
	},
}

// TestApprovalEndToEndFailedPrompt pins the accepted gap in RT-021: the gate has no version branch,
// so a prompt that fails on a 2025-11-25 session surfaces as a protocol error instead of a refusal
// from cg. The 2026-07-28 client fails the same way, because it fulfills the requests itself.
func TestApprovalEndToEndFailedPrompt(t *testing.T) {
	failing := func(context.Context, *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
		return nil, fmt.Errorf("dialog crashed")
	}

	for _, test := range failedPromptTests {
		t.Run(test.Version, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			g := newTestGate(t, "version: 1\n", false)
			cs := connectGate(t, g, test.Version, failing)

			out, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi"}})
			if err == nil {
				t.Fatalf("CallTool succeeded with out=%v refusal=%q, want a protocol error", out, refusal)
			}
			for _, want := range test.WantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "cg_run refused") {
				t.Errorf("error = %q, want no cg refusal text on this path", err)
			}
		})
	}
}

// metaCapabilityTest pins where the gate reads the elicitation capability on a 2026-07-28 session.
// Handler shapes what the client advertises in initialize. Meta is the client capabilities object
// sent in the call's _meta, which SEP-2575 makes authoritative for the request.
type metaCapabilityTest struct {
	Name    string
	Handler func(context.Context, *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error)
	Meta    map[string]any

	WantPrompt bool
}

var metaCapabilityTests = []metaCapabilityTest{
	{
		Name: "elicitation only in _meta prompts",
		Meta: map[string]any{"elicitation": map[string]any{}},

		WantPrompt: true,
	},
	{
		Name:    "elicitation only in initialize fails closed",
		Handler: (&formScript{}).handle,
		Meta:    map[string]any{},
	},
}

// TestApprovalEndToEndCapabilityFromMeta turns off the client's multi round-trip handling so the
// first call's raw result is visible: an approval form when the gate sees elicitation, or the
// fail-closed refusal when it does not.
func TestApprovalEndToEndCapabilityFromMeta(t *testing.T) {
	for _, test := range metaCapabilityTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			g := newTestGate(t, "version: 1\n", false)
			cs := connectGateWith(t, g, protocolMultiRoundTrip, &mcpsdk.ClientOptions{
				ElicitationHandler: test.Handler,
				MultiRoundTrip:     &mcpsdk.MultiRoundTripOptions{Disabled: true},
			})

			res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
				Meta:      mcpsdk.Meta{mcpsdk.MetaKeyClientCapabilities: test.Meta},
				Name:      "cg_run",
				Arguments: map[string]any{"command": []string{"echo", "hi"}},
			})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}

			if got := res.NeedsInput(); got != test.WantPrompt {
				t.Fatalf("NeedsInput = %v, want %v (IsError=%v, content=%v)", got, test.WantPrompt, res.IsError, res.Content)
			}
			if test.WantPrompt {
				if _, ok := res.InputRequests["approval-1"]; !ok {
					t.Errorf("InputRequests = %v, want an approval-1 form", res.InputRequests)
				}

				return
			}

			if text := resultText(res); !res.IsError || !strings.Contains(text, "the client cannot prompt for approval") {
				t.Errorf("result = %q, want the fail-closed refusal", text)
			}
		})
	}
}
