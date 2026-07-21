package approvecmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/model"
)

// runCheck executes `cg check` with args, isolated from the real user config:
// HOME points at an empty temp dir so DefaultGlobalPath finds nothing, and
// CLAUDE_PROJECT_DIR points at root so DefaultProjectRoot doesn't fall back to
// the test binary's working directory. projectYAML, when non-empty, is written
// to root/.cg.yaml before the command runs.
func runCheck(t *testing.T, root, projectYAML string, args ...string) (stdout string, err error) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	if projectYAML != "" {
		if writeErr := os.WriteFile(filepath.Join(root, ".cg.yaml"), []byte(projectYAML), 0o644); writeErr != nil {
			t.Fatalf("write .cg.yaml: %v", writeErr)
		}
	}
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	var buf bytes.Buffer
	c := NewCheckCommand()
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs(args)

	err = c.Execute()
	return buf.String(), err
}

// exitCode extracts the code cmd/cg/main.go would exit with for err.
func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var exitErr *model.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *model.ExitError, got %T: %v", err, err)
	}
	return exitErr.Code
}

func TestCheckAllowRuns(t *testing.T) {
	root := t.TempDir()
	out, err := runCheck(t, root, "version: 1\nallow:\n  - prefix: [git, status]\n", "--", "git", "status")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; err = %v", exitCode(t, err), err)
	}
	if !strings.HasPrefix(out, "run: git status") {
		t.Errorf("stdout = %q, want it to start with %q", out, "run: git status")
	}
}

func TestCheckDenyRefuses(t *testing.T) {
	root := t.TempDir()
	out, err := runCheck(t, root, "version: 1\ndeny:\n  - prefix: [rm, -rf]\n    message: delete specific paths instead\n", "--", "rm", "-rf", "x")
	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; err = %v", exitCode(t, err), err)
	}
	if !strings.Contains(out, "rule: deny") {
		t.Errorf("stdout = %q, want it to name the deny rule", out)
	}
	if !strings.Contains(out, "delete specific paths instead") {
		t.Errorf("stdout = %q, want the rule message", out)
	}
}

func TestCheckBuiltinDenyRefuses(t *testing.T) {
	root := t.TempDir()
	_, err := runCheck(t, root, "version: 1\n", "--", "sh", "-c", "echo hi")
	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; err = %v", exitCode(t, err), err)
	}
}

func TestCheckRestrictRefuses(t *testing.T) {
	root := t.TempDir()
	out, err := runCheck(t, root, "version: 1\nrestrict:\n  - prefix: [echo]\n    message: only echo hi is permitted here\n", "--", "echo", "bye")
	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; err = %v", exitCode(t, err), err)
	}
	if !strings.Contains(out, "rule: restrict") {
		t.Errorf("stdout = %q, want it to name the restrict rule", out)
	}
}

func TestCheckUnmatchedPrompts(t *testing.T) {
	root := t.TempDir()
	out, err := runCheck(t, root, "version: 1\n", "--", "git", "status")
	if exitCode(t, err) != 3 {
		t.Fatalf("exit code = %d, want 3; err = %v", exitCode(t, err), err)
	}
	if !strings.HasPrefix(out, "prompt: git status") {
		t.Errorf("stdout = %q, want it to start with %q", out, "prompt: git status")
	}
}

func TestCheckEnvOverrideDowngradesAllow(t *testing.T) {
	root := t.TempDir()
	out, err := runCheck(t, root, "version: 1\nallow:\n  - prefix: [make]\n", "--env", "LD_PRELOAD=evil.so", "--", "make")
	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; err = %v", exitCode(t, err), err)
	}
	if !strings.Contains(out, "LD_PRELOAD") {
		t.Errorf("stdout = %q, want the offending var named", out)
	}
	if !strings.Contains(out, "permit_unsafe_envs") {
		t.Errorf("stdout = %q, want a hint about permit_unsafe_envs", out)
	}
}

func TestCheckEnvOverrideRefusesUnmatched(t *testing.T) {
	root := t.TempDir()
	out, err := runCheck(t, root, "version: 1\n", "--env", "LD_PRELOAD=evil.so", "--", "mystery-tool")
	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; err = %v", exitCode(t, err), err)
	}
	if !strings.Contains(out, "LD_PRELOAD") {
		t.Errorf("stdout = %q, want the offending var named", out)
	}
}

func TestCheckMissingCommandIsUsageError(t *testing.T) {
	root := t.TempDir()
	_, err := runCheck(t, root, "version: 1\n")
	if exitCode(t, err) != 2 {
		t.Fatalf("exit code = %d, want 2; err = %v", exitCode(t, err), err)
	}
}

func TestCheckMalformedConfigIsUsageError(t *testing.T) {
	root := t.TempDir()
	_, err := runCheck(t, root, "version: 1\ndeny:\n  - message: nothing to match\n", "--", "git", "status")
	if exitCode(t, err) != 2 {
		t.Fatalf("exit code = %d, want 2; err = %v", exitCode(t, err), err)
	}
}

func TestCheckOutputJSON(t *testing.T) {
	root := t.TempDir()
	out, err := runCheck(t, root, "version: 1\nallow:\n  - prefix: [git, status]\n", "--output", "json", "--", "git", "status")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; err = %v", exitCode(t, err), err)
	}

	var res checkResult
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("unmarshalling %q: %v", out, jsonErr)
	}
	if res.Decision != "run" {
		t.Errorf("decision = %q, want %q", res.Decision, "run")
	}
	if res.Section != "allow" {
		t.Errorf("section = %q, want %q", res.Section, "allow")
	}
	if res.Rule == nil || res.Rule.Kind != "prefix" {
		t.Errorf("rule = %+v, want a prefix rule", res.Rule)
	}
}

func TestCheckProjectConfigFlag(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alt.yaml"), []byte("version: 1\nallow:\n  - prefix: [git, status]\n"), 0o644); err != nil {
		t.Fatalf("write alt.yaml: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	var buf bytes.Buffer
	c := NewCheckCommand()
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs([]string{"--project-config", "alt.yaml", "--", "git", "status"})

	err := c.Execute()
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; err = %v", exitCode(t, err), err)
	}
}
