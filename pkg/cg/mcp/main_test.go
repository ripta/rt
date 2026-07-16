package mcp

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/ripta/rt/pkg/cg"
)

func TestMain(m *testing.M) {
	// RunSupervised re-execs os.Executable(), which under `go test` is this
	// test binary. Dispatch that invocation to the real cg root command so the
	// spawn path is exercised with its production argv.
	if len(os.Args) > 1 && os.Args[1] == "supervise" {
		os.Exit(cgMain())
	}

	os.Exit(m.Run())
}

func cgMain() int {
	err := cg.NewCommand().Execute()
	if err == nil {
		return 0
	}

	var exitErr *cg.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}

	fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
	return 1
}
