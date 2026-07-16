package mcp

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ripta/rt/pkg/cg"
)

// spawnRunMain is the body of the spawn-run TestMain dispatch. It stands in
// for an MCP server that started a run and then gets terminated: it spawns the
// supervised child, reports the run ID on stdout, and blocks until killed.
func spawnRunMain(args []string) int {
	run, err := cg.RunSupervised(args, cg.SuperviseOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "RunSupervised: %v\n", err)
		return 1
	}

	fmt.Println(run.ID)
	select {}
}

// startServerRun launches the spawn-run helper with the given child command
// and reads back the run ID. The helper inherits this process's environment,
// so a t.Setenv'd TMPDIR points both processes at the same capture root. A
// cleanup kills the child's process group if the run is still in flight at
// test end.
func startServerRun(t *testing.T, args ...string) (*exec.Cmd, string, string) {
	t.Helper()

	server := exec.Command(os.Args[0], append([]string{"spawn-run"}, args...)...)
	stdout, err := server.StdoutPipe()
	if err != nil {
		t.Fatalf("creating helper stdout pipe: %v", err)
	}
	server.Stderr = os.Stderr

	if err := server.Start(); err != nil {
		t.Fatalf("starting spawn-run helper: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_ = server.Wait()
	})

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("reading run ID from helper: %v", err)
	}
	id := strings.TrimSpace(line)
	dir := filepath.Join(cg.CaptureRoot(), id)

	t.Cleanup(func() {
		if pid, perr := cg.ReadPidFile(dir); perr == nil {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	})

	return server, id, dir
}

// waitStdoutContains polls the run's captured stdout until it contains s,
// failing the test on timeout. Like waitReady, this handshake avoids racing
// the child's startup.
func waitStdoutContains(t *testing.T, dir, s string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(dir, "stdout"))
		if strings.Contains(string(data), s) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("captured stdout never contained %q", s)
}

// spawnPoolMain is the body of the spawn-pool TestMain dispatch, the pool
// analogue of spawnRunMain: it starts a supervised pool of one command
// repeated three times at parallelism 1, reports the pool ID on stdout, and
// blocks until killed. That shape is what the restart test needs: while the
// first run blocks, the other two stay pending.
func spawnPoolMain(args []string) int {
	pc := cg.PoolCommand{Argv: args}
	if resolved, _ := cg.ResolveCommand(args, ""); resolved != nil {
		pc.Resolved = resolved.Resolved
		pc.Canonical = resolved.Canonical
	}

	pool, err := cg.PoolSupervised(&cg.PoolSpec{
		Commands:    []cg.PoolCommand{pc},
		Repeat:      3,
		Parallelism: 1,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "PoolSupervised: %v\n", err)
		return 1
	}

	fmt.Println(pool.ID)
	select {}
}

// startServerPool launches the spawn-pool helper with the given child command
// and reads back the pool ID. Like startServerRun, the helper inherits this
// process's environment, so a t.Setenv'd TMPDIR points every process at the
// same capture root. A cleanup SIGINTs the pool supervisor if the pool is
// still in flight at test end; SIGINT is the kill-everything protocol signal,
// so in-flight members die with it.
func startServerPool(t *testing.T, args ...string) (*exec.Cmd, string, string) {
	t.Helper()

	server := exec.Command(os.Args[0], append([]string{"spawn-pool"}, args...)...)
	stdout, err := server.StdoutPipe()
	if err != nil {
		t.Fatalf("creating helper stdout pipe: %v", err)
	}
	server.Stderr = os.Stderr

	if err := server.Start(); err != nil {
		t.Fatalf("starting spawn-pool helper: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_ = server.Wait()
	})

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("reading pool ID from helper: %v", err)
	}
	id := strings.TrimSpace(line)
	dir := filepath.Join(cg.CaptureRoot(), id)

	t.Cleanup(func() {
		if pid, perr := cg.ReadPidFile(dir); perr == nil {
			_ = syscall.Kill(pid, syscall.SIGINT)
		}
	})

	return server, id, dir
}

