package model

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitRunAlreadyFinished(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}}, ExitCode: 0})

	res, err := WaitRun(context.Background(), "AAAAAA", time.Second)
	if err != nil {
		t.Fatalf("WaitRun: %v", err)
	}
	if !res.Finished {
		t.Errorf("finished = false, want true")
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Errorf("exit_code = %v, want 0", res.ExitCode)
	}
}

func TestWaitRunTimeout(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	seedRunDir(t, "AAAAAA", nil)

	res, err := WaitRun(context.Background(), "AAAAAA", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitRun: %v", err)
	}
	if res.Finished {
		t.Errorf("finished = true, want false on timeout")
	}
}

func TestWaitRunTransition(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir := seedRunDir(t, "AAAAAA", nil)

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = WriteMeta(dir, &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}}, ExitCode: 0})
	}()

	res, err := WaitRun(context.Background(), "AAAAAA", 5*time.Second)
	if err != nil {
		t.Fatalf("WaitRun: %v", err)
	}
	if !res.Finished {
		t.Errorf("finished = false, want true after transition")
	}
}

func TestWaitRunUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := WaitRun(context.Background(), "ZZZZZZ", time.Second); !errors.Is(err, ErrUnknownRunID) {
		t.Errorf("err = %v, want ErrUnknownRunID", err)
	}
}
