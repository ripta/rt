package mcp

import (
	"context"
	"os"
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
		RunInfo:    cg.RunInfo{ID: "AAAAAA", Command: []string{"echo", "done"}},
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
			RunInfo:    cg.RunInfo{ID: "AAAAAA", Command: []string{"echo", "fp"}},
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

func TestHandleWaitFastPathSupervised(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	reg := newRunRegistry()

	async := false
	_, started, err := handleRun(context.Background(), reg, nil, nil, runInput{
		Command: []string{"sh", "-c", "sleep 0.2; echo fp"},
		Wait:    &async,
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !started.Started {
		t.Fatalf("run not started: %+v", started)
	}

	// The registry holds the Done channel driven by supervisor-exit EOF, so
	// the wait takes the in-process path and sees the finished run.
	_, out, err := handleWait(context.Background(), reg, waitInput{ID: started.ID, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if !out.Finished {
		t.Errorf("Finished = false, want true")
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", out.ExitCode)
	}
}

func TestHandleWaitSlowPath(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedRunDir(t, "AAAAAA", nil)

	go func() {
		time.Sleep(250 * time.Millisecond)
		_ = cg.WriteMeta(cg.CaptureRoot()+"/AAAAAA", &cg.Meta{
			RunInfo:    cg.RunInfo{ID: "AAAAAA", Command: []string{"echo", "sp"}},
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

// seedPoolDir writes a pool directory with the given manifest, standing in for
// a pool started by another process.
func seedPoolDir(t *testing.T, id string, m *cg.PoolManifest) string {
	t.Helper()

	dir := cg.CaptureRoot() + "/" + id
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := cg.WritePoolManifest(dir, m); err != nil {
		t.Fatalf("WritePoolManifest: %v", err)
	}
	return dir
}

// runningPoolManifest builds an in-flight manifest with one running member.
func runningPoolManifest(id string) *cg.PoolManifest {
	return &cg.PoolManifest{
		ID:       id,
		Commands: [][]string{{"echo", "member"}},
		Runs:     []cg.PoolRunRecord{{Command: 0, RunID: "BBBBBB", Status: cg.PoolRunRunning}},
	}
}

// finishPoolManifest marks the manifest's single member finished with exit 0
// and stamps finished_at.
func finishPoolManifest(m *cg.PoolManifest) {
	exit := 0
	m.Runs[0].Status = cg.PoolRunFinished
	m.Runs[0].ExitCode = &exit
	now := time.Now().UTC()
	m.FinishedAt = &now
}

func TestHandleWaitPoolAlreadyFinished(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	m := runningPoolManifest("AAAAAA")
	finishPoolManifest(m)
	seedPoolDir(t, "AAAAAA", m)

	_, out, err := handleWait(context.Background(), newRunRegistry(), waitInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if !out.Finished {
		t.Errorf("Finished = false, want true")
	}
	if out.Total != 1 || out.Succeeded != 1 {
		t.Errorf("counts = %+v, want 1/1 (total/succeeded)", out.poolSummary)
	}
}

func TestHandleWaitPoolFastPath(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	m := runningPoolManifest("AAAAAA")
	dir := seedPoolDir(t, "AAAAAA", m)

	reg := newRunRegistry()
	done := make(chan struct{})
	reg.Add("AAAAAA", done)

	// Finish the manifest and close Done as the pool supervisor's exit would.
	go func() {
		time.Sleep(50 * time.Millisecond)
		finishPoolManifest(m)
		if err := cg.WritePoolManifest(dir, m); err != nil {
			t.Errorf("WritePoolManifest: %v", err)
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
	if out.Total != 1 || out.Succeeded != 1 {
		t.Errorf("counts = %+v, want 1/1 (total/succeeded)", out.poolSummary)
	}
	if elapsed >= waitPollInterval {
		t.Errorf("elapsed = %v, want < %v (fast path should beat ticker)", elapsed, waitPollInterval)
	}
}

func TestHandleWaitPoolSlowPath(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	m := runningPoolManifest("AAAAAA")
	dir := seedPoolDir(t, "AAAAAA", m)

	go func() {
		time.Sleep(250 * time.Millisecond)
		finishPoolManifest(m)
		_ = cg.WritePoolManifest(dir, m)
	}()

	_, out, err := handleWait(context.Background(), newRunRegistry(), waitInput{ID: "AAAAAA", TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if !out.Finished {
		t.Errorf("Finished = false, want true")
	}
	if out.Succeeded != 1 {
		t.Errorf("Succeeded = %d, want 1", out.Succeeded)
	}
}

func TestHandleWaitPoolTimeoutPartialSummary(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedPoolDir(t, "AAAAAA", runningPoolManifest("AAAAAA"))

	_, out, err := handleWait(context.Background(), newRunRegistry(), waitInput{ID: "AAAAAA", TimeoutMs: 100})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if out.Finished {
		t.Errorf("Finished = true, want false on timeout")
	}
	if out.Total != 1 || out.Running != 1 {
		t.Errorf("counts = %+v, want one running record in the partial summary", out.poolSummary)
	}
	if len(out.Runs) != 1 || out.Runs[0].Status != cg.PoolRunRunning {
		t.Errorf("Runs = %+v, want the running member visible", out.Runs)
	}
}
