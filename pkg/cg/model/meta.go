package model

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
	RunInfo
	FinishedAt  time.Time `json:"finished_at"`
	DurationMs  int64     `json:"duration_ms"`
	ExitCode    int       `json:"exit_code"`
	Signal      *int      `json:"signal"`
	StdoutLines int64     `json:"stdout_lines"`
	StderrLines int64     `json:"stderr_lines"`
	Usage       *Usage    `json:"usage,omitempty"`
}

// WriteMeta serialises m and writes it atomically to dir/meta.json.
func WriteMeta(dir string, m *Meta) error {
	if err := writeFileAtomic(dir, MetaFilename, m); err != nil {
		return fmt.Errorf("meta: %w", err)
	}
	return nil
}

// writeFileAtomic serialises v as indented JSON and writes it atomically to
// dir/filename via a temporary file and rename. The temporary file is created
// in the same directory so the rename stays on one filesystem.
func writeFileAtomic(dir, filename string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", filename, err)
	}

	tmp, err := os.CreateTemp(dir, filename+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating %s tmpfile: %w", filename, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing %s tmpfile: %w", filename, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing %s tmpfile: %w", filename, err)
	}

	if err := os.Rename(tmpPath, filepath.Join(dir, filename)); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming %s tmpfile: %w", filename, err)
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
