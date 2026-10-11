package model

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// writeCaptureRun writes a finished run's meta.json under the capture root.
func writeCaptureRun(t *testing.T, id, pool string, argv []string, day int, durationMs int64) {
	t.Helper()

	dir := filepath.Join(CaptureRoot(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating run dir: %v", err)
	}

	meta := &Meta{
		RunInfo: RunInfo{
			ID: id, Command: argv, Cwd: statsFixtureCwd, Pool: pool,
			StartedAt: statsFixtureBase.AddDate(0, 0, day),
		},
		DurationMs: durationMs,
	}
	if err := WriteMeta(dir, meta); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}
}

// writeCapturePool writes a pool manifest under the capture root. Every member succeeded.
func writeCapturePool(t *testing.T, id string, commands [][]string, parallelism int, finished bool) {
	t.Helper()

	dir := filepath.Join(CaptureRoot(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating pool dir: %v", err)
	}

	started := statsFixtureBase
	m := &PoolManifest{
		ID: id, Commands: commands, Repeat: 1, Parallelism: parallelism, Cwd: statsFixtureCwd,
		StartedAt: started,
	}
	for i := range commands {
		m.Runs = append(m.Runs, PoolRunRecord{Command: i, Status: PoolRunFinished, ExitCode: intp(0)})
	}
	if finished {
		end := started.Add(90 * time.Second)
		m.FinishedAt = &end
	}

	if err := WritePoolManifest(dir, m); err != nil {
		t.Fatalf("WritePoolManifest: %v", err)
	}
}

func countLogLines(t *testing.T, h *History) int {
	t.Helper()

	return len(readHistoryIDs(t, h.LogPaths()...))
}

func TestEnsureImported(t *testing.T) {
	h := newFixtureHistory(t)
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("creating capture root: %v", err)
	}

	testCmd := []string{"make", "test"}
	lintCmd := []string{"make", "lint"}

	writeCapturePool(t, "PPPPP1", [][]string{testCmd, lintCmd}, 4, true)
	writeCapturePool(t, "PPPPP2", [][]string{testCmd}, 2, false)
	writeCaptureRun(t, "MMMMM1", "PPPPP1", testCmd, 1, 3000)
	writeCaptureRun(t, "MMMMM2", "PPPPP1", lintCmd, 1, 4000)
	writeCaptureRun(t, "QQQQQ1", "ZZZZZ9", testCmd, 1, 5000)
	writeCaptureRun(t, "SSSSS1", "", testCmd, 0, 1000)
	writeCaptureRun(t, "SSSSS2", "", testCmd, 2, 2000)

	// A run that finished after the first release is in the log already and must not repeat.
	fixtureRun{ID: "SSSSS2", Argv: "make test", Day: 2, DurationMs: 2000}.append(t, h)

	if err := h.EnsureImported(); err != nil {
		t.Fatalf("EnsureImported: %v", err)
	}

	// The prior record, standalone SSSSS1, the two members, and the finished pool. Not the
	// orphan QQQQQ1 and not the unfinished PPPPP2.
	if got := countLogLines(t, h); got != 5 {
		t.Fatalf("log has %d lines, want 5", got)
	}

	result := queryStats(t, h, fixtureQuery("make test"))
	want := []matchShape{
		{StatsMatchExact, "make test", false, 0, 2},
		{StatsMatchExact, "make test", true, 4, 1},
		{StatsMatchPrefix, "make", true, 4, 1},
	}
	if got := shapesOf(result); !slices.Equal(got, want) {
		t.Errorf("matches differ\n got: %+v\nwant: %+v", got, want)
	}

	pool, err := h.PoolWallStats(statsFixtureCwd, [][]string{testCmd, lintCmd}, 1, 4, DefaultStatsSamples)
	if err != nil {
		t.Fatalf("PoolWallStats: %v", err)
	}
	if pool.Count != 1 || pool.MaxMs != 90000 {
		t.Errorf("pool stats = %+v, want the imported 90s pool", pool)
	}
}

func TestEnsureImportedRunsOncePerMarker(t *testing.T) {
	h := newFixtureHistory(t)
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("creating capture root: %v", err)
	}

	cmd := []string{"make", "test"}
	writeCaptureRun(t, "SSSSS1", "", cmd, 0, 1000)

	for range 2 {
		if err := h.EnsureImported(); err != nil {
			t.Fatalf("EnsureImported: %v", err)
		}
		if got := countLogLines(t, h); got != 1 {
			t.Fatalf("log has %d lines, want 1: the import must be idempotent", got)
		}
	}

	writeCaptureRun(t, "SSSSS2", "", cmd, 1, 2000)

	if err := h.EnsureImported(); err != nil {
		t.Fatalf("EnsureImported: %v", err)
	}
	if got := countLogLines(t, h); got != 1 {
		t.Errorf("log has %d lines, want 1: the marker should stop a second import", got)
	}

	if err := os.Remove(filepath.Join(h.Dir(), HistoryImportedFilename)); err != nil {
		t.Fatalf("removing marker: %v", err)
	}

	if err := h.EnsureImported(); err != nil {
		t.Fatalf("EnsureImported: %v", err)
	}
	if got := countLogLines(t, h); got != 2 {
		t.Errorf("log has %d lines, want 2: only the new run imports once the marker is gone", got)
	}
}

func TestEnsureImportedWithoutCaptureRoot(t *testing.T) {
	h := newFixtureHistory(t)

	if err := h.EnsureImported(); err != nil {
		t.Fatalf("EnsureImported: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(h.Dir(), HistoryImportedFilename)); err != nil {
		t.Errorf("marker missing after an import of nothing: %v", err)
	}
}
