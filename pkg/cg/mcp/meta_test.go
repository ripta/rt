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
	if out.State != "finished" {
		t.Errorf("State = %q, want finished", out.State)
	}
	if out.ExitCode == nil || *out.ExitCode != -1 {
		t.Errorf("ExitCode = %v, want -1", out.ExitCode)
	}
	if out.Signal == nil || *out.Signal != 15 {
		t.Errorf("Signal = %v, want 15", out.Signal)
	}
	if out.StdoutLines == nil || *out.StdoutLines != 1 {
		t.Errorf("StdoutLines = %v, want 1", out.StdoutLines)
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

func TestHandleMetaInFlight(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedRunDir(t, "AAAAAA", nil)

	_, out, err := handleMeta(context.Background(), nil, metaInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleMeta: %v", err)
	}
	if out.ID != "AAAAAA" {
		t.Errorf("ID = %q, want AAAAAA", out.ID)
	}
	if out.State != "running" {
		t.Errorf("State = %q, want running", out.State)
	}
	if out.Command != nil {
		t.Errorf("Command = %v, want nil for in-flight", out.Command)
	}
	if out.ExitCode != nil {
		t.Errorf("ExitCode = %v, want nil for in-flight", out.ExitCode)
	}
	if out.StartedAt != nil {
		t.Errorf("StartedAt = %v, want nil for in-flight", out.StartedAt)
	}
}
