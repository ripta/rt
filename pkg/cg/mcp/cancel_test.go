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

	"github.com/ripta/rt/pkg/cg/model"
)

// startCancelRun launches a real child under capture, registers its Done
// channel, and arranges a best-effort kill at test end so a hung child does
// not outlive the test.
func startCancelRun(t *testing.T, reg *runRegistry, args ...string) *model.CaptureRun {
	t.Helper()
	run, err := model.RunSupervised(args, model.SuperviseOptions{})
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
	}
	reg.Add(run.ID, run.Done)
	t.Cleanup(func() {
		select {
		case <-run.Done:
			return
		default:
		}
		if pid, perr := model.ReadPidFile(run.Dir); perr == nil {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	})
	return run
}

// waitDone blocks until the run's Done channel closes or the timeout fires,
// failing the test on timeout.
func waitDone(t *testing.T, run *model.CaptureRun, timeout time.Duration) {
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
func waitReady(t *testing.T, run *model.CaptureRun, timeout time.Duration) {
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

// startCancelPool launches a real pool via the run_many path without waiting
// and returns its ID and directory once member job is running.
func startCancelPool(t *testing.T, reg *runRegistry, job int, commands ...[]string) (string, string) {
	t.Helper()

	async := false
	_, started, err := handleRunMany(context.Background(), reg, nil, nil, "", runManyInput{
		Commands: commands,
		Wait:     &async,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if !started.Started {
		t.Fatalf("pool not started: %+v", started)
	}

	dir := filepath.Join(model.CaptureRoot(), started.ID)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m, err := model.ReadPoolManifest(dir)
		if err == nil && len(m.Runs) > job && m.Runs[job].Status == model.PoolRunRunning {
			return started.ID, dir
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pool %s member %d never started running", started.ID, job)
	return "", ""
}

func TestHandleCancelPoolStop(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	reg := newRunRegistry()
	id, dir := startCancelPool(t, reg, 0,
		[]string{"sh", "-c", "sleep 0.5; echo done"},
		[]string{"echo", "never"},
	)

	_, out, err := handleCancel(context.Background(), reg, cancelInput{ID: id})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if !out.Pool || !out.Signaled {
		t.Errorf("output = %+v, want pool and signaled", out)
	}
	if out.Signal != int(syscall.SIGTERM) {
		t.Errorf("Signal = %d, want SIGTERM", out.Signal)
	}

	waitForPoolFinished(t, dir)
	m, err := model.ReadPoolManifest(dir)
	if err != nil {
		t.Fatalf("ReadPoolManifest: %v", err)
	}
	if m.Runs[0].Status != model.PoolRunFinished || m.Runs[0].Signal != nil {
		t.Errorf("in-flight run = %+v, want finished without a signal", m.Runs[0])
	}
	if m.Runs[1].Status != model.PoolRunSkipped {
		t.Errorf("pending run status = %q, want skipped", m.Runs[1].Status)
	}
}

func TestHandleCancelPoolKill(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	reg := newRunRegistry()
	id, dir := startCancelPool(t, reg, 0,
		[]string{"sleep", "30"},
		[]string{"echo", "never"},
	)

	_, out, err := handleCancel(context.Background(), reg, cancelInput{ID: id, Signal: "SIGINT"})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if !out.Pool || !out.Signaled {
		t.Errorf("output = %+v, want pool and signaled", out)
	}

	waitForPoolFinished(t, dir)
	m, err := model.ReadPoolManifest(dir)
	if err != nil {
		t.Fatalf("ReadPoolManifest: %v", err)
	}
	if m.Runs[0].Status != model.PoolRunFinished || m.Runs[0].Signal == nil {
		t.Errorf("in-flight run = %+v, want finished by a signal", m.Runs[0])
	}
	if m.Runs[1].Status != model.PoolRunSkipped {
		t.Errorf("pending run status = %q, want skipped", m.Runs[1].Status)
	}
}

func TestHandleCancelPoolEscalation(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	reg := newRunRegistry()
	id, dir := startCancelPool(t, reg, 0, []string{"sleep", "30"})

	// SIGTERM only stops scheduling; the 30s member keeps the pool alive past
	// the escalation window, so the SIGINT escalation must cancel it.
	_, out, err := handleCancel(context.Background(), reg, cancelInput{
		ID:              id,
		Signal:          "SIGTERM",
		EscalateAfterMs: 200,
		EscalateSignal:  "SIGINT",
	})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if !out.Pool || !out.Signaled || !out.Escalated {
		t.Errorf("output = %+v, want pool, signaled, escalated", out)
	}
	if out.EscalateSignal != int(syscall.SIGINT) {
		t.Errorf("EscalateSignal = %d, want SIGINT", out.EscalateSignal)
	}

	waitForPoolFinished(t, dir)
	m, err := model.ReadPoolManifest(dir)
	if err != nil {
		t.Fatalf("ReadPoolManifest: %v", err)
	}
	if m.Runs[0].Status != model.PoolRunFinished || m.Runs[0].Signal == nil {
		t.Errorf("member = %+v, want finished by a signal", m.Runs[0])
	}
}

func TestHandleCancelPoolFinished(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	m := runningPoolManifest("PPPPPP")
	finishPoolManifest(m)
	seedPoolDir(t, "PPPPPP", m)

	_, out, err := handleCancel(context.Background(), newRunRegistry(), cancelInput{ID: "PPPPPP"})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if !out.Pool || out.Signaled || !out.Finished {
		t.Errorf("output = %+v, want pool, unsignaled, finished", out)
	}
}

func TestHandleCancelPoolAbandoned(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	// Abandoned: no finished_at, released lock, and a stale pid file left by a
	// SIGKILLed supervisor. The guard must report finished without signalling.
	dir := seedPoolDir(t, "PPPPPP", runningPoolManifest("PPPPPP"))
	seedLockFile(t, dir)
	if err := model.WritePidFile(dir, os.Getpid()); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	_, out, err := handleCancel(context.Background(), newRunRegistry(), cancelInput{ID: "PPPPPP"})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if !out.Pool || out.Signaled || !out.Finished {
		t.Errorf("output = %+v, want pool, unsignaled, finished", out)
	}
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

	seedRunDir(t, "AAAAAA", &model.Meta{
		RunInfo:    model.RunInfo{ID: "AAAAAA", Command: []string{"echo", "done"}},
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
	if err := model.WritePidFile(dir, gonePid); err != nil {
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
