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
		RunInfo:     cg.RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}},
		ExitCode:    -1,
		Signal:      &sig,
		DurationMs:  12,
		StdoutLines: 1,
		Usage:       &cg.Usage{Source: cg.UsageSourceRusageChildren, UserUS: 8000, SystemUS: 3000},
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
	if out.Usage == nil || out.Usage.Source != cg.UsageSourceRusageChildren || out.Usage.UserUS != 8000 {
		t.Errorf("Usage = %+v, want source=%s user_us=8000", out.Usage, cg.UsageSourceRusageChildren)
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
	if out.Usage != nil {
		t.Errorf("Usage = %v, want nil for in-flight", out.Usage)
	}
}

func TestHandleMetaInFlightWithStartInfo(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	dir := seedRunDir(t, "AAAAAA", nil)
	if err := cg.WriteStartInfo(dir, &cg.StartInfo{RunInfo: cg.RunInfo{Command: []string{"sleep", "9"}, Cwd: "/work"}}); err != nil {
		t.Fatalf("WriteStartInfo: %v", err)
	}

	_, out, err := handleMeta(context.Background(), nil, metaInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleMeta: %v", err)
	}
	if out.State != "running" {
		t.Errorf("State = %q, want running", out.State)
	}
	if out.Cwd != "/work" {
		t.Errorf("Cwd = %q, want /work from start.json", out.Cwd)
	}
	if len(out.Command) != 2 || out.Command[0] != "sleep" {
		t.Errorf("Command = %v, want start.json command", out.Command)
	}
}
