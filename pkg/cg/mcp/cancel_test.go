package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ripta/rt/pkg/cg"
)

// startCancelRun launches a real child under capture, registers its Done
// channel, and arranges a best-effort kill at test end so a hung child does
// not outlive the test.
func startCancelRun(t *testing.T, reg *runRegistry, args ...string) *cg.CaptureRun {
	t.Helper()
	run, err := cg.RunCapture(args, nil, "", nil)
	if err != nil {
		t.Fatalf("RunCapture: %v", err)
	}
	reg.Add(run.ID, run.Done)
	t.Cleanup(func() {
		select {
		case <-run.Done:
			return
		default:
		}
		if pid, perr := cg.ReadPidFile(run.Dir); perr == nil {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	})
	return run
}

// waitDone blocks until the run's Done channel closes or the timeout fires,
// failing the test on timeout.
func waitDone(t *testing.T, run *cg.CaptureRun, timeout time.Duration) {
	t.Helper()
	select {
	case <-run.Done:
	case <-time.After(timeout):
		t.Fatalf("child %s did not exit within %v", run.ID, timeout)
	}
}

// waitReady polls the run's captured stdout until it contains "ready", which a
// child prints once its signal trap is installed. Without this handshake a
// cancel sent immediately after start can race the trap and hit the default
// disposition instead of the handler under test.
func waitReady(t *testing.T, run *cg.CaptureRun, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(run.Dir, "stdout"))
		if strings.Contains(string(data), "ready") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child %s never became ready", run.ID)
}

func TestHandleCancelSigterm(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	reg := newRunRegistry()
	run := startCancelRun(t, reg, "sleep", "30")

	_, out, err := handleCancel(context.Background(), reg, cancelInput{ID: run.ID, Signal: "SIGTERM"})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if !out.Signaled {
		t.Errorf("Signaled = false, want true")
	}
	if out.Signal != int(syscall.SIGTERM) {
		t.Errorf("Signal = %d, want %d", out.Signal, int(syscall.SIGTERM))
	}
	if out.Escalated {
		t.Errorf("Escalated = true, want false")
	}
	if out.Finished {
		t.Errorf("Finished = true, want false for fire-and-forget")
	}
	waitDone(t, run, 5*time.Second)
}

func TestHandleCancelSigkill(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	reg := newRunRegistry()
	run := startCancelRun(t, reg, "sleep", "30")

	_, out, err := handleCancel(context.Background(), reg, cancelInput{ID: run.ID, Signal: "SIGKILL"})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if !out.Signaled {
		t.Errorf("Signaled = false, want true")
	}
	if out.Signal != int(syscall.SIGKILL) {
		t.Errorf("Signal = %d, want %d", out.Signal, int(syscall.SIGKILL))
	}
	waitDone(t, run, 5*time.Second)
}

func TestHandleCancelAlreadyFinished(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedRunDir(t, "AAAAAA", &cg.Meta{
		ID:         "AAAAAA",
		Command:    []string{"echo", "done"},
		ExitCode:   0,
		DurationMs: 5,
	})

	_, out, err := handleCancel(context.Background(), newRunRegistry(), cancelInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if out.Signaled {
		t.Errorf("Signaled = true, want false for a finished run")
	}
	if !out.Finished {
		t.Errorf("Finished = false, want true for a finished run")
	}
}

func TestHandleCancelUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleCancel(context.Background(), newRunRegistry(), cancelInput{ID: "ZZZZZZ"})
	if err == nil {
		t.Fatalf("expected error for unknown ID")
	}
	if !strings.Contains(err.Error(), "unknown run id: ZZZZZZ") {
		t.Errorf("error = %q, want unknown run id message", err.Error())
	}
}

func TestHandleCancelInvalidSignal(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleCancel(context.Background(), newRunRegistry(), cancelInput{ID: "AAAAAA", Signal: "SIGFOO"})
	if err == nil {
		t.Fatalf("expected error for invalid signal")
	}
	if !strings.Contains(err.Error(), "unsupported signal") {
		t.Errorf("error = %q, want unsupported signal message", err.Error())
	}
}

func TestHandleCancelEscalationFired(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	reg := newRunRegistry()
	// The shell ignores TERM and busy-loops on a builtin, so a group SIGTERM
	// does not bring it down; only the escalated SIGKILL does. A builtin loop
	// avoids spawning a child that a group signal could take down instead.
	run := startCancelRun(t, reg, "sh", "-c", `trap "" TERM; echo ready; while :; do :; done`)
	waitReady(t, run, 2*time.Second)

	_, out, err := handleCancel(context.Background(), reg, cancelInput{
		ID:              run.ID,
		Signal:          "SIGTERM",
		EscalateAfterMs: 300,
		EscalateSignal:  "SIGKILL",
	})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if !out.Signaled {
		t.Errorf("Signaled = false, want true")
	}
	if !out.Escalated {
		t.Errorf("Escalated = false, want true")
	}
	if out.EscalateSignal != int(syscall.SIGKILL) {
		t.Errorf("EscalateSignal = %d, want %d", out.EscalateSignal, int(syscall.SIGKILL))
	}
	waitDone(t, run, 5*time.Second)
}

func TestHandleCancelEscalationNotNeeded(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	reg := newRunRegistry()
	// The shell exits cleanly on TERM, so escalation should never fire.
	run := startCancelRun(t, reg, "sh", "-c", `trap "exit 0" TERM; echo ready; sleep 30`)
	waitReady(t, run, 2*time.Second)

	_, out, err := handleCancel(context.Background(), reg, cancelInput{
		ID:              run.ID,
		Signal:          "SIGTERM",
		EscalateAfterMs: 2000,
	})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if !out.Signaled {
		t.Errorf("Signaled = false, want true")
	}
	if out.Escalated {
		t.Errorf("Escalated = true, want false (child exited within window)")
	}
	if !out.Finished {
		t.Errorf("Finished = false, want true (child exited within window)")
	}
	waitDone(t, run, 5*time.Second)
}

func TestHandleCancelProcessGone(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	// Start and reap a child so its PID is no longer a live process group.
	c := exec.Command("true")
	if err := c.Start(); err != nil {
		t.Fatalf("starting true: %v", err)
	}
	gonePid := c.Process.Pid
	_ = c.Wait()

	dir := seedRunDir(t, "AAAAAA", nil)
	if err := cg.WritePidFile(dir, gonePid); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	_, out, err := handleCancel(context.Background(), newRunRegistry(), cancelInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if out.Signaled {
		t.Errorf("Signaled = true, want false for a gone process")
	}
	if !out.Finished {
		t.Errorf("Finished = false, want true for a gone process")
	}
}

func TestHandleCancelNoPidFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedRunDir(t, "AAAAAA", nil)

	_, _, err := handleCancel(context.Background(), newRunRegistry(), cancelInput{ID: "AAAAAA"})
	if err == nil {
		t.Fatalf("expected error for in-flight run with no pid file")
	}
	if !strings.Contains(err.Error(), "no pid recorded") {
		t.Errorf("error = %q, want no pid recorded message", err.Error())
	}
}
