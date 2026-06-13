package mcp

import (
	"context"
	"os"
	"path/filepath"
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
		ID:         "AAAAAA",
		Command:    []string{"echo", "new"},
		DurationMs: 12,
	})
	dirOld := seedRunDir(t, "BBBBBB", &cg.Meta{
		ID:         "BBBBBB",
		Command:    []string{"echo", "old"},
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
		ID:         "AAAAAA",
		Command:    []string{"echo", "done"},
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

	seedRunDir(t, "AAAAAA", &cg.Meta{ID: "AAAAAA", Command: []string{"echo", "done"}})
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
	if err := cg.WriteStartInfo(dir, &cg.StartInfo{Command: []string{"sleep", "30"}, StartedAt: started}); err != nil {
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

	dirA := seedRunDir(t, "AAAAAA", &cg.Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &cg.Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})

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
