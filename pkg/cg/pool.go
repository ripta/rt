package cg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PoolManifestFilename is the file within a pool directory that holds the pool's
// spec echo and per-run state. Its presence is what distinguishes a pool directory
// from a run directory; a pool has no stream files.
const PoolManifestFilename = "pool.json"

// Pool error policies. Continue runs the whole pool regardless of failures. Stop
// schedules nothing new after the first failure and lets in-flight runs finish.
// Kill additionally cancels in-flight runs via their process groups.
const (
	OnErrorContinue = "continue"
	OnErrorStop     = "stop"
	OnErrorKill     = "kill"
)

// Pool member run statuses as recorded in the manifest. A finished record carries
// the exit code and optional signal; a start_error record carries the error text.
// Skipped marks runs never started because scheduling stopped.
const (
	PoolRunPending    = "pending"
	PoolRunRunning    = "running"
	PoolRunFinished   = "finished"
	PoolRunStartError = "start_error"
	PoolRunSkipped    = "skipped"
)

// PoolManifest is the pool's on-disk record, rewritten atomically by the pool
// supervisor as state changes. A manifest without FinishedAt belongs to a pool
// that is still running, or, when the pool lock is released, one whose
// supervisor died: the abandoned-pool state. Env overrides never appear here.
type PoolManifest struct {
	ID          string          `json:"id"`
	Commands    [][]string      `json:"commands"`
	Repeat      int             `json:"repeat"`
	Parallelism int             `json:"parallelism"`
	OnError     string          `json:"on_error"`
	Cwd         string          `json:"cwd,omitempty"`
	StartedAt   time.Time       `json:"started_at"`
	FinishedAt  *time.Time      `json:"finished_at,omitempty"`
	Runs        []PoolRunRecord `json:"runs"`
}

// PoolRunRecord is one scheduled run within a pool. Command indexes the
// manifest's Commands array. RunID is empty while the run is pending or when
// it was skipped.
type PoolRunRecord struct {
	Command    int    `json:"command"`
	RunID      string `json:"run_id,omitempty"`
	Status     string `json:"status"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	Signal     *int   `json:"signal,omitempty"`
	StartError string `json:"start_error,omitempty"`
}

// WritePoolManifest serialises m and writes it atomically to dir/pool.json.
func WritePoolManifest(dir string, m *PoolManifest) error {
	if err := writeFileAtomic(dir, PoolManifestFilename, m); err != nil {
		return fmt.Errorf("pool manifest: %w", err)
	}
	return nil
}

// ReadPoolManifest loads pool.json from dir.
func ReadPoolManifest(dir string) (*PoolManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, PoolManifestFilename))
	if err != nil {
		return nil, err
	}
	var m PoolManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing pool.json: %w", err)
	}
	return &m, nil
}

// NewPoolDir allocates a pool directory under the capture root, sharing the
// run ID scheme. Unlike NewCapture it creates no stream files; the pool
// supervisor populates the directory with pool.json, lock, and pid.
func NewPoolDir() (id, dir string, err error) {
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		return "", "", fmt.Errorf("creating capture root: %w", err)
	}
	return newRunDir(CaptureRoot())
}
