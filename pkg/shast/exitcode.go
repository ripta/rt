package shast

import "fmt"

// ExitError represents a non-zero exit for a `shast` subcommand. It satisfies
// the error interface and carries the exit code for use by the entry point.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit code %d", e.Code)
}
