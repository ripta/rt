package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/approve"
)

// newTestGate builds a gate from project YAML written under a temp project root.
// The global layer is pointed at a nonexistent path so the test never reads the
// real ~/.config/cg/approve.yaml.
func newTestGate(t *testing.T, projectYAML string, blindly bool) *gate {
	t.Helper()

	root := t.TempDir()
	if projectYAML != "" {
		dir := filepath.Join(root, ".cg")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "approve.yaml"), []byte(projectYAML), 0o644); err != nil {
			t.Fatalf("write approve.yaml: %v", err)
		}
	}

	store, err := approve.Load(approve.LoadOptions{
		GlobalPath:  filepath.Join(root, "no-global.yaml"),
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatalf("approve.Load: %v", err)
	}
	return &gate{store: store, blindlyAllow: blindly}
}

func TestGateAllowRuns(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\nallow:\n  - prefix: [echo]\n", false)
	_, out, err := handleRun(context.Background(), nil, g, false, runInput{
		Command: []string{"echo", "hi"},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", out.ExitCode)
	}
}

func TestGateDenyRefusesWithMessage(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\ndeny:\n  - prefix: [rm, -rf]\n    message: delete specific paths instead\n", false)
	_, _, err := handleRun(context.Background(), nil, g, false, runInput{
		Command: []string{"rm", "-rf", "x"},
	})
	if err == nil {
		t.Fatalf("expected refusal for denied command")
	}
	if !strings.Contains(err.Error(), "delete specific paths instead") {
		t.Errorf("err = %v, want the rule message surfaced", err)
	}
}

func TestGateBuiltinDenyRefuses(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	_, _, err := handleRun(context.Background(), nil, g, false, runInput{
		Command: []string{"sh", "-c", "echo hi"},
	})
	if err == nil {
		t.Fatalf("expected built-in deny to refuse sh")
	}
}

func TestGateBlindlyAllowBypass(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	// sh is in the built-in deny set; --blindly-allow runs it anyway.
	g := newTestGate(t, "version: 1\n", true)
	_, out, err := handleRun(context.Background(), nil, g, false, runInput{
		Command: []string{"sh", "-c", "echo bypassed"},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !strings.Contains(out.StdoutExcerpt, "bypassed") {
		t.Errorf("StdoutExcerpt = %q, want to contain %q", out.StdoutExcerpt, "bypassed")
	}
}

func TestGateFailsClosedOnUnmatched(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	_, _, err := handleRun(context.Background(), nil, g, false, runInput{
		Command: []string{"echo", "hi"},
	})
	if err == nil {
		t.Fatalf("expected fail-closed refusal for unmatched command")
	}
	if !strings.Contains(err.Error(), "--blindly-allow") {
		t.Errorf("err = %v, want a hint about --blindly-allow", err)
	}
}

func TestGateEnvGateRefuses(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\nallow:\n  - prefix: [echo]\n", false)
	_, _, err := handleRun(context.Background(), nil, g, false, runInput{
		Command: []string{"echo", "hi"},
		Env:     map[string]string{"LD_PRELOAD": "evil.so"},
	})
	if err == nil {
		t.Fatalf("expected env-gate refusal")
	}
	if !strings.Contains(err.Error(), "LD_PRELOAD") {
		t.Errorf("err = %v, want the offending var named", err)
	}
}

func TestGateEnvGatePermitted(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\nallow:\n  - prefix: [echo]\n    permit_unsafe_envs: [PATH]\n", false)
	_, out, err := handleRun(context.Background(), nil, g, false, runInput{
		Command: []string{"echo", "hi"},
		Env:     map[string]string{"PATH": os.Getenv("PATH")},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", out.ExitCode)
	}
}

func TestGateAllowAllPassesEnv(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	// allow-all short-circuits to run and does not apply the env gate.
	g := newTestGate(t, "version: 1\nmode: allow-all\n", false)
	_, out, err := handleRun(context.Background(), nil, g, false, runInput{
		Command: []string{"echo", "hi"},
		Env:     map[string]string{"PATH": os.Getenv("PATH")},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", out.ExitCode)
	}
}
