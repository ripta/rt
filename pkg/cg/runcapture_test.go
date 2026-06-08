package cg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitDone(t *testing.T, run *CaptureRun, d time.Duration) {
	t.Helper()
	select {
	case <-run.Done:
	case <-time.After(d):
		t.Fatalf("run %s did not finish within %s", run.ID, d)
	}
}

func TestRunCaptureEcho(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	run, err := RunCapture([]string{"echo", "hello"}, "", nil)
	if err != nil {
		t.Fatalf("RunCapture: %v", err)
	}
	waitDone(t, run, 5*time.Second)

	out, err := os.ReadFile(filepath.Join(run.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("stdout = %q, want %q", out, "hello\n")
	}

	meta, err := ReadMeta(run.Dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if meta.ID != run.ID {
		t.Errorf("meta.ID = %q, want %q", meta.ID, run.ID)
	}
	if meta.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", meta.ExitCode)
	}
	if meta.StdoutLines != 1 {
		t.Errorf("StdoutLines = %d, want 1", meta.StdoutLines)
	}
	if meta.StderrLines != 0 {
		t.Errorf("StderrLines = %d, want 0", meta.StderrLines)
	}
	if meta.Signal != nil {
		t.Errorf("Signal = %v, want nil", *meta.Signal)
	}
	if meta.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want >= 0", meta.DurationMs)
	}
}

func TestRunCaptureNonZeroExit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	run, err := RunCapture([]string{"sh", "-c", "exit 3"}, "", nil)
	if err != nil {
		t.Fatalf("RunCapture: %v", err)
	}
	waitDone(t, run, 5*time.Second)

	meta, err := ReadMeta(run.Dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if meta.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", meta.ExitCode)
	}
}

func TestRunCaptureStartError(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, err := RunCapture([]string{"this-binary-does-not-exist-zzzz"}, "", nil)
	if err == nil {
		t.Fatalf("RunCapture: expected error, got nil")
	}

	entries, _ := os.ReadDir(CaptureRoot())
	for _, e := range entries {
		if e.IsDir() && isValidRunID(e.Name()) {
			t.Errorf("leftover capture dir after start failure: %s", e.Name())
		}
	}
}

func TestRunCaptureEmptyCommand(t *testing.T) {
	if _, err := RunCapture(nil, "", nil); err == nil {
		t.Fatalf("expected error for empty command")
	}
}

func TestRunCaptureEnv(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	run, err := RunCapture([]string{"sh", "-c", "echo $CG_TEST_KEY"}, "", map[string]string{"CG_TEST_KEY": "from-mcp"})
	if err != nil {
		t.Fatalf("RunCapture: %v", err)
	}
	waitDone(t, run, 5*time.Second)

	out, err := os.ReadFile(filepath.Join(run.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if strings.TrimSpace(string(out)) != "from-mcp" {
		t.Errorf("stdout = %q, want %q", out, "from-mcp\n")
	}
}

func TestRunCaptureEnvOverride(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("CG_OVERRIDE_ME", "parent-value")

	run, err := RunCapture([]string{"sh", "-c", "echo $CG_OVERRIDE_ME"}, "", map[string]string{"CG_OVERRIDE_ME": "child-value"})
	if err != nil {
		t.Fatalf("RunCapture: %v", err)
	}
	waitDone(t, run, 5*time.Second)

	out, err := os.ReadFile(filepath.Join(run.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if strings.TrimSpace(string(out)) != "child-value" {
		t.Errorf("stdout = %q, want %q", out, "child-value\n")
	}
}

func TestRunCaptureCwd(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	dir := t.TempDir()
	run, err := RunCapture([]string{"pwd"}, dir, nil)
	if err != nil {
		t.Fatalf("RunCapture: %v", err)
	}
	waitDone(t, run, 5*time.Second)

	out, err := os.ReadFile(filepath.Join(run.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	got, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("EvalSymlinks(stdout): %v", err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(want): %v", err)
	}
	if got != want {
		t.Errorf("pwd = %q, want %q", got, want)
	}
}

func TestRunCaptureStderr(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	run, err := RunCapture([]string{"sh", "-c", "echo only-err >&2"}, "", nil)
	if err != nil {
		t.Fatalf("RunCapture: %v", err)
	}
	waitDone(t, run, 5*time.Second)

	stderr, err := os.ReadFile(filepath.Join(run.Dir, "stderr"))
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if string(stderr) != "only-err\n" {
		t.Errorf("stderr = %q, want %q", stderr, "only-err\n")
	}

	meta, err := ReadMeta(run.Dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if meta.StderrLines != 1 || meta.StdoutLines != 0 {
		t.Errorf("lines: out=%d err=%d, want out=0 err=1", meta.StdoutLines, meta.StderrLines)
	}
}
