package cg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteMetaRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sig := 15
	want := &Meta{
		ID:          "Q3F9K2",
		Command:     []string{"echo", "hi"},
		StartedAt:   time.Date(2026, 6, 6, 19, 25, 6, int(time.Millisecond), time.UTC),
		FinishedAt:  time.Date(2026, 6, 6, 19, 25, 6, int(13*time.Millisecond), time.UTC),
		DurationMs:  12,
		ExitCode:    0,
		Signal:      &sig,
		StdoutLines: 1,
		StderrLines: 0,
	}
	if err := WriteMeta(dir, want); err != nil {
		t.Fatalf("WriteMeta() error = %v", err)
	}

	got, err := ReadMeta(dir)
	if err != nil {
		t.Fatalf("ReadMeta() error = %v", err)
	}
	if got.ID != want.ID || got.DurationMs != want.DurationMs || got.ExitCode != want.ExitCode ||
		got.StdoutLines != want.StdoutLines || got.StderrLines != want.StderrLines {
		t.Errorf("scalar fields mismatch:\n got=%+v\nwant=%+v", got, want)
	}
	if len(got.Command) != len(want.Command) || got.Command[0] != "echo" || got.Command[1] != "hi" {
		t.Errorf("Command = %v, want %v", got.Command, want.Command)
	}
	if got.Signal == nil || *got.Signal != sig {
		t.Errorf("Signal = %v, want %d", got.Signal, sig)
	}
	if !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, want.StartedAt)
	}
	if !got.FinishedAt.Equal(want.FinishedAt) {
		t.Errorf("FinishedAt = %v, want %v", got.FinishedAt, want.FinishedAt)
	}
}

func TestWriteMetaSignalNullWhenAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := &Meta{
		ID:         "ABC123",
		Command:    []string{"true"},
		StartedAt:  time.Unix(0, 0).UTC(),
		FinishedAt: time.Unix(0, 0).UTC(),
		ExitCode:   0,
	}
	if err := WriteMeta(dir, m); err != nil {
		t.Fatalf("WriteMeta() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, MetaFilename))
	if err != nil {
		t.Fatalf("reading meta.json: %v", err)
	}
	if !strings.Contains(string(data), `"signal": null`) {
		t.Errorf("meta.json missing explicit null signal: %s", data)
	}

	// JSON must parse and unmarshal back.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Errorf("meta.json not valid JSON: %v", err)
	}
	if raw["signal"] != nil {
		t.Errorf("signal = %v, want nil", raw["signal"])
	}
}

func TestWriteMetaAtomicNoTmpRemains(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := &Meta{ID: "TEST01", Command: []string{"true"}}
	if err := WriteMeta(dir, m); err != nil {
		t.Fatalf("WriteMeta() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == MetaFilename {
			continue
		}
		t.Errorf("stray file in capture dir: %s", e.Name())
	}
}
