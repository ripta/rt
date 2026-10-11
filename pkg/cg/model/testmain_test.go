package model

import (
	"errors"
	"os"
	"testing"
)

// TestMain dispatches a re-exec'd supervise-run/supervise-pool invocation to
// the real commands before running the test suite. RunSupervised and
// PoolSupervised (and tests that spawn the supervisor path directly, like
// TestCancelRunLive) re-exec os.Executable(), which under `go test` is this
// package's own test binary; without this dispatch, that invocation would
// just run the test suite again instead of supervising, recursively forking
// forever. The bare `supervise` form is the pre-rename alias.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "supervise-run", "supervise-pool", "supervise":
			os.Exit(cgMain())
		}
	}

	if len(os.Args) >= 3 && os.Args[1] == "history-append" {
		os.Exit(historyAppendMain(os.Args[2:]))
	}

	// Supervisors spawned by tests inherit this, so no test writes the real state dir.
	state, err := os.MkdirTemp("", "cg-state-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", state)

	code := m.Run()
	os.RemoveAll(state)
	os.Exit(code)
}

func cgMain() int {
	c := newTestRoot()
	err := c.Execute()
	if err == nil {
		return 0
	}

	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}

	return 1
}
