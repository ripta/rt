package cg

import (
	"os"
	"time"
)

// RunInfo holds the start-time facts common to every capture run. StartInfo,
// Meta, and StartDebug embed it so the three on-disk records stay in parity.
type RunInfo struct {
	ID        string    `json:"id"`
	Command   []string  `json:"command"`
	Cwd       string    `json:"cwd,omitempty"`
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
