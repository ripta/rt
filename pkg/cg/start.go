package cg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StartFilename is the file name within a run directory that records a capture's
// command and start time while it is in flight. It is written when the child
// starts and removed once the run finishes and meta.json supersedes it, so its
// presence, with neither meta.json nor debug.json, marks a run still running.
const StartFilename = "start.json"

// StartInfo is the per-run record written when a capture begins, before the
// child has finished. It lets `cg ls` and cg_list surface the command and a
// precise elapsed time for a run that is still going.
type StartInfo struct {
	Command   []string  `json:"command"`
	StartedAt time.Time `json:"started_at"`
}

// WriteStartInfo serialises s and writes it to dir/start.json.
func WriteStartInfo(dir string, s *StartInfo) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling start info: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, StartFilename), data, 0o644); err != nil {
		return fmt.Errorf("writing start.json: %w", err)
	}
	return nil
}

// ReadStartInfo loads start.json from dir.
func ReadStartInfo(dir string) (*StartInfo, error) {
	data, err := os.ReadFile(filepath.Join(dir, StartFilename))
	if err != nil {
		return nil, err
	}
	var s StartInfo
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing start.json: %w", err)
	}
	return &s, nil
}

// RemoveStartInfo deletes dir/start.json, ignoring a missing file. It is
// best-effort: a finished run is identified by meta.json, so a leftover
// start.json is harmless.
func RemoveStartInfo(dir string) {
	_ = os.Remove(filepath.Join(dir, StartFilename))
}
