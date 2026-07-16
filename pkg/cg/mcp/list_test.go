package mcp

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/ripta/rt/pkg/cg"
)

// seedRunDir creates $TMPDIR/cg/<id>/ with empty stdout and stderr files. When
// meta is non-nil it is written to meta.json so the run looks complete.
func seedRunDir(t *testing.T, id string, meta *cg.Meta) string {
	t.Helper()
	dir := filepath.Join(cg.CaptureRoot(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	for _, name := range []string{"stdout", "stderr"} {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		f.Close()
	}
	if meta != nil {
		if err := cg.WriteMeta(dir, meta); err != nil {
			t.Fatalf("WriteMeta: %v", err)
		}
	}
	return dir
}

// seedLockFile creates dir/lock without holding the flock, matching a run whose
// supervisor has died.
func seedLockFile(t *testing.T, dir string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, cg.LockFilename))
	if err != nil {
		t.Fatalf("creating lock file: %v", err)
	}
	f.Close()
}

// holdRunLock takes the exclusive flock a live supervisor would hold and releases
// it on test cleanup.
func holdRunLock(t *testing.T, dir string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, cg.LockFilename), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("opening lock file: %v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		t.Fatalf("holding run lock: %v", err)
	}
	t.Cleanup(func() { f.Close() })
}

func TestHandleListEmpty(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleList(context.Background(), nil, listInput{})
	if err != nil {
		t.Fatalf("handleList: %v", err)
	}
	if len(out.Runs) != 0 {
		t.Errorf("expected 0 runs, got %d", len(out.Runs))
	}
}

func TestHandleListDefaultsToFinished(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	dirNew := seedRunDir(t, "AAAAAA", &cg.Meta{
		RunInfo:    cg.RunInfo{ID: "AAAAAA", Command: []string{"echo", "new"}},
		DurationMs: 12,
	})
	dirOld := seedRunDir(t, "BBBBBB", &cg.Meta{
		RunInfo:    cg.RunInfo{ID: "BBBBBB", Command: []string{"echo", "old"}},
		ExitCode:   2,
		DurationMs: 1234,
	})
	// Incomplete: dir only, no meta.json. Must be skipped under the default.
	seedRunDir(t, "CCCCCC", nil)
	// Non-Crockford dir without meta.json. Must be skipped under every filter.
	if err := os.MkdirAll(filepath.Join(cg.CaptureRoot(), "lowercase"), 0o755); err != nil {
		t.Fatalf("mkdir junk: %v", err)
	}

	now := time.Now()
	if err := os.Chtimes(dirNew, now, now); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}
	if err := os.Chtimes(dirOld, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}

	_, out, err := handleList(context.Background(), nil, listInput{})
	if err != nil {
		t.Fatalf("handleList: %v", err)
	}
	if len(out.Runs) != 2 {
		t.Fatalf("expected 2 runs, got %d: %+v", len(out.Runs), out.Runs)
	}
	if out.Runs[0].ID != "AAAAAA" {
		t.Errorf("Runs[0].ID = %q, want AAAAAA", out.Runs[0].ID)
	}
	if out.Runs[0].State != "finished" {
		t.Errorf("Runs[0].State = %q, want finished", out.Runs[0].State)
	}
	if out.Runs[1].ID != "BBBBBB" {
		t.Errorf("Runs[1].ID = %q, want BBBBBB", out.Runs[1].ID)
	}
	if out.Runs[0].DurationMs == nil || *out.Runs[0].DurationMs != 12 {
		t.Errorf("Runs[0].DurationMs = %v, want 12", out.Runs[0].DurationMs)
	}
	if out.Runs[1].ExitCode == nil || *out.Runs[1].ExitCode != 2 {
		t.Errorf("Runs[1].ExitCode = %v, want 2", out.Runs[1].ExitCode)
	}
}

