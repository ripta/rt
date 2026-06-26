package cg

import (
	"fmt"

	"github.com/spf13/cobra"
)

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

	c.AddCommand(NewRunCommand())
	c.AddCommand(NewOutCommand())
	c.AddCommand(NewErrCommand())
	c.AddCommand(NewPathsCommand())
	c.AddCommand(NewLsCommand())
	c.AddCommand(NewPruneCommand())

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
