package cg

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestRunMetaFinished(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	seedRunDir(t, "AAAAAA", &Meta{ID: "AAAAAA", Command: []string{"echo", "hi"}, ExitCode: 0, DurationMs: 12, StdoutLines: 1})

	res, err := RunMeta("AAAAAA")
	if err != nil {
		t.Fatalf("RunMeta: %v", err)
	}
	if res.State != RunStateFinished {
		t.Errorf("state = %q, want finished", res.State)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Errorf("exit_code = %v, want 0", res.ExitCode)
	}
	if res.DurationMs == nil || *res.DurationMs != 12 {
		t.Errorf("duration_ms = %v, want 12", res.DurationMs)
	}
}

func TestRunMetaRunning(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	seedRunDir(t, "AAAAAA", nil)

	res, err := RunMeta("AAAAAA")
	if err != nil {
		t.Fatalf("RunMeta: %v", err)
	}
	if res.State != RunStateRunning {
		t.Errorf("state = %q, want running", res.State)
	}
	if res.ExitCode != nil {
		t.Errorf("exit_code = %v, want nil for running run", res.ExitCode)
	}
}

func TestRunMetaFailed(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir := seedRunDir(t, "AAAAAA", nil)
	if err := WriteStartDebug(dir, &StartDebug{Command: []string{"nope"}, StartError: "boom"}); err != nil {
		t.Fatalf("WriteStartDebug: %v", err)
	}

	res, err := RunMeta("AAAAAA")
	if err != nil {
		t.Fatalf("RunMeta: %v", err)
	}
	if res.State != RunStateFailed {
		t.Errorf("state = %q, want failed", res.State)
	}
	if res.Debug == nil || res.Debug.StartError != "boom" {
		t.Errorf("debug = %+v, want start_error boom", res.Debug)
	}
}

func TestRunMetaUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := RunMeta("ZZZZZZ"); !errors.Is(err, ErrUnknownRunID) {
		t.Errorf("err = %v, want ErrUnknownRunID", err)
	}
}

func TestMetaCommandUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	stdout, stderr, err := runCgSplit("meta", "ABCDEF")
	assertExitCode1(t, err)
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if stderr != "unknown run id: ABCDEF\n" {
		t.Errorf("stderr = %q, want unknown-id line", stderr)
	}
}

func TestMetaCommandFinishedJSON(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	seedRunDir(t, "AAAAAA", &Meta{ID: "AAAAAA", Command: []string{"echo", "hi"}, StartedAt: time.Unix(0, 0).UTC()})

	stdout, _, err := runCgSplit("meta", "AAAAAA")
	if err != nil {
		t.Fatalf("meta command: %v", err)
	}
	var res MetaResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("unmarshal stdout %q: %v", stdout, err)
	}
	if res.ID != "AAAAAA" || res.State != RunStateFinished {
		t.Errorf("res = %+v, want id AAAAAA state finished", res)
	}
}
