package cg

import (
	"encoding/json"
	"fmt"
	"io"
)

// Run state strings shared by the meta, list, wait, and cancel outputs.
const (
	RunStateRunning  = "running"
	RunStateFinished = "finished"
	RunStateFailed   = "failed"
)

// writeJSON marshals v as indented JSON and writes it to w with a trailing
// newline. The resolution subcommands emit machine-readable JSON, so this is
// their shared rendering path.
func writeJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling output: %w", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}
