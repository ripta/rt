package cg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DebugFilename is the file name within a run directory that holds
// start-failure diagnostics.
const DebugFilename = "debug.json"

// StartDebug holds diagnostic information written when a child process fails
// to start. It is persisted to debug.json alongside stdout and stderr.
type StartDebug struct {
	Command       []string `json:"command"`
	ResolvedPath  string   `json:"resolved_path,omitempty"`
	CanonicalPath string   `json:"canonical_path,omitempty"`
	Cwd           string   `json:"cwd,omitempty"`
	Path          string   `json:"path,omitempty"`
	StartError    string   `json:"start_error"`
}

// WriteStartDebug serialises d and writes it to dir/debug.json.
func WriteStartDebug(dir string, d *StartDebug) error {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling debug: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, DebugFilename), data, 0o644); err != nil {
		return fmt.Errorf("writing debug.json: %w", err)
	}
	return nil
}

// ReadStartDebug loads debug.json from dir.
func ReadStartDebug(dir string) (*StartDebug, error) {
	data, err := os.ReadFile(filepath.Join(dir, DebugFilename))
	if err != nil {
		return nil, err
	}
	var d StartDebug
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parsing debug.json: %w", err)
	}
	return &d, nil
}
