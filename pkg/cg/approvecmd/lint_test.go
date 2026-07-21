package approvecmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runLint executes `cg lint` with args, isolated from the real user config the
// same way runCheck is: HOME points at an empty temp dir, and
// CLAUDE_PROJECT_DIR points at root. projectYAML, when non-empty, is written
// to root/.cg.yaml before the command runs.
func runLint(t *testing.T, root, projectYAML string, args ...string) (stdout string, err error) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	if projectYAML != "" {
		if writeErr := os.WriteFile(filepath.Join(root, ".cg.yaml"), []byte(projectYAML), 0o644); writeErr != nil {
			t.Fatalf("write .cg.yaml: %v", writeErr)
		}
	}
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	var buf bytes.Buffer
	c := NewLintCommand()
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs(args)

	err = c.Execute()
	return buf.String(), err
}

func TestLintCleanConfig(t *testing.T) {
	root := t.TempDir()
	out, err := runLint(t, root, "version: 1\nallow:\n  - prefix: [git, status]\n")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; err = %v", exitCode(t, err), err)
	}
	if !strings.Contains(out, "(ok)") {
		t.Errorf("stdout = %q, want it to report the project layer ok", out)
	}
}

func TestLintMissingFiles(t *testing.T) {
	root := t.TempDir()
	out, err := runLint(t, root, "")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; err = %v", exitCode(t, err), err)
	}
	if !strings.Contains(out, "(absent)") {
		t.Errorf("stdout = %q, want it to report absent layers", out)
	}
}

func TestLintProjectBrokenReportsEveryError(t *testing.T) {
	root := t.TempDir()
	out, err := runLint(t, root, "version: 1\ndeny:\n  - message: no kind here\n  - exact: [git]\n    prefix: [git]\n")
	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; err = %v", exitCode(t, err), err)
	}
	if !strings.Contains(out, "no rule-kind key") {
		t.Errorf("stdout = %q, want the first rule's error", out)
	}
	if !strings.Contains(out, "more than one rule-kind key") {
		t.Errorf("stdout = %q, want the second rule's error", out)
	}
}

func TestLintBothLayersBrokenReportsBoth(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "cg")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir global config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "approve.yaml"), []byte("version: 1\nmode: loose\n"), 0o644); err != nil {
		t.Fatalf("write global approve.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cg.yaml"), []byte("version: 1\nallow:\n  - prefix: [make]\n    message: not allowed here\n"), 0o644); err != nil {
		t.Fatalf("write .cg.yaml: %v", err)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	var buf bytes.Buffer
	c := NewLintCommand()
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs(nil)
	err := c.Execute()
	out := buf.String()

	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; err = %v", exitCode(t, err), err)
	}
	if !strings.Contains(out, "unknown mode") {
		t.Errorf("stdout = %q, want the global layer's error", out)
	}
	if !strings.Contains(out, "message is not valid on an allow rule") {
		t.Errorf("stdout = %q, want the project layer's error", out)
	}
}

func TestLintOutputJSON(t *testing.T) {
	root := t.TempDir()
	out, err := runLint(t, root, "version: 1\ndeny:\n  - message: no kind here\n", "--output", "json")
	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; err = %v", exitCode(t, err), err)
	}

	var res lintResult
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("unmarshalling %q: %v", out, jsonErr)
	}
	if !res.Project.Present {
		t.Errorf("project.present = false, want true")
	}
	if len(res.Project.Issues) != 1 {
		t.Errorf("project.issues = %v, want exactly one issue", res.Project.Issues)
	}
	if len(res.Global.Issues) != 0 {
		t.Errorf("global.issues = %v, want none", res.Global.Issues)
	}
}

func TestLintProjectConfigFlag(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alt.yaml"), []byte("version: 1\ndeny:\n  - message: no kind here\n"), 0o644); err != nil {
		t.Fatalf("write alt.yaml: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	var buf bytes.Buffer
	c := NewLintCommand()
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs([]string{"--project-config", "alt.yaml"})
	err := c.Execute()

	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; err = %v", exitCode(t, err), err)
	}
	if !strings.Contains(buf.String(), "alt.yaml") {
		t.Errorf("stdout = %q, want it to name alt.yaml", buf.String())
	}
}
