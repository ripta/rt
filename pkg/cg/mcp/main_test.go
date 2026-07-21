package mcp

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/cg/model"
)

func TestMain(m *testing.M) {
	// RunSupervised re-execs os.Executable(), which under `go test` is this
	// test binary. Dispatch that invocation to the real cg root command so the
	// spawn path is exercised with its production argv. The bare `supervise`
	// form is the pre-rename alias.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "supervise-run", "supervise-pool", "supervise":
			os.Exit(cgMain())
		}
	}

	// spawn-run and spawn-pool play a doomed MCP server for the
	// restart-tolerance tests: each starts supervised work and blocks until the
	// test kills it.
	if len(os.Args) > 1 && os.Args[1] == "spawn-run" {
		os.Exit(spawnRunMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "spawn-pool" {
		os.Exit(spawnPoolMain(os.Args[2:]))
	}

	os.Exit(m.Run())
}

// cgMain dispatches to the two hidden commands RunSupervised/PoolSupervised
// re-exec: it does not need the full cg command tree, just these, so it
// builds its own minimal root rather than importing package cg. Package cg
// imports package mcp, so mcp importing cg back would cycle in the test
// build.
func cgMain() int {
	root := &cobra.Command{Use: "cg", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(model.NewSuperviseRunCommand())
	root.AddCommand(model.NewSupervisePoolCommand())

	err := root.Execute()
	if err == nil {
		return 0
	}

	var exitErr *model.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}

	fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
	return 1
}
