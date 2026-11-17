package approvecmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inertRules = "version: 1\nallow:\n  - prefix: [~no-such-user-for-cg-test/bin/tool]\n"

func TestCheckReportsInertTildeRuleAgainstItsFile(t *testing.T) {
	root := t.TempDir()

	out, err := runCheck(t, root, inertRules, "--", "tool")
	if exitCode(t, err) != 3 {
		t.Fatalf("exit code = %d, want 3 (prompt); err = %v", exitCode(t, err), err)
	}

	for _, want := range []string{"warning:", filepath.Join(root, ".cg.yaml"), "~no-such-user-for-cg-test/bin/tool", "matches no command"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

func TestCheckFailsTheLoadForAnUnresolvableTildeDeny(t *testing.T) {
	root := t.TempDir()

	out, err := runCheck(t, root, "version: 1\ndeny:\n  - prefix: [~no-such-user-for-cg-test/bin/tool]\n", "--", "tool")
	if exitCode(t, err) != 2 {
		t.Fatalf("exit code = %d, want 2; err = %v", exitCode(t, err), err)
	}
	if !strings.Contains(out, ".cg.yaml") || !strings.Contains(out, "~no-such-user-for-cg-test/bin/tool") {
		t.Errorf("output %q should name the file and token", out)
	}
}

func TestCheckExpandsATildeRuleFromHome(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()

	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(bin, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, ".cg.yaml"), []byte("version: 1\nallow:\n  - prefix: [~/bin/tool]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	var buf bytes.Buffer
	c := NewCheckCommand()
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs([]string{"--", tool})

	err := c.Execute()
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), buf.String())
	}
	if !strings.Contains(buf.String(), "[~/bin/tool]") {
		t.Errorf("output %q should show the file's spelling", buf.String())
	}
}

func TestLintReportsInertTildeRuleAsAWarning(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".cg.yaml"), []byte(inertRules), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	var buf bytes.Buffer
	c := NewLintCommand()
	c.SetOut(&buf)
	c.SetErr(&buf)

	if err := c.Execute(); exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), buf.String())
	}
	if !strings.Contains(buf.String(), "warning:") || !strings.Contains(buf.String(), ".cg.yaml") {
		t.Errorf("output %q should warn against the project file", buf.String())
	}
}

func TestProjectConfigAcceptsAbsoluteAndTildeValues(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "rules.yaml"), []byte("version: 1\nallow:\n  - prefix: [git, status]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	for _, value := range []string{"~/rules.yaml", filepath.Join(home, "rules.yaml")} {
		var buf bytes.Buffer
		c := NewCheckCommand()
		c.SetOut(&buf)
		c.SetErr(&buf)
		c.SetArgs([]string{"--project-config", value, "--", "git", "status"})

		if err := c.Execute(); exitCode(t, err) != 0 {
			t.Errorf("--project-config %s: exit code = %d; out = %s", value, exitCode(t, err), buf.String())
		}
	}
}

func TestProjectConfigUnresolvableUserFails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	var buf bytes.Buffer
	c := NewLintCommand()
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs([]string{"--project-config", "~no-such-user-for-cg-test/rules.yaml"})

	err := c.Execute()
	if exitCode(t, err) != 2 || !strings.Contains(buf.String(), "~no-such-user-for-cg-test/rules.yaml") {
		t.Errorf("exit code = %d, out = %s; want 2 naming the token", exitCode(t, err), buf.String())
	}
}