func TestHandleListStateAll(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	dirFin := seedRunDir(t, "AAAAAA", &cg.Meta{
		RunInfo:    cg.RunInfo{ID: "AAAAAA", Command: []string{"echo", "done"}},
		DurationMs: 7,
	})
	dirRun := seedRunDir(t, "CCCCCC", nil)

	now := time.Now()
	if err := os.Chtimes(dirFin, now, now); err != nil {
		t.Fatalf("chtimes fin: %v", err)
	}
	runMtime := now.Add(-30 * time.Minute)
	if err := os.Chtimes(dirRun, runMtime, runMtime); err != nil {
		t.Fatalf("chtimes run: %v", err)
	}

	_, out, err := handleList(context.Background(), nil, listInput{State: "all"})
	if err != nil {
		t.Fatalf("handleList: %v", err)
	}
	if len(out.Runs) != 2 {
		t.Fatalf("expected 2 runs, got %d: %+v", len(out.Runs), out.Runs)
	}
	if out.Runs[0].ID != "AAAAAA" || out.Runs[0].State != "finished" {
		t.Errorf("Runs[0] = %+v, want AAAAAA/finished", out.Runs[0])
	}
	if out.Runs[1].ID != "CCCCCC" || out.Runs[1].State != "running" {
		t.Errorf("Runs[1] = %+v, want CCCCCC/running", out.Runs[1])
	}
	r := out.Runs[1]
	if r.Command != nil {
		t.Errorf("in-flight Command = %v, want nil", r.Command)
	}
	if r.FinishedAt != nil {
		t.Errorf("in-flight FinishedAt = %v, want nil", r.FinishedAt)
	}
	if r.DurationMs != nil {
		t.Errorf("in-flight DurationMs = %v, want nil", r.DurationMs)
	}
	if r.ExitCode != nil {
		t.Errorf("in-flight ExitCode = %v, want nil", r.ExitCode)
	}
	if r.StdoutLines != nil || r.StderrLines != nil {
		t.Errorf("in-flight line counts = %v/%v, want nil", r.StdoutLines, r.StderrLines)
	}
	if r.StartedAt == nil {
		t.Fatalf("in-flight StartedAt = nil, want mtime")
	}
	if !r.StartedAt.Equal(runMtime) {
		t.Errorf("in-flight StartedAt = %v, want %v", *r.StartedAt, runMtime)
	}
}

func TestHandleListStateRunning(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedRunDir(t, "AAAAAA", &cg.Meta{RunInfo: cg.RunInfo{ID: "AAAAAA", Command: []string{"echo", "done"}}})
	seedRunDir(t, "CCCCCC", nil)

	_, out, err := handleList(context.Background(), nil, listInput{State: "running"})
	if err != nil {
		t.Fatalf("handleList: %v", err)
	}
	if len(out.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d: %+v", len(out.Runs), out.Runs)
	}
	if out.Runs[0].ID != "CCCCCC" || out.Runs[0].State != "running" {
		t.Errorf("Runs[0] = %+v, want CCCCCC/running", out.Runs[0])
	}
}

func TestHandleListRunningReadsStartInfo(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	dir := seedRunDir(t, "CCCCCC", nil)
	started := time.Now().Add(-2 * time.Minute).UTC()
	if err := cg.WriteStartInfo(dir, &cg.StartInfo{RunInfo: cg.RunInfo{Command: []string{"sleep", "30"}, StartedAt: started}}); err != nil {
		t.Fatalf("WriteStartInfo: %v", err)
	}

	_, out, err := handleList(context.Background(), nil, listInput{State: "running"})
	if err != nil {
		t.Fatalf("handleList: %v", err)
	}
	if len(out.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d: %+v", len(out.Runs), out.Runs)
	}
	r := out.Runs[0]
	if want := []string{"sleep", "30"}; len(r.Command) != 2 || r.Command[0] != want[0] || r.Command[1] != want[1] {
		t.Errorf("running Command = %v, want %v", r.Command, want)
	}
	if r.StartedAt == nil || !r.StartedAt.Equal(started) {
		t.Errorf("running StartedAt = %v, want %v", r.StartedAt, started)
	}
}

