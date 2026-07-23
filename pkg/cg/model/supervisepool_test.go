package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// newPoolTestDir simulates the server side of a pool: an isolated capture root
// and an allocated pool directory.
func newPoolTestDir(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())

	id, dir, err := NewPoolDir()
	if err != nil {
		t.Fatalf("NewPoolDir: %v", err)
	}
	return id, dir
}

// poolSpec builds a spec for the given argvs with each executable identity
// resolved the way the server resolves it.
func poolSpec(t *testing.T, argvs ...[]string) *PoolSpec {
	t.Helper()

	spec := &PoolSpec{}
	for _, argv := range argvs {
		resolved, _ := ResolveCommand(argv, "")
		spec.Commands = append(spec.Commands, PoolCommand{
			Argv:      argv,
			Resolved:  resolved.Resolved,
			Canonical: resolved.Canonical,
		})
	}
	return spec
}

// runPool drives supervisePool in-process with the marshaled spec on stdin and
// decodes the ack from stdout.
func runPool(t *testing.T, dir string, spec *PoolSpec) (SuperviseAck, error) {
	t.Helper()

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshaling spec: %v", err)
	}

	var out bytes.Buffer
	runErr := supervisePool(dir, bytes.NewReader(data), &out)

	var ack SuperviseAck
	if err := json.Unmarshal(out.Bytes(), &ack); err != nil {
		t.Fatalf("decoding ack from %q: %v", out.String(), err)
	}
	return ack, runErr
}

