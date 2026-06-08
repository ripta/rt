package cg

import (
	"fmt"
	"os"
	"path/filepath"
)

// CaptureDirName is the subdirectory under $TMPDIR that holds per-run capture
// directories.
const CaptureDirName = "cg"

// CaptureRoot returns the parent directory that holds all per-run capture
// directories: $TMPDIR/cg.
func CaptureRoot() string {
	return filepath.Join(os.TempDir(), CaptureDirName)
}

// Capture holds the open stdout and stderr files for a single capture run,
// along with the run's identifier and directory.
type Capture struct {
	ID     string
	Dir    string
	Stdout *os.File
	Stderr *os.File
}

// NewCapture allocates a fresh run ID, creates $TMPDIR/cg/<ID>/, and opens
// stdout and stderr inside it.
func NewCapture() (*Capture, error) {
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		return nil, fmt.Errorf("creating capture root: %w", err)
	}

	id, dir, err := newRunDir(CaptureRoot())
	if err != nil {
		return nil, err
	}

	stdout, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		return nil, fmt.Errorf("creating stdout capture file: %w", err)
	}
	stderr, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		stdout.Close()
		return nil, fmt.Errorf("creating stderr capture file: %w", err)
	}

	return &Capture{ID: id, Dir: dir, Stdout: stdout, Stderr: stderr}, nil
}

// Close closes the captured stdout and stderr files. The first error
// encountered is returned.
func (c *Capture) Close() error {
	first := c.Stdout.Close()
	if err := c.Stderr.Close(); err != nil && first == nil {
		first = err
	}
	return first
}
