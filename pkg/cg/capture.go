package cg

import (
	"fmt"
	"os"
	"path/filepath"
)

// Capture manages temporary files for capturing raw output.
type Capture struct {
	Stdout    *os.File
	Stderr    *os.File
	Lifecycle *os.File
	prefix    PrefixFunc
}

// NewCapture creates three temporary files for capturing stdout, stderr,
// and lifecycle output.
func NewCapture(pid int, prefix PrefixFunc) (*Capture, error) {
	dir := os.TempDir()

	stdout, err := os.Create(filepath.Join(dir, fmt.Sprintf("cg-%d-stdout", pid)))
	if err != nil {
		return nil, fmt.Errorf("creating stdout capture file: %w", err)
	}

	stderr, err := os.Create(filepath.Join(dir, fmt.Sprintf("cg-%d-stderr", pid)))
	if err != nil {
		stdout.Close()
		return nil, fmt.Errorf("creating stderr capture file: %w", err)
	}

	lifecycle, err := os.Create(filepath.Join(dir, fmt.Sprintf("cg-%d-lifecycle", pid)))
	if err != nil {
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("creating lifecycle capture file: %w", err)
	}

	return &Capture{
		Stdout:    stdout,
		Stderr:    stderr,
		Lifecycle: lifecycle,
		prefix:    prefix,
	}, nil
}

// WriteLifecycle writes an annotated lifecycle message.
func (c *Capture) WriteLifecycle(msg string) error {
	_, err := fmt.Fprintf(c.Lifecycle, "%s%c: %s\n", c.prefix(), IndicatorInfo, msg)
	return err
}

// Close closes all three capture files. Returns the first error encountered.
func (c *Capture) Close() error {
	first := c.Stdout.Close()
	if err := c.Stderr.Close(); err != nil && first == nil {
		first = err
	}
	if err := c.Lifecycle.Close(); err != nil && first == nil {
		first = err
	}
	return first
}
