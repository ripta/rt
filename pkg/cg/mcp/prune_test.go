package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ripta/rt/pkg/cg"
)

func intPtr(i int) *int { return &i }

func TestHandlePruneEmptyRoot(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handlePrune(context.Background(), nil, pruneInput{})
	if err != nil {
		t.Fatalf("handlePrune: %v", err)
	}
	if len(out.Removed) != 0 {
		t.Errorf("Removed = %v, want empty", out.Removed)
	}
	if out.Removed == nil {
		t.Errorf("Removed is nil, want non-nil empty slice")
	}
}

func TestHandlePruneKeepDefault(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	seedRunDir(t, "AAAAAA", &cg.Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	seedRunDir(t, "BBBBBB", &cg.Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})

	_, out, err := handlePrune(context.Background(), nil, pruneInput{})
	if err != nil {
		t.Fatalf("handlePrune: %v", err)
	}
	if len(out.Removed) != 0 {
		t.Errorf("Removed = %v, want empty (under default keep)", out.Removed)
	}
	for _, id := range []string{"AAAAAA", "BBBBBB"} {
		if _, err := os.Stat(filepath.Join(cg.CaptureRoot(), id)); err != nil {
			t.Errorf("run %s removed unexpectedly: %v", id, err)
		}
	}
}

func TestHandlePruneKeepEvictsOldest(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirA := seedRunDir(t, "AAAAAA", &cg.Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &cg.Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})
	dirC := seedRunDir(t, "CCCCCC", &cg.Meta{ID: "CCCCCC", Command: []string{"echo", "c"}})
	if err := os.Chtimes(dirA, now, now); err != nil {
		t.Fatalf("chtimes a: %v", err)
	}
	if err := os.Chtimes(dirB, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("chtimes b: %v", err)
	}
	if err := os.Chtimes(dirC, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("chtimes c: %v", err)
	}

	_, out, err := handlePrune(context.Background(), nil, pruneInput{Keep: intPtr(1)})
	if err != nil {
		t.Fatalf("handlePrune: %v", err)
	}
	want := []string{"BBBBBB", "CCCCCC"}
	if len(out.Removed) != len(want) || out.Removed[0] != want[0] || out.Removed[1] != want[1] {
		t.Errorf("Removed = %v, want %v", out.Removed, want)
	}
	if _, err := os.Stat(dirA); err != nil {
		t.Errorf("AAAAAA removed unexpectedly: %v", err)
	}
	if _, err := os.Stat(dirB); !os.IsNotExist(err) {
		t.Errorf("BBBBBB still exists: %v", err)
	}
	if _, err := os.Stat(dirC); !os.IsNotExist(err) {
		t.Errorf("CCCCCC still exists: %v", err)
	}
}

func TestHandlePruneDryRun(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirA := seedRunDir(t, "AAAAAA", &cg.Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &cg.Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})
	if err := os.Chtimes(dirA, now, now); err != nil {
		t.Fatalf("chtimes a: %v", err)
	}
	if err := os.Chtimes(dirB, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("chtimes b: %v", err)
	}

	_, out, err := handlePrune(context.Background(), nil, pruneInput{Keep: intPtr(1), DryRun: true})
	if err != nil {
		t.Fatalf("handlePrune: %v", err)
	}
	if len(out.Removed) != 1 || out.Removed[0] != "BBBBBB" {
		t.Errorf("Removed = %v, want [BBBBBB]", out.Removed)
	}
	if !out.DryRun {
		t.Errorf("DryRun = false, want true")
	}
	if _, err := os.Stat(dirB); err != nil {
		t.Errorf("BBBBBB removed despite dry_run: %v", err)
	}
}

func TestHandlePruneOlderThan(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirA := seedRunDir(t, "AAAAAA", &cg.Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &cg.Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})
	dirC := seedRunDir(t, "CCCCCC", &cg.Meta{ID: "CCCCCC", Command: []string{"echo", "c"}})
	if err := os.Chtimes(dirA, now, now); err != nil {
		t.Fatalf("chtimes a: %v", err)
	}
	if err := os.Chtimes(dirB, now.Add(-30*time.Minute), now.Add(-30*time.Minute)); err != nil {
		t.Fatalf("chtimes b: %v", err)
	}
	if err := os.Chtimes(dirC, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("chtimes c: %v", err)
	}

	_, out, err := handlePrune(context.Background(), nil, pruneInput{OlderThan: "1h"})
	if err != nil {
		t.Fatalf("handlePrune: %v", err)
	}
	if len(out.Removed) != 1 || out.Removed[0] != "CCCCCC" {
		t.Errorf("Removed = %v, want [CCCCCC]", out.Removed)
	}
	if _, err := os.Stat(dirA); err != nil {
		t.Errorf("AAAAAA removed unexpectedly: %v", err)
	}
	if _, err := os.Stat(dirB); err != nil {
		t.Errorf("BBBBBB removed unexpectedly: %v", err)
	}
	if _, err := os.Stat(dirC); !os.IsNotExist(err) {
		t.Errorf("CCCCCC still exists: %v", err)
	}
}

func TestHandlePruneOlderThanDaySuffix(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(cg.CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirA := seedRunDir(t, "AAAAAA", &cg.Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &cg.Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})
	if err := os.Chtimes(dirA, now, now); err != nil {
		t.Fatalf("chtimes a: %v", err)
	}
	if err := os.Chtimes(dirB, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour)); err != nil {
		t.Fatalf("chtimes b: %v", err)
	}

	_, out, err := handlePrune(context.Background(), nil, pruneInput{OlderThan: "7d"})
	if err != nil {
		t.Fatalf("handlePrune: %v", err)
	}
	if len(out.Removed) != 1 || out.Removed[0] != "BBBBBB" {
		t.Errorf("Removed = %v, want [BBBBBB]", out.Removed)
	}
}

func TestHandlePruneMutuallyExclusive(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handlePrune(context.Background(), nil, pruneInput{Keep: intPtr(1), OlderThan: "1h"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %q, want to contain 'mutually exclusive'", err)
	}
}

func TestHandlePruneInvalidOlderThan(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handlePrune(context.Background(), nil, pruneInput{OlderThan: "garbage"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid older_than") {
		t.Errorf("err = %q, want to contain 'invalid older_than'", err)
	}
}

func TestHandlePruneSkipsNonRunEntries(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := cg.CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "lowercase"), 0o755); err != nil {
		t.Fatalf("mkdir junk: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	seedRunDir(t, "INCOMP", nil)

	now := time.Now()
	dirA := seedRunDir(t, "AAAAAA", &cg.Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &cg.Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})
	if err := os.Chtimes(dirA, now, now); err != nil {
		t.Fatalf("chtimes a: %v", err)
	}
	if err := os.Chtimes(dirB, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("chtimes b: %v", err)
	}

	_, out, err := handlePrune(context.Background(), nil, pruneInput{Keep: intPtr(1)})
	if err != nil {
		t.Fatalf("handlePrune: %v", err)
	}
	if len(out.Removed) != 1 || out.Removed[0] != "BBBBBB" {
		t.Errorf("Removed = %v, want [BBBBBB]", out.Removed)
	}

	for _, name := range []string{"lowercase", "notes.txt", "INCOMP", "AAAAAA"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s removed unexpectedly: %v", name, err)
		}
	}
	if _, err := os.Stat(dirB); !os.IsNotExist(err) {
		t.Errorf("BBBBBB still exists: %v", err)
	}
}
