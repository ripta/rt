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

func TestHandleListOrderAndSkipIncomplete(t *testing.T) {
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
	// Incomplete: dir only, no meta.json. Must be skipped.
	seedRunDir(t, "CCCCCC", nil)
	// Non-Crockford dir without meta.json. Must be skipped.
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
	if out.Runs[1].ID != "BBBBBB" {
		t.Errorf("Runs[1].ID = %q, want BBBBBB", out.Runs[1].ID)
	}
	if out.Runs[0].DurationMs != 12 {
		t.Errorf("Runs[0].DurationMs = %d, want 12", out.Runs[0].DurationMs)
	}
	if out.Runs[1].ExitCode != 2 {
		t.Errorf("Runs[1].ExitCode = %d, want 2", out.Runs[1].ExitCode)
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
