package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg"
)

func TestHandleMetaSuccess(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	sig := 15
	seedRunDir(t, "AAAAAA", &cg.Meta{
		ID:          "AAAAAA",
		Command:     []string{"echo", "hi"},
		ExitCode:    -1,
		Signal:      &sig,
		DurationMs:  12,
		StdoutLines: 1,
	})

	_, out, err := handleMeta(context.Background(), nil, metaInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleMeta: %v", err)
	}
	if out.ID != "AAAAAA" {
		t.Errorf("ID = %q, want AAAAAA", out.ID)
	}
	if out.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", out.ExitCode)
	}
	if out.Signal == nil || *out.Signal != 15 {
		t.Errorf("Signal = %v, want 15", out.Signal)
	}
	if out.StdoutLines != 1 {
		t.Errorf("StdoutLines = %d, want 1", out.StdoutLines)
	}
}

func TestHandleMetaUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleMeta(context.Background(), nil, metaInput{ID: "ZZZZZZ"})
	if err == nil {
		t.Fatalf("expected error for unknown ID")
	}
	if !strings.Contains(err.Error(), "unknown run id: ZZZZZZ") {
		t.Errorf("error = %q, want to contain unknown run id message", err.Error())
	}
}

func TestHandleMetaInvalidID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleMeta(context.Background(), nil, metaInput{ID: "lowercase"})
	if err == nil {
		t.Fatalf("expected error for invalid ID format")
	}
	if !strings.Contains(err.Error(), "unknown run id") {
		t.Errorf("error = %q, want unknown run id message", err.Error())
	}
}

func TestHandleMetaIncompleteRun(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedRunDir(t, "AAAAAA", nil)

	_, _, err := handleMeta(context.Background(), nil, metaInput{ID: "AAAAAA"})
	if err == nil {
		t.Fatalf("expected error for incomplete run")
	}
	if !strings.Contains(err.Error(), "incomplete run") {
		t.Errorf("error = %q, want incomplete run message", err.Error())
	}
}