// waitPoolRunning polls the pool manifest until run index i is running,
// failing the test on timeout.
func waitPoolRunning(t *testing.T, dir string, i int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m, err := cg.ReadPoolManifest(dir)
		if err == nil && i < len(m.Runs) && m.Runs[i].Status == cg.PoolRunRunning {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pool run %d never reached running", i)
}

// killServer SIGKILLs the spawn-run helper and reaps it, simulating an MCP
// host tearing down the server mid-run.
func killServer(t *testing.T, server *exec.Cmd) {
	t.Helper()
	if err := server.Process.Kill(); err != nil {
		t.Fatalf("killing spawn-run helper: %v", err)
	}
	_ = server.Wait()
}

func TestRestartToleranceWaitAndList(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	stop := filepath.Join(os.TempDir(), "stop")
	script := fmt.Sprintf(`echo ready; while [ ! -e %s ]; do echo tick; sleep 0.05; done`, stop)
	server, id, dir := startServerRun(t, "sh", "-c", script)

	waitStdoutContains(t, dir, "ready", 5*time.Second)
	killServer(t, server)

	// The child must keep running and writing after the server's death: the
	// captured stdout grows past the size recorded at kill time.
	info, err := os.Stat(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatalf("stat captured stdout: %v", err)
	}
	grew := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		after, serr := os.Stat(filepath.Join(dir, "stdout"))
		if serr == nil && after.Size() > info.Size() {
			grew = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !grew {
		t.Fatalf("captured stdout did not grow after server death")
	}

	// A fresh server has an empty registry, so every handler works purely off
	// the run directory.
	reg := newRunRegistry()

	_, lst, err := handleList(context.Background(), nil, listInput{State: "running"})
	if err != nil {
		t.Fatalf("handleList running: %v", err)
	}
	found := false
	for _, r := range lst.Runs {
		if r.ID == id {
			found = true
			if r.State != "running" {
				t.Errorf("State = %q, want running", r.State)
			}
			if len(r.Command) == 0 || r.Command[0] != "sh" {
				t.Errorf("Command = %v, want the sh command from start.json", r.Command)
			}
		}
	}
	if !found {
		t.Fatalf("run %s missing from running list", id)
	}

	if err := os.WriteFile(stop, nil, 0o644); err != nil {
		t.Fatalf("writing stop file: %v", err)
	}

	_, out, err := handleWait(context.Background(), reg, waitInput{ID: id, TimeoutMs: 10000})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if !out.Finished {
		t.Fatalf("Finished = false, want true after the child exits")
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", out.ExitCode)
	}

	m, err := cg.ReadMeta(dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if m.ExitCode != 0 {
		t.Errorf("meta ExitCode = %d, want 0", m.ExitCode)
	}
	if m.StdoutLines == 0 {
		t.Errorf("meta StdoutLines = 0, want > 0")
	}

	_, lst, err = handleList(context.Background(), nil, listInput{State: "finished"})
	if err != nil {
		t.Fatalf("handleList finished: %v", err)
	}
	found = false
	for _, r := range lst.Runs {
		if r.ID == id && r.State == "finished" {
			found = true
		}
	}
	if !found {
		t.Fatalf("run %s missing from finished list", id)
	}
}

func TestRestartToleranceCancel(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	server, id, dir := startServerRun(t, "sh", "-c", "echo ready; sleep 30")

	waitStdoutContains(t, dir, "ready", 5*time.Second)
	killServer(t, server)

	// The canceling process never parented the child: the child's parent is
	// the supervisor, and the process that spawned the supervisor is dead.
	reg := newRunRegistry()

	_, cout, err := handleCancel(context.Background(), reg, cancelInput{ID: id, Signal: "SIGTERM"})
	if err != nil {
		t.Fatalf("handleCancel: %v", err)
	}
	if !cout.Signaled {
		t.Fatalf("Signaled = false, want true")
	}

	_, wout, err := handleWait(context.Background(), reg, waitInput{ID: id, TimeoutMs: 10000})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if !wout.Finished {
		t.Fatalf("Finished = false, want true after cancel")
	}

	m, err := cg.ReadMeta(dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if m.Signal == nil || *m.Signal != int(syscall.SIGTERM) {
		t.Errorf("meta Signal = %v, want %d", m.Signal, int(syscall.SIGTERM))
	}
}

func TestRestartTolerancePool(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	release := filepath.Join(os.TempDir(), "release")
	script := fmt.Sprintf(`while [ ! -e %s ]; do sleep 0.05; done`, release)
	server, id, dir := startServerPool(t, "sh", "-c", script)

	waitPoolRunning(t, dir, 0, 5*time.Second)
	killServer(t, server)

	// The detached supervisor owns scheduling: after the server's death the
	// pool must still be live, with the first run in flight and the rest
	// pending, not abandoned.
	m, err := cg.ReadPoolManifest(dir)
	if err != nil {
		t.Fatalf("ReadPoolManifest: %v", err)
	}
	if len(m.Runs) != 3 {
		t.Fatalf("len(Runs) = %d, want 3", len(m.Runs))
	}
	for i := 1; i < len(m.Runs); i++ {
		if m.Runs[i].Status != cg.PoolRunPending {
			t.Errorf("Runs[%d].Status = %q, want pending", i, m.Runs[i].Status)
		}
	}
	if cg.RunLockReleased(dir) {
		t.Fatalf("pool lock released after server death, want held by the supervisor")
	}

	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatalf("writing release file: %v", err)
	}

	// A fresh server has an empty registry, so the pool wait must take the
	// manifest-polling fallback. The wait itself observes the pending runs
	// getting scheduled and the pool finishing.
	reg := newRunRegistry()

	_, out, err := handleWait(context.Background(), reg, waitInput{ID: id, TimeoutMs: 10000})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if !out.Finished {
		t.Fatalf("Finished = false, want true after the release file lands")
	}
	if out.Total != 3 || out.Succeeded != 3 {
		t.Errorf("Total = %d, Succeeded = %d, want 3 and 3", out.Total, out.Succeeded)
	}
	if len(out.Runs) != 3 {
		t.Fatalf("len(Runs) = %d, want 3", len(out.Runs))
	}
	for i, r := range out.Runs {
		if r.RunID == "" {
			t.Errorf("Runs[%d].RunID is empty, want a member run ID", i)
		}
		if r.Status != cg.PoolRunFinished {
			t.Errorf("Runs[%d].Status = %q, want finished", i, r.Status)
		}
		if r.ExitCode == nil || *r.ExitCode != 0 {
			t.Errorf("Runs[%d].ExitCode = %v, want 0", i, r.ExitCode)
		}
	}

	// Together with the pending check above, a completed manifest proves the
	// last two runs were scheduled after the server died.
	m, err = cg.ReadPoolManifest(dir)
	if err != nil {
		t.Fatalf("ReadPoolManifest after wait: %v", err)
	}
	if m.FinishedAt == nil {
		t.Errorf("FinishedAt = nil, want set after the pool drains")
	}
}
