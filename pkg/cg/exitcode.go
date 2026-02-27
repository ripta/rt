package cg

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// ExitError represents a non-zero exit from a child process. It satisfies the
// error interface and carries the exit code for use by the entry point.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit code %d", e.Code)
}

// ExitCodeFromError maps an error from exec.Cmd.Wait into a conventional exit
// code. Returns 127 for command-not-found, 126 for permission denied, and the
// child's actual exit code for exec.ExitError.
//
// For any other error it returns 1 as a generic failure code.
func ExitCodeFromError(err error) int {
	if err == nil {
		return 0
	}

	if errors.Is(err, exec.ErrNotFound) {
		return 127
	}

	var pathErr *exec.Error
	if errors.As(err, &pathErr) {
		if errors.Is(pathErr.Err, syscall.EACCES) {
			return 126
		}
		return 127
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	return 1
}
