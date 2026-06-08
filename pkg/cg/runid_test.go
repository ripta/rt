package cg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateRunID(t *testing.T) {
	t.Parallel()

	for range 100 {
		id, err := generateRunID()
		if err != nil {
			t.Fatalf("generateRunID() error = %v", err)
		}
		if len(id) != runIDLen {
			t.Errorf("len(id) = %d, want %d (id=%q)", len(id), runIDLen, id)
		}
		for _, r := range id {
			if !strings.ContainsRune(runIDAlphabet, r) {
				t.Errorf("id %q contains %q, not in Crockford alphabet", id, r)
			}
		}
	}
}

func TestNewRunDirCreatesUniqueDir(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	id, dir, err := newRunDir(parent)
	if err != nil {
		t.Fatalf("newRunDir() error = %v", err)
	}
	if len(id) != runIDLen {
		t.Errorf("len(id) = %d, want %d", len(id), runIDLen)
	}
	if filepath.Dir(dir) != parent {
		t.Errorf("dir parent = %q, want %q", filepath.Dir(dir), parent)
	}
	if filepath.Base(dir) != id {
		t.Errorf("dir base = %q, want %q", filepath.Base(dir), id)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", dir)
	}
}

func TestNewRunDirRetriesOnCollision(t *testing.T) {
	parent := t.TempDir()

	// Force the first two attempts to collide by returning a fixed ID, then
	// fall back to the real generator.
	collide := "COLIDE"
	if err := os.Mkdir(filepath.Join(parent, collide), 0o755); err != nil {
		t.Fatalf("priming collision dir: %v", err)
	}

	calls := 0
	prev := idSource
	idSource = func() (string, error) {
		calls++
		if calls <= 2 {
			return collide, nil
		}
		return generateRunID()
	}
	t.Cleanup(func() { idSource = prev })

	id, dir, err := newRunDir(parent)
	if err != nil {
		t.Fatalf("newRunDir() error = %v", err)
	}
	if id == collide {
		t.Fatalf("newRunDir returned colliding id %q", collide)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	if calls < 3 {
		t.Errorf("idSource called %d times, want at least 3", calls)
	}
}

func TestNewRunDirExhaustsAttempts(t *testing.T) {
	parent := t.TempDir()

	fixed := "BLOCK1"
	if err := os.Mkdir(filepath.Join(parent, fixed), 0o755); err != nil {
		t.Fatalf("priming collision dir: %v", err)
	}

	prev := idSource
	idSource = func() (string, error) { return fixed, nil }
	t.Cleanup(func() { idSource = prev })

	_, _, err := newRunDir(parent)
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	want := fmt.Sprintf("after %d attempts", runIDAttempts)
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want substring %q", err, want)
	}
}
