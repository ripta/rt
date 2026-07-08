package cg

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// grepCmdOptions holds flags for the `cg grep` subcommand.
type grepCmdOptions struct {
	Text            string
	Pattern         string
	Streams         string
	CaseInsensitive bool
	InvertMatch     bool
	MaxMatches      int
}

// NewGrepCommand returns the `cg grep <ID>` subcommand. It searches a run's
// captured output and prints matching lines as indented JSON.
func NewGrepCommand() *cobra.Command {
	opts := &grepCmdOptions{}
	c := &cobra.Command{
		Use:           "grep <ID>",
		Short:         "Search a run's captured output and print matches as JSON",
		Long:          "Search a run's captured output line by line. Supply exactly one of --text (fixed string) or --pattern (RE2 regex). Works on in-flight runs.",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := Grep(args[0], GrepOptions{
				Text:            opts.Text,
				Pattern:         opts.Pattern,
				Streams:         opts.Streams,
				CaseInsensitive: opts.CaseInsensitive,
				InvertMatch:     opts.InvertMatch,
				MaxMatches:      opts.MaxMatches,
			})
			if errors.Is(err, ErrUnknownRunID) {
				fmt.Fprintf(cmd.ErrOrStderr(), "unknown run id: %s\n", args[0])
				return &ExitError{Code: 1}
			}
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 2}
			}
			return writeJSON(cmd.OutOrStdout(), res)
		},
	}
	c.Flags().StringVar(&opts.Text, "text", "", "fixed-string substring to match (mutually exclusive with --pattern)")
	c.Flags().StringVar(&opts.Pattern, "pattern", "", "RE2 regular expression to match (mutually exclusive with --text)")
	c.Flags().StringVar(&opts.Streams, "streams", "", "which streams to search: all (default), stdout, or stderr")
	c.Flags().BoolVarP(&opts.CaseInsensitive, "ignore-case", "i", false, "fold case when matching")
	c.Flags().BoolVarP(&opts.InvertMatch, "invert-match", "v", false, "return lines that do NOT match")
	c.Flags().IntVar(&opts.MaxMatches, "max-matches", 0, "cap on returned matches (default 1000, max 10000)")
	return c
}
