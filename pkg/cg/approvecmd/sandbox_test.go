package approvecmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const checkSandboxRules = `version: 2
sandboxes:
  go:
    seatbelt:
      profile: go.sb
      already_sandboxed: refuse
      proxy: http://localhost:3128
allow:
  - prefix: [echo, hi]
    sandbox: go
  - prefix: [echo]
`

func writeProfile(t *testing.T, root, text string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, "go.sb"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckReportsTheSandbox(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "(version 1)\n(allow default)\n")

	out, err := runCheck(t, root, checkSandboxRules, "-o", "json", "--", "echo", "hi", "there")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}

	var res checkResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decoding %q: %v", out, err)
	}
	if res.Sandbox == nil || res.Sandbox.Name != "go" || res.Sandbox.Platform != runtime.GOOS {
		t.Fatalf("sandbox = %+v, want the deciding rule's sandbox", res.Sandbox)
	}
	if res.Rule == nil || res.Rule.Sandbox != "go" {
		t.Errorf("rule = %+v, want it to carry its sandbox", res.Rule)
	}

	if runtime.GOOS == "darwin" {
		if !res.Sandbox.Applicable || res.Sandbox.Class == "" || res.Sandbox.AlreadySandboxed != "refuse" || res.Sandbox.Proxy != "http://localhost:3128" {
			t.Errorf("sandbox = %+v, want the seatbelt block with its probe class", res.Sandbox)
		}
		if res.Sandbox.Profile != filepath.Join(root, "go.sb") && !strings.HasSuffix(res.Sandbox.Profile, "go.sb") {
			t.Errorf("profile = %q, want the resolved profile path", res.Sandbox.Profile)
		}
	} else if res.Sandbox.Applicable {
		t.Errorf("sandbox = %+v, want no block for a platform without Seatbelt", res.Sandbox)
	}

	out, err = runCheck(t, root, "", "--", "echo", "hi", "there")
	if exitCode(t, err) != 0 || !strings.Contains(out, "  sandbox: go") {
		t.Errorf("text output = %q, want a sandbox line", out)
	}

	out, err = runCheck(t, root, "", "-o", "json", "--", "echo", "bye")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d; out = %s", exitCode(t, err), out)
	}

	var plain checkResult
	if err := json.Unmarshal([]byte(out), &plain); err != nil {
		t.Fatal(err)
	}
	if plain.Sandbox != nil {
		t.Errorf("sandbox = %+v, want none for the broader rule with no sandbox", plain.Sandbox)
	}
}

func TestCheckReportsTheSandboxUnderAllowAll(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "(version 1)\n(allow default)\n")

	out, err := runCheck(t, root, strings.Replace(checkSandboxRules, "version: 2\n", "version: 2\nmode: allow-all\n", 1), "-o", "json", "--", "echo", "hi")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d; out = %s", exitCode(t, err), out)
	}

	var res checkResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Decision != "run" || res.Sandbox == nil || res.Sandbox.Name != "go" {
		t.Errorf("result = %+v, want a run under sandbox go in allow-all mode", res)
	}
}

func TestLintReportsSandboxProblems(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("profiles are read and compiled on darwin only, not %s", runtime.GOOS)
	}

	t.Run("a missing profile is an issue", func(t *testing.T) {
		root := t.TempDir()

		out, err := runLint(t, root, checkSandboxRules)
		if exitCode(t, err) != 1 || !strings.Contains(out, "reading profile") {
			t.Errorf("exit = %d, out = %q, want the merged pass to report the missing profile", exitCode(t, err), out)
		}
	})

	t.Run("a profile that does not compile is an issue naming the file", func(t *testing.T) {
		root := t.TempDir()
		writeProfile(t, root, "(version 1)\n(not-a-form")

		out, err := runLint(t, root, checkSandboxRules)
		if exitCode(t, err) != 1 || !strings.Contains(out, "does not compile") || !strings.Contains(out, ".cg.yaml") {
			t.Errorf("exit = %d, out = %q, want the compile error and the defining file", exitCode(t, err), out)
		}
	})

	t.Run("a healthy profile is clean", func(t *testing.T) {
		root := t.TempDir()
		writeProfile(t, root, "(version 1)\n(allow default)\n")

		out, err := runLint(t, root, checkSandboxRules)
		if exitCode(t, err) != 0 {
			t.Errorf("exit = %d, out = %q, want clean", exitCode(t, err), out)
		}
	})
}

func TestLintGlobalFileAloneReportsRelativeProfile(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "cg")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}

	global := "version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: go.sb, already_sandboxed: run}\n"
	if err := os.WriteFile(filepath.Join(globalDir, "approve.yaml"), []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	var buf bytes.Buffer
	c := NewLintCommand()
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs(nil)

	err := c.Execute()
	if exitCode(t, err) != 1 || !strings.Contains(buf.String(), "relative profile path") {
		t.Errorf("exit = %d, out = %q, want the global file's relative profile reported on its own", exitCode(t, err), buf.String())
	}
}
