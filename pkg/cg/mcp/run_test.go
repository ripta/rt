package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ripta/rt/pkg/cg"
)

func TestHandleRunSyncSuccess(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleRun(context.Background(), nil, runInput{
		Command: []string{"echo", "hi"},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ID == "" {
		t.Errorf("ID empty")
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", out.ExitCode)
	}
	if out.StdoutExcerpt != "hi\n" {
		t.Errorf("StdoutExcerpt = %q, want %q", out.StdoutExcerpt, "hi\n")
	}
	if out.Truncated {
		t.Errorf("Truncated = true, want false")
	}
	if out.TimedOut {
		t.Errorf("TimedOut = true, want false")
	}
	if out.Started {
		t.Errorf("Started = true, want false")
	}
	if out.StdoutLines == nil || *out.StdoutLines != 1 {
		t.Errorf("StdoutLines = %v, want 1", out.StdoutLines)
	}
}

func TestHandleRunNonZeroExit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleRun(context.Background(), nil, runInput{
		Command: []string{"sh", "-c", "exit 7"},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 7 {
		t.Errorf("ExitCode = %v, want 7", out.ExitCode)
	}
}

func TestHandleRunAsync(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	wait := false
	_, out, err := handleRun(context.Background(), nil, runInput{
		Command: []string{"echo", "async"},
		Wait:    &wait,
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !out.Started {
		t.Errorf("Started = false, want true")
	}
	if out.ExitCode != nil {
		t.Errorf("ExitCode = %v, want nil (async)", out.ExitCode)
	}
	if out.StdoutExcerpt != "" {
		t.Errorf("StdoutExcerpt = %q, want empty (async)", out.StdoutExcerpt)
	}

	// Wait for the background goroutine to flush meta.json so the test
	// doesn't leave files unattended.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(cg.CaptureRoot(), out.ID, "meta.json")); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("meta.json never appeared for async run %s", out.ID)
}

func TestHandleRunTimeout(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleRun(context.Background(), nil, runInput{
		Command:       []string{"sh", "-c", "echo partial; sleep 2"},
		WaitTimeoutMs: 200,
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !out.TimedOut {
		t.Errorf("TimedOut = false, want true")
	}
	if out.ExitCode != nil {
		t.Errorf("ExitCode = %v, want nil (still running)", out.ExitCode)
	}
	if !strings.Contains(out.StdoutExcerpt, "partial") {
		t.Errorf("StdoutExcerpt = %q, want to contain %q", out.StdoutExcerpt, "partial")
	}
}

func TestHandleRunExcerptTruncated(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleRun(context.Background(), nil, runInput{
		Command:      []string{"sh", "-c", "printf 'abcdefghijklmnop'"},
		ExcerptBytes: 4,
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.StdoutExcerpt != "abcd" {
		t.Errorf("StdoutExcerpt = %q, want %q", out.StdoutExcerpt, "abcd")
	}
	if !out.Truncated {
		t.Errorf("Truncated = false, want true")
	}
}

func TestHandleRunExcerptClamped(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleRun(context.Background(), nil, runInput{
		Command:      []string{"echo", "ok"},
		ExcerptBytes: 1 << 24, // 16 MB, way over the cap
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.StdoutExcerpt != "ok\n" {
		t.Errorf("StdoutExcerpt = %q, want %q", out.StdoutExcerpt, "ok\n")
	}
}

func TestHandleRunEmptyCommand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleRun(context.Background(), nil, runInput{})
	if err == nil {
		t.Fatalf("expected error for empty command, got nil")
	}
}

func TestHandleRunStartError(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleRun(context.Background(), nil, runInput{
		Command: []string{"this-binary-does-not-exist-zzzz"},
	})
	if err == nil {
		t.Fatalf("expected error for missing binary, got nil")
	}
}

func TestHandleRunContextCancelled(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := handleRun(ctx, nil, runInput{
		Command:       []string{"sh", "-c", "sleep 2"},
		WaitTimeoutMs: 5000,
	})
	if err == nil {
		t.Fatalf("expected ctx.Err(), got nil")
	}
}
