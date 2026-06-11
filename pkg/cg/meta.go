package cg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MetaFilename is the file name within a run directory that holds the
// machine-readable run summary.
const MetaFilename = "meta.json"

// Meta is the per-run metadata persisted alongside the captured stdout and stderr.
type Meta struct {
	ID          string    `json:"id"`
	Command     []string  `json:"command"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	DurationMs  int64     `json:"duration_ms"`
	ExitCode    int       `json:"exit_code"`
	Signal      *int      `json:"signal"`
	StdoutLines int64     `json:"stdout_lines"`
	StderrLines int64     `json:"stderr_lines"`
}

// WriteMeta serialises m and writes it atomically to dir/meta.json via a
// temporary file and rename. The temporary file is created in the same
// directory so the rename stays on one filesystem.
func WriteMeta(dir string, m *Meta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling meta: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "meta.json.tmp-*")
	if err != nil {
		return fmt.Errorf("creating meta tmpfile: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing meta tmpfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing meta tmpfile: %w", err)
	}

	final := filepath.Join(dir, MetaFilename)
	if err := os.Rename(tmpPath, final); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming meta tmpfile: %w", err)
	}
	return nil
}

// ReadMeta loads meta.json from dir.
func ReadMeta(dir string) (*Meta, error) {
	data, err := os.ReadFile(filepath.Join(dir, MetaFilename))
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing meta.json: %w", err)
	}
	return &m, nil
}
