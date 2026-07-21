// Package shast parses shell command strings with mvdan.cc/sh/v3/syntax and
// exposes the result as JSON, since that package has no JSON marshaling of
// its own and a naive encoding/json pass over it loses position info and
// every interface field's concrete type.
package shast

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewCommand creates the shast cobra command. shast is dispatch-only: with no
// arguments it prints help; an unrecognized first token is an unknown-command
// error.
func NewCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "shast",
		Short: "Parse and inspect shell commands",
		Long:  "Parse a shell command with mvdan.cc/sh/v3/syntax and inspect the result via the shast subcommands.",

		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: dispatchOnly,
	}

	c.AddCommand(NewParseCommand())

	return c
}

func dispatchOnly(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "unknown command %q for %q\n", args[0], cmd.CommandPath())
	return &ExitError{Code: 2}
}
