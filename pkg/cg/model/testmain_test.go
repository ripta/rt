package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain dispatches a re-exec'd supervise-run/supervise-pool invocation to
// the real commands before running the test suite. RunSupervised and
// PoolSupervised (and tests that spawn the supervisor path directly, like
// TestCancelRunLive) re-exec os.Executable(), which under `go test` is this
// package's own test binary; without this dispatch, that invocation would
// just run the test suite again instead of supervising, recursively forking
// forever. The bare `supervise` form is the pre-rename alias.
//
// A copy of the test binary named "rt" models the multi-call binary. It knows the supervisors only
// under the "cg" subcommand, so `rt supervise-run` fails the way the real one does.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "cg" && isSuperviseName(os.Args[2]) {
		os.Args = append(os.Args[:1:1], os.Args[2:]...)
		os.Exit(cgMain())
	}

	if len(os.Args) > 1 && isSuperviseName(os.Args[1]) {
		if filepath.Base(os.Args[0]) == "rt" {
			fmt.Fprintf(os.Stderr, "Error: unknown command %q for \"rt\"\n", os.Args[1])
			os.Exit(1)
		}
		os.Exit(cgMain())
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

func isSuperviseName(name string) bool {
	switch name {
	case "supervise-run", "supervise-pool", "supervise":
		return true
	}
	return false
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
