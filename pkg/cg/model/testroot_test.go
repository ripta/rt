package model

import "github.com/spf13/cobra"

// newTestRoot builds a bare cobra root with every model command attached, for
// tests that dispatch through cobra's flag/arg parsing rather than calling
// package functions directly. It mirrors package cg's NewCommand() minus the
// bare-invocation help/error behavior, which belongs to that package.
func newTestRoot() *cobra.Command {
	c := &cobra.Command{
		Use:           "cg",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	c.AddCommand(NewRunCommand())
	c.AddCommand(NewOutCommand())
	c.AddCommand(NewErrCommand())
	c.AddCommand(NewPathsCommand())
	c.AddCommand(NewLsCommand())
	c.AddCommand(NewPruneCommand())
	c.AddCommand(NewMetaCommand())
	c.AddCommand(NewWaitCommand())
	c.AddCommand(NewCancelCommand())
	c.AddCommand(NewGrepCommand())
	c.AddCommand(NewNoteCommand())
	c.AddCommand(NewSuperviseRunCommand())
	c.AddCommand(NewSupervisePoolCommand())
	return c
}
