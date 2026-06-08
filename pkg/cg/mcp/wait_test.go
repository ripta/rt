package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ripta/rt/pkg/cg"
)

func TestHandleWaitUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleWait(context.Background(), newRunRegistry(), waitInput{ID: "ZZZZZZ"})
	if err == nil {
		t.Fatalf("expected error for unknown ID")
	}
	if !strings.Contains(err.Error(), "unknown run id: ZZZZZZ") {
		t.Errorf("error = %q, want unknown run id message", err.Error())
	}
}

func TestHandleWaitAlreadyFinished(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	exit := 3
	seedRunDir(t, "AAAAAA", &cg.Meta{
		ID:         "AAAAAA",
		Command:    []string{"echo", "done"},
		ExitCode:   exit,
		DurationMs: 5,
	})

	_, out, err := handleWait(context.Background(), newRunRegistry(), waitInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if !out.Finished {
		t.Errorf("Finished = false, want true")
	}
	if out.ID != "AAAAAA" {
		t.Errorf("ID = %q, want AAAAAA", out.ID)
	}
	if out.ExitCode == nil || *out.ExitCode != exit {
		t.Errorf("ExitCode = %v, want %d", out.ExitCode, exit)
	}
}

func TestHandleWaitFastPath(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedRunDir(t, "AAAAAA", nil)

	reg := newRunRegistry()
	done := make(chan struct{})
	reg.Add("AAAAAA", done)

	// Close Done after a short delay and write meta.json as the real run would.
	go func() {
		time.Sleep(50 * time.Millisecond)
		if err := cg.WriteMeta(cg.CaptureRoot()+"/AAAAAA", &cg.Meta{
			ID:         "AAAAAA",
			Command:    []string{"echo", "fp"},
			DurationMs: 9,
		}); err != nil {
			t.Errorf("WriteMeta: %v", err)
		}
		close(done)
	}()

	start := time.Now()
	_, out, err := handleWait(context.Background(), reg, waitInput{ID: "AAAAAA", TimeoutMs: 5000})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if !out.Finished {
		t.Errorf("Finished = false, want true")
	}
	if out.DurationMs == nil || *out.DurationMs != 9 {
		t.Errorf("DurationMs = %v, want 9", out.DurationMs)
	}
	// Fast path should not need the 100 ms poll tick; sanity-check we beat that.
	if elapsed >= waitPollInterval {
		t.Errorf("elapsed = %v, want < %v (fast path should beat ticker)", elapsed, waitPollInterval)
	}
}

func TestHandleWaitSlowPath(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedRunDir(t, "AAAAAA", nil)

	go func() {
		time.Sleep(250 * time.Millisecond)
		_ = cg.WriteMeta(cg.CaptureRoot()+"/AAAAAA", &cg.Meta{
			ID:         "AAAAAA",
			Command:    []string{"echo", "sp"},
			DurationMs: 11,
		})
	}()

	_, out, err := handleWait(context.Background(), newRunRegistry(), waitInput{ID: "AAAAAA", TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if !out.Finished {
		t.Errorf("Finished = false, want true")
	}
	if out.DurationMs == nil || *out.DurationMs != 11 {
		t.Errorf("DurationMs = %v, want 11", out.DurationMs)
	}
}

func TestHandleWaitTimeout(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedRunDir(t, "AAAAAA", nil)

	_, out, err := handleWait(context.Background(), newRunRegistry(), waitInput{ID: "AAAAAA", TimeoutMs: 100})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if out.Finished {
		t.Errorf("Finished = true, want false on timeout")
	}
	if out.ID != "AAAAAA" {
		t.Errorf("ID = %q, want AAAAAA", out.ID)
	}
	if out.ExitCode != nil {
		t.Errorf("ExitCode = %v, want nil on timeout", out.ExitCode)
	}
}
