// Package model implements cg's capture-run domain: the on-disk model for
// runs and pools (meta, start, debug, and pool manifest records), execution
// (RunSupervised, PoolSupervised, and the supervise-run/supervise-pool
// commands that back them), resolution and locking, notes, grep, prune, and
// the `cg run` annotator. It has no dependency on package cg, approve, or
// mcp, so any of them can depend on it without an import cycle; package cg
// composes model's commands into the full `cg` tree alongside mcp and
// approvecmd.
package model

import (
	"os"
	"time"
)

// RunInfo holds the start-time facts common to every capture run. StartInfo,
// Meta, and StartDebug embed it so the three on-disk records stay in parity.
// Pool names the pool this run is a member of; empty for standalone runs.
// SessionID names the cg mcp server process that spawned this run; empty for
// runs not spawned by a server, including the standalone cg run CLI.
type RunInfo struct {
	ID        string    `json:"id"`
	Command   []string  `json:"command"`
	Cwd       string    `json:"cwd,omitempty"`
	Pool      string    `json:"pool,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// effectiveCwd records the run's working directory. An empty cwd means the
// child inherits the caller's directory, so resolve it to an absolute path.
func effectiveCwd(cwd string) string {
	if cwd != "" {
		return cwd
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}