// readManifest loads the pool manifest and fails the test on error.
func readManifest(t *testing.T, dir string) *PoolManifest {
	t.Helper()

	m, err := ReadPoolManifest(dir)
	if err != nil {
		t.Fatalf("ReadPoolManifest: %v", err)
	}
	return m
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, desc string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

func TestSupervisePoolContinueRunsAll(t *testing.T) {
	id, dir := newPoolTestDir(t)

	spec := poolSpec(t, []string{"echo", "one"}, []string{"sh", "-c", "exit 3"}, []string{"echo", "two"})
	ack, err := runPool(t, dir, spec)
	if err != nil {
		t.Fatalf("supervisePool: %v", err)
	}
	if !ack.Started || ack.Pid != os.Getpid() {
		t.Errorf("ack = %+v, want started with this process's pid", ack)
	}

	m := readManifest(t, dir)
	if m.ID != id {
		t.Errorf("manifest.ID = %q, want %q", m.ID, id)
	}
	if m.FinishedAt == nil {
		t.Error("manifest has no finished_at, want complete")
	}
	if m.OnError != OnErrorContinue || m.Repeat != 1 || m.Parallelism != 1 {
		t.Errorf("normalized spec echo = %s/%d/%d, want continue/1/1", m.OnError, m.Repeat, m.Parallelism)
	}
	if len(m.Runs) != 3 {
		t.Fatalf("len(Runs) = %d, want 3", len(m.Runs))
	}

	wantExits := []int{0, 3, 0}
	for i, rec := range m.Runs {
		if rec.Status != PoolRunFinished {
			t.Errorf("run %d status = %q, want finished", i, rec.Status)
			continue
		}
		if rec.RunID == "" {
			t.Errorf("run %d has no run ID", i)
		}
		if rec.ExitCode == nil || *rec.ExitCode != wantExits[i] {
			t.Errorf("run %d exit = %v, want %d", i, rec.ExitCode, wantExits[i])
		}
		if rec.Command != i {
			t.Errorf("run %d command index = %d, want %d", i, rec.Command, i)
		}
	}

	// Members are ordinary sibling run dirs carrying the pool field.
	memberDir := filepath.Join(CaptureRoot(), m.Runs[0].RunID)
	meta, err := ReadMeta(memberDir)
	if err != nil {
		t.Fatalf("ReadMeta on member: %v", err)
	}
	if meta.Pool != id {
		t.Errorf("member meta.Pool = %q, want %q", meta.Pool, id)
	}
}

func TestSupervisePoolThreadsSessionID(t *testing.T) {
	id, dir := newPoolTestDir(t)

	spec := poolSpec(t, []string{"echo", "one"}, []string{"echo", "two"})
	spec.SessionID = "SESS"
	if _, err := runPool(t, dir, spec); err != nil {
		t.Fatalf("supervisePool: %v", err)
	}

	m := readManifest(t, dir)
	if m.SessionID != "SESS" {
		t.Errorf("manifest.SessionID = %q, want %q", m.SessionID, "SESS")
	}

	// Each member carries both the pool ID and the session it inherited.
	for i, rec := range m.Runs {
		meta, err := ReadMeta(filepath.Join(CaptureRoot(), rec.RunID))
		if err != nil {
			t.Fatalf("ReadMeta on member %d: %v", i, err)
		}
		if meta.Pool != id {
			t.Errorf("member %d meta.Pool = %q, want %q", i, meta.Pool, id)
		}
		if meta.SessionID != "SESS" {
			t.Errorf("member %d meta.SessionID = %q, want %q", i, meta.SessionID, "SESS")
		}
	}
}

func TestSupervisePoolOrderingWithRepeat(t *testing.T) {
	_, dir := newPoolTestDir(t)

	logFile := filepath.Join(t.TempDir(), "order.log")
	spec := poolSpec(t,
		[]string{"sh", "-c", "echo one >> " + logFile},
		[]string{"sh", "-c", "echo two >> " + logFile},
	)
	spec.Repeat = 2

	if _, err := runPool(t, dir, spec); err != nil {
		t.Fatalf("supervisePool: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("reading order log: %v", err)
	}
	want := "one\none\ntwo\ntwo\n"
	if string(data) != want {
		t.Errorf("order log = %q, want %q", data, want)
	}

	m := readManifest(t, dir)
	wantCommands := []int{0, 0, 1, 1}
	for i, rec := range m.Runs {
		if rec.Command != wantCommands[i] {
			t.Errorf("run %d command index = %d, want %d", i, rec.Command, wantCommands[i])
		}
	}
}

func TestSupervisePoolStopLetsInFlightFinish(t *testing.T) {
	_, dir := newPoolTestDir(t)

	spec := poolSpec(t,
		[]string{"sh", "-c", "sleep 0.4; echo done"},
		[]string{"sh", "-c", "exit 3"},
		[]string{"echo", "never"},
	)
	spec.Parallelism = 2
	spec.OnError = OnErrorStop

	if _, err := runPool(t, dir, spec); err != nil {
		t.Fatalf("supervisePool: %v", err)
	}

	m := readManifest(t, dir)
	if m.FinishedAt == nil {
		t.Error("manifest has no finished_at, want complete")
	}

	sleeper := m.Runs[0]
	if sleeper.Status != PoolRunFinished || sleeper.ExitCode == nil || *sleeper.ExitCode != 0 {
		t.Errorf("in-flight run = %+v, want finished with exit 0", sleeper)
	}
	if sleeper.Signal != nil {
		t.Errorf("in-flight run signal = %v, want nil: stop must not kill", *sleeper.Signal)
	}

	if m.Runs[1].Status != PoolRunFinished || *m.Runs[1].ExitCode != 3 {
		t.Errorf("failing run = %+v, want finished with exit 3", m.Runs[1])
	}

	skipped := m.Runs[2]
	if skipped.Status != PoolRunSkipped {
		t.Errorf("unscheduled run status = %q, want skipped", skipped.Status)
	}
	if skipped.RunID != "" {
		t.Errorf("skipped run has run ID %q, want none", skipped.RunID)
	}
}

func TestSupervisePoolKillCancelsInFlight(t *testing.T) {
	_, dir := newPoolTestDir(t)

	spec := poolSpec(t,
		[]string{"sleep", "30"},
		[]string{"sh", "-c", "exit 3"},
		[]string{"echo", "never"},
	)
	spec.Parallelism = 2
	spec.OnError = OnErrorKill

	start := time.Now()
	if _, err := runPool(t, dir, spec); err != nil {
		t.Fatalf("supervisePool: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("pool took %v, want well under the sleeper's 30s", elapsed)
	}

	m := readManifest(t, dir)
	sleeper := m.Runs[0]
	if sleeper.Status != PoolRunFinished {
		t.Fatalf("in-flight run status = %q, want finished", sleeper.Status)
	}
	if sleeper.Signal == nil || *sleeper.Signal != int(syscall.SIGTERM) {
		t.Errorf("in-flight run signal = %v, want SIGTERM", sleeper.Signal)
	}
	if m.Runs[2].Status != PoolRunSkipped {
		t.Errorf("unscheduled run status = %q, want skipped", m.Runs[2].Status)
	}
}

func TestSupervisePoolFailureAcks(t *testing.T) {
	badSpecs := []struct {
		name string
		spec *PoolSpec
	}{
		{"empty commands", &PoolSpec{}},
		{"empty argv", &PoolSpec{Commands: []PoolCommand{{}}}},
		{"unknown on_error", &PoolSpec{Commands: []PoolCommand{{Argv: []string{"true"}}}, OnError: "retry"}},
		{"negative repeat", &PoolSpec{Commands: []PoolCommand{{Argv: []string{"true"}}}, Repeat: -1}},
		{"negative parallelism", &PoolSpec{Commands: []PoolCommand{{Argv: []string{"true"}}}, Parallelism: -2}},
	}

	for _, tc := range badSpecs {
		t.Run(tc.name, func(t *testing.T) {
			_, dir := newPoolTestDir(t)

			ack, err := runPool(t, dir, tc.spec)
			if err == nil {
				t.Fatal("supervisePool succeeded, want error")
			}
			if ack.Started || ack.StartError == "" {
				t.Errorf("ack = %+v, want a start error", ack)
			}
			if _, merr := ReadPoolManifest(dir); merr == nil {
				t.Error("manifest written for a rejected spec")
			}
		})
	}

	t.Run("bad JSON", func(t *testing.T) {
		_, dir := newPoolTestDir(t)

		var out bytes.Buffer
		if err := supervisePool(dir, strings.NewReader("{nope"), &out); err == nil {
			t.Fatal("supervisePool succeeded, want error")
		}

		var ack SuperviseAck
		if err := json.Unmarshal(out.Bytes(), &ack); err != nil {
			t.Fatalf("decoding ack from %q: %v", out.String(), err)
		}
		if ack.Started || ack.StartError == "" {
			t.Errorf("ack = %+v, want a start error", ack)
		}
	})

	t.Run("lock held", func(t *testing.T) {
		_, dir := newPoolTestDir(t)

		lock, err := acquireRunLock(dir)
		if err != nil {
			t.Fatalf("pre-acquiring lock: %v", err)
		}
		defer lock.Close()

		ack, err := runPool(t, dir, poolSpec(t, []string{"true"}))
		if err == nil {
			t.Fatal("supervisePool succeeded, want error")
		}
		if ack.Started || ack.StartError == "" {
			t.Errorf("ack = %+v, want a start error", ack)
		}
	})
}

func TestSupervisePoolCommandWiring(t *testing.T) {
	c := NewSupervisePoolCommand()
	if c.Name() != "supervise-pool" {
		t.Errorf("command name = %q, want %q", c.Name(), "supervise-pool")
	}
	if !c.Hidden {
		t.Error("supervise-pool command is not hidden")
	}
}

// startDetachedPool spawns this test binary as `supervise-pool <dir>`, sends the
// spec, and decodes the ack. It mirrors the server's spawn path so signal tests
// have a real process to signal.
func startDetachedPool(t *testing.T, dir string, spec *PoolSpec) (*exec.Cmd, SuperviseAck) {
	t.Helper()

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshaling spec: %v", err)
	}

	sup := exec.Command(os.Args[0], "supervise-pool", dir)
	sup.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	stdin, err := sup.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := sup.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := sup.Start(); err != nil {
		t.Fatalf("starting pool supervisor: %v", err)
	}

	if _, err := stdin.Write(data); err != nil {
		t.Fatalf("writing spec: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatalf("closing spec pipe: %v", err)
	}

	var ack SuperviseAck
	if err := json.NewDecoder(stdout).Decode(&ack); err != nil {
		t.Fatalf("decoding ack: %v", err)
	}
	if !ack.Started || ack.Pid != sup.Process.Pid {
		t.Fatalf("ack = %+v, want started with pid %d", ack, sup.Process.Pid)
	}
	return sup, ack
}

// waitForRunning polls the manifest until run index job reports running, then
// returns its run ID.
func waitForRunning(t *testing.T, dir string, job int) string {
	t.Helper()

	var runID string
	waitFor(t, fmt.Sprintf("run %d to be running", job), func() bool {
		m, err := ReadPoolManifest(dir)
		if err != nil || len(m.Runs) <= job {
			return false
		}
		if m.Runs[job].Status != PoolRunRunning {
			return false
		}
		runID = m.Runs[job].RunID
		return true
	})
	return runID
}

func TestSupervisePoolSignalStopScheduling(t *testing.T) {
	_, dir := newPoolTestDir(t)

	spec := poolSpec(t,
		[]string{"sh", "-c", "sleep 0.5; echo done"},
		[]string{"echo", "never"},
	)

	sup, _ := startDetachedPool(t, dir, spec)
	waitForRunning(t, dir, 0)

	if err := sup.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if err := sup.Wait(); err != nil {
		t.Fatalf("pool supervisor exit: %v", err)
	}

	m := readManifest(t, dir)
	if m.FinishedAt == nil {
		t.Error("manifest has no finished_at, want complete")
	}
	if m.Runs[0].Status != PoolRunFinished || m.Runs[0].Signal != nil {
		t.Errorf("in-flight run = %+v, want finished without a signal", m.Runs[0])
	}
	if m.Runs[1].Status != PoolRunSkipped {
		t.Errorf("pending run status = %q, want skipped", m.Runs[1].Status)
	}
	if !RunLockReleased(dir) {
		t.Error("pool lock still held after exit")
	}
}

func TestSupervisePoolSignalKillEverything(t *testing.T) {
	_, dir := newPoolTestDir(t)

	spec := poolSpec(t,
		[]string{"sleep", "30"},
		[]string{"echo", "never"},
	)

	sup, _ := startDetachedPool(t, dir, spec)
	waitForRunning(t, dir, 0)

	if err := sup.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("SIGINT: %v", err)
	}
	if err := sup.Wait(); err != nil {
		t.Fatalf("pool supervisor exit: %v", err)
	}

	m := readManifest(t, dir)
	if m.FinishedAt == nil {
		t.Error("manifest has no finished_at, want complete")
	}
	rec := m.Runs[0]
	if rec.Status != PoolRunFinished || rec.Signal == nil || *rec.Signal != int(syscall.SIGTERM) {
		t.Errorf("in-flight run = %+v, want finished with SIGTERM", rec)
	}
	if m.Runs[1].Status != PoolRunSkipped {
		t.Errorf("pending run status = %q, want skipped", m.Runs[1].Status)
	}
}

func TestSupervisePoolAbandonedOnSIGKILL(t *testing.T) {
	_, dir := newPoolTestDir(t)

	spec := poolSpec(t, []string{"sleep", "30"})

	sup, _ := startDetachedPool(t, dir, spec)
	runID := waitForRunning(t, dir, 0)

	// The orphaned member outlives the pool supervisor; kill it through the
	// production cancel path and wait for its own supervisor to finish, so the
	// test's TMPDIR teardown does not race member bookkeeping.
	t.Cleanup(func() {
		_, _ = CancelRun(context.Background(), runID, CancelOptions{Signal: "SIGKILL"})
		waitFor(t, "member bookkeeping", func() bool {
			_, err := ReadMeta(filepath.Join(CaptureRoot(), runID))
			return err == nil
		})
	})

	if err := sup.Process.Kill(); err != nil {
		t.Fatalf("SIGKILL: %v", err)
	}
	_ = sup.Wait()

	if !RunLockReleased(dir) {
		t.Error("pool lock still held after SIGKILL")
	}

	m := readManifest(t, dir)
	if m.FinishedAt != nil {
		t.Error("manifest has finished_at, want incomplete: the abandoned state")
	}
	if m.Runs[0].Status != PoolRunRunning {
		t.Errorf("member status = %q, want the stale running record", m.Runs[0].Status)
	}
}