func TestHandleListStateAbandoned(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedRunDir(t, "AAAAAA", &cg.Meta{RunInfo: cg.RunInfo{ID: "AAAAAA", Command: []string{"echo", "done"}}})

	// In flight on the shell path: no meta.json and no lock file.
	seedRunDir(t, "DDDDDD", nil)

	// Abandoned: the lock file exists but nothing holds it, and start.json
	// survives from before the supervisor died.
	dirAband := seedRunDir(t, "CCCCCC", nil)
	seedLockFile(t, dirAband)
	started := time.Now().Add(-5 * time.Minute).UTC()
	if err := cg.WriteStartInfo(dirAband, &cg.StartInfo{RunInfo: cg.RunInfo{Command: []string{"sleep", "600"}, StartedAt: started}}); err != nil {
		t.Fatalf("WriteStartInfo: %v", err)
	}

	_, out, err := handleList(context.Background(), nil, listInput{State: "abandoned"})
	if err != nil {
		t.Fatalf("handleList: %v", err)
	}
	if len(out.Runs) != 1 {
		t.Fatalf("expected 1 abandoned run, got %d: %+v", len(out.Runs), out.Runs)
	}
	r := out.Runs[0]
	if r.ID != "CCCCCC" || r.State != "abandoned" {
		t.Errorf("Runs[0] = %+v, want CCCCCC/abandoned", r)
	}
	if want := []string{"sleep", "600"}; len(r.Command) != 2 || r.Command[0] != want[0] || r.Command[1] != want[1] {
		t.Errorf("abandoned Command = %v, want %v", r.Command, want)
	}
	if r.StartedAt == nil || !r.StartedAt.Equal(started) {
		t.Errorf("abandoned StartedAt = %v, want %v", r.StartedAt, started)
	}

	_, out, err = handleList(context.Background(), nil, listInput{State: "running"})
	if err != nil {
		t.Fatalf("handleList running: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "DDDDDD" {
		t.Fatalf("running filter = %+v, want just DDDDDD", out.Runs)
	}

	_, out, err = handleList(context.Background(), nil, listInput{State: "all"})
	if err != nil {
		t.Fatalf("handleList all: %v", err)
	}
	if len(out.Runs) != 3 {
		t.Errorf("all filter returned %d runs, want 3: %+v", len(out.Runs), out.Runs)
	}
}

func TestHandleListHeldLockListsRunning(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	dir := seedRunDir(t, "CCCCCC", nil)
	holdRunLock(t, dir)

	_, out, err := handleList(context.Background(), nil, listInput{State: "abandoned"})
	if err != nil {
		t.Fatalf("handleList abandoned: %v", err)
	}
	if len(out.Runs) != 0 {
		t.Errorf("held lock listed as abandoned: %+v", out.Runs)
	}

	_, out, err = handleList(context.Background(), nil, listInput{State: "running"})
	if err != nil {
		t.Fatalf("handleList running: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "CCCCCC" || out.Runs[0].State != "running" {
		t.Errorf("running filter = %+v, want CCCCCC/running", out.Runs)
	}
}

func TestHandleListInvalidState(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleList(context.Background(), nil, listInput{State: "bogus"})
	if err == nil {
		t.Fatalf("handleList: expected error, got nil; out=%+v", out)
	}
	if len(out.Runs) != 0 {
		t.Errorf("expected zero runs on error, got %d", len(out.Runs))
	}
}

func TestHandleListSkipsInvalidIDs(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(cg.CaptureRoot(), "lowercase"), 0o755); err != nil {
		t.Fatalf("mkdir junk: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cg.CaptureRoot(), "ABC"), 0o755); err != nil {
		t.Fatalf("mkdir short: %v", err)
	}

	_, out, err := handleList(context.Background(), nil, listInput{State: "all"})
	if err != nil {
		t.Fatalf("handleList: %v", err)
	}
	if len(out.Runs) != 0 {
		t.Errorf("expected 0 runs, got %d: %+v", len(out.Runs), out.Runs)
	}
}

func TestHandleListLimit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	dirA := seedRunDir(t, "AAAAAA", &cg.Meta{RunInfo: cg.RunInfo{ID: "AAAAAA", Command: []string{"echo", "a"}}})
	dirB := seedRunDir(t, "BBBBBB", &cg.Meta{RunInfo: cg.RunInfo{ID: "BBBBBB", Command: []string{"echo", "b"}}})

	now := time.Now()
	if err := os.Chtimes(dirA, now, now); err != nil {
		t.Fatalf("chtimes a: %v", err)
	}
	if err := os.Chtimes(dirB, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("chtimes b: %v", err)
	}

	_, out, err := handleList(context.Background(), nil, listInput{Limit: 1})
	if err != nil {
		t.Fatalf("handleList: %v", err)
	}
	if len(out.Runs) != 1 {
		t.Fatalf("expected 1 run with Limit=1, got %d", len(out.Runs))
	}
	if out.Runs[0].ID != "AAAAAA" {
		t.Errorf("Runs[0].ID = %q, want most-recent AAAAAA", out.Runs[0].ID)
	}
}

func TestHandleListLimitClampedToMax(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleList(context.Background(), nil, listInput{Limit: 1 << 30})
	if err != nil {
		t.Fatalf("handleList: %v", err)
	}
	if len(out.Runs) != 0 {
		t.Errorf("expected 0 runs (empty root), got %d", len(out.Runs))
	}
}

// seedFinishedPool seeds a finished pool with two finished members, one green
// and one failed, plus the member run dirs whose meta names the pool.
func seedFinishedPool(t *testing.T, poolID, okID, badID string) *cg.PoolManifest {
	t.Helper()
	finished := time.Now().UTC()
	m := &cg.PoolManifest{
		ID:         poolID,
		Commands:   [][]string{{"echo", "hi"}},
		StartedAt:  finished.Add(-time.Minute),
		FinishedAt: &finished,
		Runs: []cg.PoolRunRecord{
			{Command: 0, RunID: okID, Status: cg.PoolRunFinished, ExitCode: intp(0)},
			{Command: 0, RunID: badID, Status: cg.PoolRunFinished, ExitCode: intp(1)},
		},
	}
	seedPoolDir(t, poolID, m)
	seedRunDir(t, okID, &cg.Meta{RunInfo: cg.RunInfo{ID: okID, Command: []string{"echo", "hi"}, Pool: poolID}})
	seedRunDir(t, badID, &cg.Meta{RunInfo: cg.RunInfo{ID: badID, Command: []string{"echo", "hi"}, Pool: poolID}, ExitCode: 1})
	return m
}

func intp(v int) *int {
	return &v
}

func TestHandleListCollapsesPools(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedFinishedPool(t, "PPPPPP", "AAAAAA", "BBBBBB")
	seedRunDir(t, "SSSSSS", &cg.Meta{RunInfo: cg.RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})

	_, out, err := handleList(context.Background(), nil, listInput{})
	if err != nil {
		t.Fatalf("handleList: %v", err)
	}
	if len(out.Runs) != 2 {
		t.Fatalf("expected pool row + standalone row, got %d: %+v", len(out.Runs), out.Runs)
	}

	var pool *listRun
	for i := range out.Runs {
		if out.Runs[i].Kind == "pool" {
			pool = &out.Runs[i]
		} else if out.Runs[i].ID != "SSSSSS" {
			t.Errorf("unexpected non-pool row %+v", out.Runs[i])
		}
	}
	if pool == nil {
		t.Fatalf("no pool row in %+v", out.Runs)
	}
	if pool.ID != "PPPPPP" || pool.State != "finished" {
		t.Errorf("pool row = %+v, want PPPPPP/finished", pool)
	}
	if pool.Counts == nil || *pool.Counts != (cg.PoolCounts{Total: 2, Succeeded: 1, Failed: 1}) {
		t.Errorf("pool counts = %+v, want total 2, 1 ok, 1 failed", pool.Counts)
	}
	if pool.FinishedAt == nil {
		t.Errorf("pool row FinishedAt = nil, want manifest finished_at")
	}
}

func TestHandleListPoolStates(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	running := seedPoolDir(t, "QQQQQQ", &cg.PoolManifest{
		ID:        "QQQQQQ",
		Commands:  [][]string{{"sleep", "60"}},
		StartedAt: time.Now().UTC(),
		Runs:      []cg.PoolRunRecord{{Command: 0, Status: cg.PoolRunRunning, RunID: "AAAAAA"}},
	})
	holdRunLock(t, running)

	abandoned := seedPoolDir(t, "XXXXXX", &cg.PoolManifest{
		ID:        "XXXXXX",
		Commands:  [][]string{{"sleep", "60"}},
		StartedAt: time.Now().UTC(),
		Runs:      []cg.PoolRunRecord{{Command: 0, Status: cg.PoolRunRunning}},
	})
	seedLockFile(t, abandoned)

	// The default state filter is finished, so neither pool surfaces.
	_, out, err := handleList(context.Background(), nil, listInput{})
	if err != nil {
		t.Fatalf("handleList default: %v", err)
	}
	if len(out.Runs) != 0 {
		t.Errorf("default filter listed unfinished pools: %+v", out.Runs)
	}

	_, out, err = handleList(context.Background(), nil, listInput{State: "running"})
	if err != nil {
		t.Fatalf("handleList running: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "QQQQQQ" || out.Runs[0].Kind != "pool" {
		t.Errorf("running filter = %+v, want QQQQQQ pool row", out.Runs)
	}

	_, out, err = handleList(context.Background(), nil, listInput{State: "abandoned"})
	if err != nil {
		t.Fatalf("handleList abandoned: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "XXXXXX" || out.Runs[0].State != "abandoned" {
		t.Errorf("abandoned filter = %+v, want XXXXXX pool row", out.Runs)
	}
}

func TestHandleListPoolMembers(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedFinishedPool(t, "PPPPPP", "AAAAAA", "BBBBBB")

	// An in-flight member: no meta.json yet, start.json names the pool. Member
	// mode must default the state filter to all so this row is not hidden.
	dirRun := seedRunDir(t, "CCCCCC", nil)
	if err := cg.WriteStartInfo(dirRun, &cg.StartInfo{RunInfo: cg.RunInfo{Command: []string{"sleep", "30"}, StartedAt: time.Now().UTC(), Pool: "PPPPPP"}}); err != nil {
		t.Fatalf("WriteStartInfo: %v", err)
	}

	seedRunDir(t, "SSSSSS", &cg.Meta{RunInfo: cg.RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})

	_, out, err := handleList(context.Background(), nil, listInput{Pool: "PPPPPP"})
	if err != nil {
		t.Fatalf("handleList members: %v", err)
	}
	if len(out.Runs) != 3 {
		t.Fatalf("expected 3 member rows, got %d: %+v", len(out.Runs), out.Runs)
	}
	for _, r := range out.Runs {
		if r.Pool != "PPPPPP" {
			t.Errorf("member row %s Pool = %q, want PPPPPP", r.ID, r.Pool)
		}
		if r.Kind != "" {
			t.Errorf("member row %s Kind = %q, want empty", r.ID, r.Kind)
		}
	}

	_, out, err = handleList(context.Background(), nil, listInput{Pool: "PPPPPP", State: "running"})
	if err != nil {
		t.Fatalf("handleList members running: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "CCCCCC" {
		t.Errorf("explicit state filter = %+v, want just CCCCCC", out.Runs)
	}
}

func TestHandleListPoolNoneAndAny(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedFinishedPool(t, "PPPPPP", "AAAAAA", "BBBBBB")
	seedRunDir(t, "SSSSSS", &cg.Meta{RunInfo: cg.RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})

	_, out, err := handleList(context.Background(), nil, listInput{Pool: "none"})
	if err != nil {
		t.Fatalf("handleList none: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "SSSSSS" {
		t.Errorf("pool none = %+v, want just SSSSSS", out.Runs)
	}

	_, out, err = handleList(context.Background(), nil, listInput{Pool: "any"})
	if err != nil {
		t.Fatalf("handleList any: %v", err)
	}
	if len(out.Runs) != 4 {
		t.Errorf("pool any listed %d rows, want 4 (pool + 2 members + standalone): %+v", len(out.Runs), out.Runs)
	}
}

func TestHandleListOrphanMemberIsStandalone(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	// A member whose pool dir is gone: it must stay visible as standalone.
	seedRunDir(t, "AAAAAA", &cg.Meta{RunInfo: cg.RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, Pool: "PPPPPP"}})

	_, out, err := handleList(context.Background(), nil, listInput{})
	if err != nil {
		t.Fatalf("handleList default: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "AAAAAA" {
		t.Errorf("default filter = %+v, want orphan AAAAAA visible", out.Runs)
	}

	_, out, err = handleList(context.Background(), nil, listInput{Pool: "none"})
	if err != nil {
		t.Fatalf("handleList none: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "AAAAAA" {
		t.Errorf("pool none = %+v, want orphan AAAAAA visible", out.Runs)
	}
}

func TestHandleListLimitCountsCollapsedRows(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedFinishedPool(t, "PPPPPP", "AAAAAA", "BBBBBB")
	dirSolo := seedRunDir(t, "SSSSSS", &cg.Meta{RunInfo: cg.RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})

	now := time.Now()
	if err := os.Chtimes(dirSolo, now, now); err != nil {
		t.Fatalf("chtimes solo: %v", err)
	}

	_, out, err := handleList(context.Background(), nil, listInput{Limit: 1})
	if err != nil {
		t.Fatalf("handleList: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "SSSSSS" {
		t.Errorf("limit 1 = %+v, want just most-recent SSSSSS", out.Runs)
	}
}

func TestHandleListInvalidPool(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleList(context.Background(), nil, listInput{Pool: "not-an-id"})
	if err == nil {
		t.Fatalf("handleList: expected error, got nil; out=%+v", out)
	}
}

func TestHandleListUnknownPoolID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	_, out, err := handleList(context.Background(), nil, listInput{Pool: "ZZZZZZ"})
	if err == nil {
		t.Fatalf("handleList: expected unknown pool error, got nil; out=%+v", out)
	}
}
