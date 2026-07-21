package approvecmd

import (
	"encoding/json"
	"fmt"
	"io"
)

// writeJSON marshals v as indented JSON and writes it to w with a trailing
// newline, mirroring package cg's unexported helper of the same name.
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
