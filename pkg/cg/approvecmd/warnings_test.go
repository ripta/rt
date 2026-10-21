package approvecmd

import (
	"encoding/json"
	"strings"
	"testing"
)

const danglingRules = "version: 1\nallow:\n  - wrapper: [sudo, -u]\n  - prefix: [echo]\n"

func TestLintWarningsAloneExitZero(t *testing.T) {
	out, err := runLint(t, t.TempDir(), danglingRules)
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}

	for _, want := range []string{"warning: allow wrapper [sudo, -u] ends on a bare flag", "merged: (ok)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestLintWarningsJSON(t *testing.T) {
	out, err := runLint(t, t.TempDir(), danglingRules, "--output", "json")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}

	var res lintResult
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("unmarshalling %q: %v", out, jsonErr)
	}
	if len(res.Project.Warnings) != 1 || len(res.Project.Issues) != 0 {
		t.Errorf("project = %+v, want one warning and no issues", res.Project)
	}
	if res.Merged.Skipped || len(res.Merged.Warnings) != 0 {
		t.Errorf("merged = %+v, want a clean pass", res.Merged)
	}
}

func TestLintMergedPassReportsCoveredWrapper(t *testing.T) {
	rules := "version: 1\nallow:\n  - prefix: [/usr/bin/nice]\n  - wrapper: [nice, -n, \"19\"]\n"

	out, err := runLint(t, t.TempDir(), rules)
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}
	if !strings.Contains(out, "merged:\n  warning: allow prefix [/usr/bin/nice] already allows every command") {
		t.Errorf("output missing the merged warning:\n%s", out)
	}
}

func TestLintErrorsStillExitOneAndSkipMergedPass(t *testing.T) {
	rules := "version: 1\nallow:\n  - wrapper: [sudo, -u]\n  - prefix: [make]\n    message: not here\n"

	out, err := runLint(t, t.TempDir(), rules)
	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; out = %s", exitCode(t, err), out)
	}
	if !strings.Contains(out, "merged: skipped: a layer failed to parse") {
		t.Errorf("output missing the skip note:\n%s", out)
	}
	if strings.Contains(out, "warning:") {
		t.Errorf("a layer that failed to parse should report no warnings:\n%s", out)
	}
}

func TestLintSkippedMergedPassJSON(t *testing.T) {
	out, err := runLint(t, t.TempDir(), "version: 1\nmode: loose\n", "-o", "json")
	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; out = %s", exitCode(t, err), out)
	}

	var res lintResult
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("unmarshalling %q: %v", out, jsonErr)
	}
	if !res.Merged.Skipped || res.Merged.Note == "" {
		t.Errorf("merged = %+v, want a skipped pass with a note", res.Merged)
	}
}

func TestLintDenyDashWarns(t *testing.T) {
	out, err := runLint(t, t.TempDir(), "version: 1\ndeny:\n  - prefix: [-n]\nrestrict:\n  - exact: [-x]\n")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}
	if strings.Count(out, "can never match a command's program") != 2 {
		t.Errorf("output should warn on both the deny and the restrict rule:\n%s", out)
	}
}

func TestCheckWarningsDoNotChangeTheExitCode(t *testing.T) {
	out, err := runCheck(t, t.TempDir(), danglingRules, "--", "echo", "hi")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}
	if !strings.HasPrefix(out, "run: echo hi") || !strings.Contains(out, "warning: allow wrapper [sudo, -u]") {
		t.Errorf("output should lead with the verdict and carry the warning:\n%s", out)
	}

	out, err = runCheck(t, t.TempDir(), danglingRules, "--", "unknown-tool")
	if exitCode(t, err) != 3 {
		t.Fatalf("exit code = %d, want 3; out = %s", exitCode(t, err), out)
	}
}

func TestCheckWarningsJSON(t *testing.T) {
	out, err := runCheck(t, t.TempDir(), danglingRules, "--output", "json", "--", "echo", "hi")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}

	var res checkResult
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("unmarshalling %q: %v", out, jsonErr)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "wrapper [sudo, -u]") {
		t.Errorf("warnings = %v, want the dangling flag warning", res.Warnings)
	}
}

func TestCheckShellWarningsOnTheAggregate(t *testing.T) {
	out, err := runCheck(t, t.TempDir(), danglingRules, "--shell", "--output", "json", "--", "echo hi && echo there")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}

	var res checkShellResult
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("unmarshalling %q: %v", out, jsonErr)
	}
	if len(res.Warnings) != 1 {
		t.Errorf("warnings = %v, want one on the aggregate", res.Warnings)
	}
	for _, c := range res.Commands {
		if len(c.Warnings) != 0 {
			t.Errorf("command carries warnings %v, want them only on the aggregate", c.Warnings)
		}
	}
}

func TestCheckWithoutWarningsOmitsTheField(t *testing.T) {
	out, err := runCheck(t, t.TempDir(), "version: 1\nallow:\n  - prefix: [echo]\n", "--output", "json", "--", "echo", "hi")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}
	if strings.Contains(out, "warnings") {
		t.Errorf("output should omit warnings:\n%s", out)
	}
}
