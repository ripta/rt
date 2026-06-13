package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestApprovalSchemaFieldOrder pins the wire order of the approval form so the
// rule field renders above the remember toggle. A property map would marshal its
// keys alphabetically and float "remember" first, so the typed schema's
// PropertyOrder is what keeps this true.
func TestApprovalSchemaFieldOrder(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(approvalSchema([]string{"go", "test"}, "/tmp/.cg.yaml"))
	if err != nil {
		t.Fatalf("marshalling schema: %v", err)
	}
	wire := string(raw)

	idxRule := strings.Index(wire, `"rule"`)
	idxRemember := strings.Index(wire, `"remember"`)
	if idxRule < 0 || idxRemember < 0 {
		t.Fatalf("schema missing a field: %s", wire)
	}
	if idxRule > idxRemember {
		t.Errorf("rule must serialize before remember:\n%s", wire)
	}
}

// fakeElicitor returns canned results for each Elicit call in order, so a test
// can script both the approval prompt and the divergence prompt. It records the
// params it was sent for assertions.
type fakeElicitor struct {
	results []*mcpsdk.ElicitResult
	calls   []*mcpsdk.ElicitParams
}

func (f *fakeElicitor) Elicit(_ context.Context, params *mcpsdk.ElicitParams) (*mcpsdk.ElicitResult, error) {
	f.calls = append(f.calls, params)
	if len(f.calls) > len(f.results) {
		return &mcpsdk.ElicitResult{Action: "cancel"}, nil
	}
	return f.results[len(f.calls)-1], nil
}

func accept(content map[string]any) *mcpsdk.ElicitResult {
	return &mcpsdk.ElicitResult{Action: "accept", Content: content}
}

func projectFile(root string) string {
	return filepath.Join(root, ".cg.yaml")
}

