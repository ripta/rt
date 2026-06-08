package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg"
)

func TestHandlePathsSuccess(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir := seedRunDir(t, "AAAAAA", &cg.Meta{ID: "AAAAAA", Command: []string{"echo", "hi"}})

	_, out, err := handlePaths(context.Background(), nil, pathsInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handlePaths: %v", err)
	}
	if out.Stdout != filepath.Join(dir, "stdout") {
		t.Errorf("Stdout = %q, want %q", out.Stdout, filepath.Join(dir, "stdout"))
	}
	if out.Stderr != filepath.Join(dir, "stderr") {
		t.Errorf("Stderr = %q, want %q", out.Stderr, filepath.Join(dir, "stderr"))
	}
	if out.Meta != filepath.Join(dir, "meta.json") {
		t.Errorf("Meta = %q, want %q", out.Meta, filepath.Join(dir, "meta.json"))
	}
}

func TestHandlePathsIncompleteRun(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir := seedRunDir(t, "AAAAAA", nil)

	_, out, err := handlePaths(context.Background(), nil, pathsInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handlePaths on incomplete run: %v", err)
	}
	if out.Stdout != filepath.Join(dir, "stdout") {
		t.Errorf("Stdout = %q", out.Stdout)
	}
}

func TestHandlePathsUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handlePaths(context.Background(), nil, pathsInput{ID: "ZZZZZZ"})
	if err == nil {
		t.Fatalf("expected error for unknown ID")
	}
	if !strings.Contains(err.Error(), "unknown run id") {
		t.Errorf("error = %q, want unknown run id message", err.Error())
	}
}
