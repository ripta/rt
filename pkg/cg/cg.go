// Package cg assembles the full `cg` command tree: the capture-run model's
// own subcommands (package model), the MCP server (package mcp), and the
// approval-rules subcommands (package approvecmd). It is the single place
// that has to know about every subcommand package, so any binary that calls
// NewCommand gets all of them without repeating the wiring itself.
package cg

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/cg/approvecmd"
	"github.com/ripta/rt/pkg/cg/mcp"
	"github.com/ripta/rt/pkg/cg/model"
)

// ExitError re-exports model.ExitError so callers that only import cg for
// NewCommand don't also need to import model to unwrap the exit code from a
// returned error.
type ExitError = model.ExitError

// NewCommand creates the cg cobra command. cg is dispatch-only: it executes
// programs through the `cg run` subcommand and exposes the capture-run model
// through the resolution subcommands. With no arguments it prints help; an
// unrecognized first token, including the old bare-`cg -- COMMAND` form, is an
// unknown-command error.
func NewCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "cg",
		Short: "Annotate command output and inspect captured runs",
		Long:  "Execute commands with annotated output via `cg run`, and inspect or prune captured runs via the resolution subcommands.",

		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: dispatchOnly,
	}

	c.AddCommand(model.NewRunCommand())
	c.AddCommand(model.NewOutCommand())
	c.AddCommand(model.NewErrCommand())
	c.AddCommand(model.NewPathsCommand())
	c.AddCommand(model.NewLsCommand())
	c.AddCommand(model.NewPruneCommand())
	c.AddCommand(model.NewMetaCommand())
	c.AddCommand(model.NewWaitCommand())
	c.AddCommand(model.NewCancelCommand())
	c.AddCommand(model.NewGrepCommand())
	c.AddCommand(model.NewNoteCommand())
	c.AddCommand(model.NewSuperviseRunCommand())
	c.AddCommand(model.NewSupervisePoolCommand())

	c.AddCommand(mcp.NewCommand())
	c.AddCommand(approvecmd.NewCheckCommand())
	c.AddCommand(approvecmd.NewLintCommand())

	return c
}

// dispatchOnly is the bare `cg` action. With no arguments it prints help and
// exits 0. Any positional argument reaches here only via `--` (cobra catches a
// bare unknown token before dispatch); the old `cg -- COMMAND` execution form
// no longer runs, so it is reported as an unknown command and exits 2.
func dispatchOnly(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "unknown command %q for %q; did you mean \"cg run -- %s\"?\n", args[0], cmd.CommandPath(), args[0])
	return &ExitError{Code: 2}
}
