package calc

import (
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewCommand creates a new calculator command.
//
// Expressions can be passed as one or more arguments. If no arguments are
// provided and STDIN is a TTY, it will start a REPL. Otherwise, the command
// will return ErrNotTTY.
func NewCommand() *cobra.Command {
	c := &Calculator{
		DecimalPlaces: 30,
		Verbose:       false,
	}
	cmd := &cobra.Command{
		Use:           "calc",
		Short:         "Calculate expressions",
		Long:          "Calculate expressions",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// mode 1: evaluate each arg
			if len(args) > 0 {
				for _, arg := range args {
					res, err := c.Evaluate(arg)
					if err != nil {
						return err
					}

					c.DisplayResult(res)
				}
				return nil
			}

			// mode 2: start interactive REPL if STDIN is a TTY
			if term.IsTerminal(int(os.Stdin.Fd())) {
				return c.REPL()
			}

			// otherwise, read from stdin
			return c.ProcessSTDIN()
		},
	}

	cmd.Flags().IntVarP(&c.DecimalPlaces, "decimal-places", "d", c.DecimalPlaces, "Number of decimal places to display")
	cmd.Flags().BoolVarP(&c.KeepTrailingZeros, "keep-trailing-zeros", "k", c.KeepTrailingZeros, "Keep trailing zeros in decimal output")
	cmd.Flags().BoolVarP(&c.UnderscoreZeros, "underscore-zeros", "u", c.UnderscoreZeros, "Insert underscore before trailing zeros, implies --keep-trailing-zeros")
	cmd.Flags().BoolVarP(&c.Verbose, "verbose", "v", c.Verbose, "Verbose output")
	cmd.Flags().BoolVarP(&c.Trace, "trace", "t", c.Trace, "Enable trace mode to print comments during evaluation")

	return cmd
}