func TestPromptAcceptRunsWithoutRemember(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	rootDir := t.TempDir()
	g := newTestGateAt(t, rootDir, "", false)
	el := &fakeElicitor{results: []*mcpsdk.ElicitResult{accept(map[string]any{"remember": false})}}

	_, out, err := handleRun(context.Background(), nil, g, el, runInput{Command: []string{"echo", "hi"}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", out.ExitCode)
	}
	if _, err := os.Stat(projectFile(rootDir)); !os.IsNotExist(err) {
		t.Errorf("no project file should be written without remember")
	}
}

func TestPromptDeclineRefuses(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	el := &fakeElicitor{results: []*mcpsdk.ElicitResult{{Action: "decline"}}}

	_, _, err := handleRun(context.Background(), nil, g, el, runInput{Command: []string{"echo", "hi"}})
	if err == nil {
		t.Fatalf("expected refusal on decline")
	}
	if !strings.Contains(err.Error(), "declined") {
		t.Errorf("err = %v, want a declined message", err)
	}
}

func TestPromptCancelRefuses(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	el := &fakeElicitor{results: []*mcpsdk.ElicitResult{{Action: "cancel"}}}

	_, _, err := handleRun(context.Background(), nil, g, el, runInput{Command: []string{"echo", "hi"}})
	if err == nil {
		t.Fatalf("expected refusal on cancel")
	}
}

func TestPromptAcceptRememberWritesRuleAndGoesLive(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	rootDir := t.TempDir()
	g := newTestGateAt(t, rootDir, "version: 1\n", false)
	el := &fakeElicitor{results: []*mcpsdk.ElicitResult{
		accept(map[string]any{"remember": true, "rule": "[echo]"}),
	}}

	_, out, err := handleRun(context.Background(), nil, g, el, runInput{Command: []string{"echo", "hi"}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", out.ExitCode)
	}

	raw, err := os.ReadFile(projectFile(rootDir))
	if err != nil {
		t.Fatalf("reading project file: %v", err)
	}
	if !strings.Contains(string(raw), "prefix: [echo]") {
		t.Errorf("project file missing remembered rule:\n%s", raw)
	}

	// The rule is live this session: a second echo must not re-prompt.
	el2 := &fakeElicitor{}
	_, _, err = handleRun(context.Background(), nil, g, el2, runInput{Command: []string{"echo", "again"}})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(el2.calls) != 0 {
		t.Errorf("second run prompted %d times, want 0 (rule should be live)", len(el2.calls))
	}
}

func TestPromptRememberEditedRule(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	rootDir := t.TempDir()
	g := newTestGateAt(t, rootDir, "version: 1\n", false)
	// The user edits the suggested rule to broaden it.
	el := &fakeElicitor{results: []*mcpsdk.ElicitResult{
		accept(map[string]any{"remember": true, "rule": "[echo, hi]"}),
	}}

	_, _, err := handleRun(context.Background(), nil, g, el, runInput{Command: []string{"echo", "hi"}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	raw, _ := os.ReadFile(projectFile(rootDir))
	if !strings.Contains(string(raw), "prefix: [echo, hi]") {
		t.Errorf("edited rule not persisted:\n%s", raw)
	}
}

func TestPromptDangerousEnvRefusedBeforePrompt(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	el := &fakeElicitor{results: []*mcpsdk.ElicitResult{accept(map[string]any{"remember": false})}}

	_, _, err := handleRun(context.Background(), nil, g, el, runInput{
		Command: []string{"echo", "hi"},
		Env:     map[string]string{"LD_PRELOAD": "evil.so"},
	})
	if err == nil {
		t.Fatalf("expected refusal for dangerous env on prompt path")
	}
	if !strings.Contains(err.Error(), "LD_PRELOAD") {
		t.Errorf("err = %v, want the offending var named", err)
	}
	if len(el.calls) != 0 {
		t.Errorf("prompted despite dangerous env; calls = %d, want 0", len(el.calls))
	}
}

func TestPromptDivergenceReloadMerge(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	rootDir := t.TempDir()
	g := newTestGateAt(t, rootDir, "version: 1\nallow:\n  - prefix: [make]\n", false)

	// Hand-edit the project file after load so persistence diverges.
	if err := os.WriteFile(projectFile(rootDir), []byte("version: 1\nallow:\n  - prefix: [make]\n  - prefix: [npm, ci]\n"), 0o600); err != nil {
		t.Fatalf("hand edit: %v", err)
	}

	el := &fakeElicitor{results: []*mcpsdk.ElicitResult{
		accept(map[string]any{"remember": true, "rule": "[echo]"}),
		accept(map[string]any{"choice": divergeReloadMerge}),
	}}

	_, _, err := handleRun(context.Background(), nil, g, el, runInput{Command: []string{"echo", "hi"}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if len(el.calls) != 2 {
		t.Fatalf("expected 2 prompts (approval + divergence), got %d", len(el.calls))
	}

	raw, _ := os.ReadFile(projectFile(rootDir))
	got := string(raw)
	for _, want := range []string{"prefix: [make]", "prefix: [npm, ci]", "prefix: [echo]"} {
		if !strings.Contains(got, want) {
			t.Errorf("reload-merge result missing %q:\n%s", want, got)
		}
	}
}

func TestPromptDivergenceSkip(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	rootDir := t.TempDir()
	g := newTestGateAt(t, rootDir, "version: 1\nallow:\n  - prefix: [make]\n", false)

	diverged := "version: 1\nallow:\n  - prefix: [make]\n  - prefix: [npm, ci]\n"
	if err := os.WriteFile(projectFile(rootDir), []byte(diverged), 0o600); err != nil {
		t.Fatalf("hand edit: %v", err)
	}

	el := &fakeElicitor{results: []*mcpsdk.ElicitResult{
		accept(map[string]any{"remember": true, "rule": "[echo]"}),
		accept(map[string]any{"choice": divergeSkip}),
	}}

	_, out, err := handleRun(context.Background(), nil, g, el, runInput{Command: []string{"echo", "hi"}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("command should still run on skip; ExitCode = %v", out.ExitCode)
	}

	// Skip leaves the on-disk file untouched.
	raw, _ := os.ReadFile(projectFile(rootDir))
	if string(raw) != diverged {
		t.Errorf("skip modified the file:\n%s", raw)
	}
}

func TestPromptRememberWriteErrorStillRuns(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	var buf bytes.Buffer
	old := stderr
	stderr = &buf
	defer func() { stderr = old }()

	rootDir := t.TempDir()
	g := newTestGateAt(t, rootDir, "", false)
	// Force a persistence failure by making the project path a directory, so the
	// atomic rename onto it fails. The command must still run, and a diagnostic
	// must be emitted.
	if err := os.Mkdir(projectFile(rootDir), 0o755); err != nil {
		t.Fatalf("seed project path as dir: %v", err)
	}

	el := &fakeElicitor{results: []*mcpsdk.ElicitResult{
		accept(map[string]any{"remember": true, "rule": "[echo]"}),
	}}
	_, out, err := handleRun(context.Background(), nil, g, el, runInput{Command: []string{"echo", "hi"}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("command should run despite write failure; ExitCode = %v", out.ExitCode)
	}
	if !strings.Contains(buf.String(), "remember") {
		t.Errorf("expected a persistence diagnostic on stderr, got %q", buf.String())
	}
}
