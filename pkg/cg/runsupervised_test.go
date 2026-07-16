package cg

import (
	"errors"
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

func TestRunSupervisedEcho(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	run, err := RunSupervised([]string{"echo", "hello"}, nil, "", nil)
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
	}
	waitDone(t, run, 5*time.Second)

	out, err := os.ReadFile(filepath.Join(run.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("stdout = %q, want %q", out, "hello\n")
	}

	// Done closes only after the supervisor exits, and the supervisor exits
	// only after meta.json is written; the read must succeed immediately.
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

	if _, err := ReadPidFile(run.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadPidFile after finish: err = %v, want ErrNotExist", err)
	}
	if _, err := os.Stat(filepath.Join(run.Dir, LockFilename)); err != nil {
		t.Errorf("lock file: %v", err)
	}
}

func TestRunSupervisedStartInfo(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	run, err := RunSupervised([]string{"sh", "-c", "sleep 0.3; echo done"}, nil, "", nil)
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
	}

	// While the child runs, start.json carries the command and start time so
	// `cg ls` and cg_list can surface an in-flight run.
	si, err := ReadStartInfo(run.Dir)
	if err != nil {
		t.Fatalf("ReadStartInfo while running: %v", err)
	}
	if strings.Join(si.Command, " ") != "sh -c sleep 0.3; echo done" {
		t.Errorf("start command = %q, want the launched argv", si.Command)
	}
	if si.StartedAt.IsZero() {
		t.Error("start time is zero")
	}

	waitDone(t, run, 5*time.Second)

	// Once meta.json supersedes it, start.json is removed.
	if _, err := ReadStartInfo(run.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadStartInfo after finish: err = %v, want ErrNotExist", err)
	}
}

func TestRunSupervisedNonZeroExit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	run, err := RunSupervised([]string{"sh", "-c", "exit 3"}, nil, "", nil)
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
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

func TestRunSupervisedStartError(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, err := RunSupervised([]string{"this-binary-does-not-exist-zzzz"}, nil, "", nil)
	if err == nil {
		t.Fatalf("RunSupervised: expected error, got nil")
	}

	var sf *StartFailure
	if !errors.As(err, &sf) {
		t.Fatalf("expected *StartFailure, got %T: %v", err, err)
	}
	if sf.RunID == "" {
		t.Error("StartFailure.RunID is empty")
	}

	// The capture dir is kept so debug.json can be inspected.
	dbg, dbgErr := ReadStartDebug(sf.Dir)
	if dbgErr != nil {
		t.Fatalf("ReadStartDebug: %v", dbgErr)
	}
	if dbg.StartError == "" {
		t.Error("StartDebug.StartError is empty")
	}

	if _, err := ReadMeta(sf.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadMeta: err = %v, want ErrNotExist", err)
	}
}

func TestRunSupervisedEmptyCommand(t *testing.T) {
	if _, err := RunSupervised(nil, nil, "", nil); err == nil {
		t.Fatalf("expected error for empty command")
	}
}

func TestRunSupervisedEnvOverride(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("CG_OVERRIDE_ME", "parent-value")

	run, err := RunSupervised([]string{"sh", "-c", "echo $CG_OVERRIDE_ME"}, nil, "", map[string]string{"CG_OVERRIDE_ME": "child-value"})
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
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

func TestRunSupervisedCwd(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	dir := t.TempDir()
	run, err := RunSupervised([]string{"pwd"}, nil, dir, nil)
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
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
