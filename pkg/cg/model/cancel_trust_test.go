package model

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// victim is an unrelated process in its own process group, standing in for a process of the
// user's that a forged pid file points cancel at.
type victim struct {
	cmd  *exec.Cmd
	gone chan struct{}
}

func startVictim(t *testing.T) *victim {
	t.Helper()

	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting victim: %v", err)
	}

	v := &victim{cmd: cmd, gone: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(v.gone)
	}()

	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-v.gone
	})
	return v
}

func (v *victim) pid() int { return v.cmd.Process.Pid }

// survives reports whether the victim is still running after a signal sent before the call
// would have had time to land.
func (v *victim) survives() bool {
	select {
	case <-v.gone:
		return false
	case <-time.After(300 * time.Millisecond):
		return true
	}
}

// newIncompleteRunDir allocates a run directory with no meta.json, the on-disk state of an
// in-flight run.
func newIncompleteRunDir(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())

	cap, err := NewCapture()
	if err != nil {
		t.Fatalf("NewCapture: %v", err)
	}
	if err := cap.Close(); err != nil {
		t.Fatalf("closing capture: %v", err)
	}
	return cap.ID, cap.Dir
}

func TestCancelRunRefusesPidNotTiedToRun(t *testing.T) {
	id, dir := newIncompleteRunDir(t)
	v := startVictim(t)

	if err := WritePidFile(dir, v.pid()); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	_, err := CancelRun(context.Background(), id, CancelOptions{Signal: "SIGKILL"})
	if err == nil || !strings.Contains(err.Error(), "cannot be tied") {
		t.Fatalf("CancelRun err = %v, want a refusal saying the pid cannot be tied to the run", err)
	}
	if !v.survives() {
		t.Error("the process the forged pid file named was signalled")
	}
}

// TestCancelRunRefusesOtherRunsChild pins that the supervisor's argv names this run's directory.
// A real run's child has a genuine supervise-run parent, but for a different directory.
func TestCancelRunRefusesOtherRunsChild(t *testing.T) {
	id, dir := newIncompleteRunDir(t)

	other, err := RunSupervised([]string{"sleep", "60"}, SuperviseOptions{})
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
	}
	t.Cleanup(func() {
		_, _ = CancelRun(context.Background(), other.ID, CancelOptions{Signal: "SIGKILL"})
		waitDone(t, other, 5*time.Second)
	})

	if err := WritePidFile(dir, other.Pid); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	_, err = CancelRun(context.Background(), id, CancelOptions{Signal: "SIGKILL"})
	if err == nil || !strings.Contains(err.Error(), "cannot be tied") {
		t.Fatalf("CancelRun err = %v, want a refusal saying the pid cannot be tied to the run", err)
	}

	select {
	case <-other.Done:
		t.Error("the other run's child was signalled")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestCancelPoolRefusesPidNotTiedToPool(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	dir := seedPoolDir(t, "PPPPPP", &PoolManifest{
		ID:        "PPPPPP",
		Commands:  [][]string{{"sleep", "60"}},
		StartedAt: time.Now().UTC(),
		Runs:      []PoolRunRecord{{Command: 0, RunID: "AAAAAA", Status: PoolRunRunning}},
	})

	lock, err := acquireRunLock(dir)
	if err != nil {
		t.Fatalf("acquiring lock: %v", err)
	}
	defer lock.Close()

	v := startVictim(t)
	if err := WritePidFile(dir, v.pid()); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	_, err = CancelRun(context.Background(), "PPPPPP", CancelOptions{Signal: "SIGKILL"})
	if err == nil || !strings.Contains(err.Error(), "cannot be tied") {
		t.Fatalf("CancelRun err = %v, want a refusal saying the pid cannot be tied to the pool", err)
	}
	if !v.survives() {
		t.Error("the process the forged pid file named was signalled")
	}
}

// TestSupervisePoolKillIgnoresForgedMemberPid pins that a pool supervisor cancelling its members
// on_error=kill signals the pid each member's ack reported, not the pid file a member's own
// command could have rewritten.
func TestSupervisePoolKillIgnoresForgedMemberPid(t *testing.T) {
	_, dir := newPoolTestDir(t)
	v := startVictim(t)

	spec := poolSpec(t,
		[]string{"sleep", "60"},
		[]string{"sh", "-c", "sleep 1; exit 1"},
	)
	spec.Parallelism = 2
	spec.OnError = OnErrorKill

	sup, _ := startDetachedPool(t, dir, spec)
	memberID := waitForRunning(t, dir, 0)
	waitForRunning(t, dir, 1)

	memberDir, err := LookupRunDir(memberID)
	if err != nil && err != ErrIncompleteRun {
		t.Fatalf("LookupRunDir: %v", err)
	}
	if err := WritePidFile(memberDir, v.pid()); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- sup.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pool supervisor exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		_ = sup.Process.Kill()
		t.Fatal("pool did not finish: the in-flight member was never cancelled")
	}

	m := readManifest(t, dir)
	rec := m.Runs[0]
	if rec.Signal == nil || *rec.Signal != int(syscall.SIGTERM) {
		t.Errorf("member 0 = %+v, want finished with SIGTERM", rec)
	}
	if !v.survives() {
		t.Error("the process the forged pid file named was signalled")
	}
}

// TestCancelRunRefusesCaptureRun pins the --capture case: the child of a shell-side cg process has a
// cg parent, but one that supervises no run directory. The refusal carries the ctrl-C hint, and a
// pool refusal does not.
func TestCancelRunRefusesCaptureRun(t *testing.T) {
	id, dir := newIncompleteRunDir(t)

	parent := exec.Command("sh", "-c", "sleep 60 & echo $!; wait")
	parent.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := parent.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := parent.Start(); err != nil {
		t.Fatalf("starting parent: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-parent.Process.Pid, syscall.SIGKILL)
		_ = parent.Wait()
	})

	var child int
	if _, err := fmt.Fscan(out, &child); err != nil {
		t.Fatalf("reading child pid: %v", err)
	}
	if err := WritePidFile(dir, child); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	// The pid is printed as soon as the shell forks, before the child execs, and a process
	// mid-exec cannot be inspected.
	waitFor(t, "child to exec", func() bool {
		info, err := lookupProcess(child)
		return err == nil && len(info.Argv) > 0 && info.Argv[0] == "sleep"
	})

	_, err = CancelRun(context.Background(), id, CancelOptions{Signal: "SIGKILL"})
	if err == nil || !strings.Contains(err.Error(), "cannot be tied") || !strings.Contains(err.Error(), "ctrl-C") {
		t.Fatalf("CancelRun err = %v, want a refusal with the ctrl-C hint", err)
	}
}

func TestCancelPoolRefusalHasNoCaptureHint(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	dir := seedPoolDir(t, "PPPPPP", &PoolManifest{
		ID:        "PPPPPP",
		Commands:  [][]string{{"sleep", "60"}},
		StartedAt: time.Now().UTC(),
		Runs:      []PoolRunRecord{{Command: 0, RunID: "AAAAAA", Status: PoolRunRunning}},
	})
	lock, err := acquireRunLock(dir)
	if err != nil {
		t.Fatalf("acquiring lock: %v", err)
	}
	defer lock.Close()

	v := startVictim(t)
	if err := WritePidFile(dir, v.pid()); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	_, err = CancelRun(context.Background(), "PPPPPP", CancelOptions{})
	if err == nil || strings.Contains(err.Error(), "--capture") {
		t.Fatalf("CancelRun err = %v, want a refusal without the --capture hint", err)
	}
}
