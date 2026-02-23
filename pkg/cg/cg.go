package cg

import (
	"github.com/spf13/cobra"
)

// DefaultFormat is the default time prefix format: short time.
const DefaultFormat = "15:04:05 "

type Options struct {
	Format string
}

// NewCommand creates the cg cobra command.
func NewCommand() *cobra.Command {
	opts := &Options{}
	c := &cobra.Command{
		Use:   "cg [flags] -- COMMAND [ARGS...]",
		Short: "Execute a command and annotate its output",
		Long:  "Execute a child command, annotating each line of stdout and stderr with a timestamp prefix and stream indicator.",

		SilenceErrors: true,
		SilenceUsage:  true,

		Args: cobra.MinimumNArgs(1),
		RunE: opts.run,
	}

	c.Flags().StringVar(&opts.Format, "format", DefaultFormat, "time prefix format (Go time.Format layout)")

	return c
}
