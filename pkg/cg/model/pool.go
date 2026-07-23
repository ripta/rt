package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

// Pool states derived from the manifest and the pool lock. A manifest with
// FinishedAt set is finished. Without it, a held lock means the supervisor is
// alive and the pool is running; a released lock means the supervisor died
// mid-pool and the pool is abandoned.
const (
	PoolStateRunning   = "running"
	PoolStateFinished  = "finished"
	PoolStateAbandoned = "abandoned"
)

// PoolState classifies the pool in dir given its manifest m.
func PoolState(dir string, m *PoolManifest) string {
	if m.FinishedAt != nil {
		return PoolStateFinished
	}
	if RunLockReleased(dir) {
		return PoolStateAbandoned
	}
	return PoolStateRunning
}

// PoolManifest is the pool's on-disk record, rewritten atomically by the pool
// supervisor as state changes. A manifest without FinishedAt belongs to a pool
// that is still running, or, when the pool lock is released, one whose
// supervisor died: the abandoned-pool state. Env overrides never appear here.
// SessionID names the cg mcp server that spawned the pool; the supervisor
// passes it down to each member run's own record.
type PoolManifest struct {
	ID          string          `json:"id"`
	Commands    [][]string      `json:"commands"`
	Repeat      int             `json:"repeat"`
	Parallelism int             `json:"parallelism"`
	OnError     string          `json:"on_error"`
	Cwd         string          `json:"cwd,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
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

// Failed reports whether the record is a failure: a start failure, or a
// finished run with a non-zero exit or a terminating signal.
func (r PoolRunRecord) Failed() bool {
	switch r.Status {
	case PoolRunStartError:
		return true
	case PoolRunFinished:
		return (r.ExitCode != nil && *r.ExitCode != 0) || r.Signal != nil
	}
	return false
}

// PoolCounts tallies a pool's member records by outcome.
type PoolCounts struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded,omitempty"`
	Failed    int `json:"failed,omitempty"`
	Skipped   int `json:"skipped,omitempty"`
	Running   int `json:"running,omitempty"`
	Pending   int `json:"pending,omitempty"`
}

// Counts tallies the manifest's run records.
func (m *PoolManifest) Counts() PoolCounts {
	c := PoolCounts{Total: len(m.Runs)}
	for _, r := range m.Runs {
		switch {
		case r.Failed():
			c.Failed++
		case r.Status == PoolRunFinished:
			c.Succeeded++
		case r.Status == PoolRunSkipped:
			c.Skipped++
		case r.Status == PoolRunRunning:
			c.Running++
		default:
			c.Pending++
		}
	}
	return c
}

// MemberIDs returns the run IDs the manifest names, skipping records that never
// got one (pending or skipped). IDs that fail validation are dropped: callers
// join these onto the capture root and remove them, so a corrupt manifest must
// never yield a traversal path.
func (m *PoolManifest) MemberIDs() []string {
	ids := make([]string, 0, len(m.Runs))
	for _, r := range m.Runs {
		if r.RunID != "" && IsValidRunID(r.RunID) {
			ids = append(ids, r.RunID)
		}
	}
	return ids
}

// PoolSupervisorPid returns the pool supervisor's pid for signalling, or
// finished=true when there is nothing left to signal: the manifest records
// finished_at, the pool lock is released, or the pid file is already gone. The
// released-lock case matters because a SIGKILLed supervisor leaves a stale pid
// file, and signalling a recycled pid is worse than reporting the pool dead.
func PoolSupervisorPid(dir string, m *PoolManifest) (int, bool, error) {
	if m.FinishedAt != nil {
		return 0, true, nil
	}
	if RunLockReleased(dir) {
		return 0, true, nil
	}
	pid, err := ReadPidFile(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, true, nil
	}
	if err != nil {
		return 0, false, err
	}
	return pid, false, nil
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
