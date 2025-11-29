package calc

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var ErrNotTTY = errors.New("STDIN is not a TTY")

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
			if len(args) == 0 {
				if term.IsTerminal(int(os.Stdin.Fd())) {
					c.REPL()
					return nil
				}

				return fmt.Errorf("%w: must specify expression as arguments", ErrNotTTY)
			}

			for _, arg := range args {
				res, err := c.Evaluate(arg)
				if err != nil {
					return err
				}

				c.DisplayResult(res)
			}

			return nil
		},
	}

	cmd.Flags().IntVarP(&c.DecimalPlaces, "decimal-places", "d", c.DecimalPlaces, "Number of decimal places to display")
	cmd.Flags().BoolVarP(&c.Verbose, "verbose", "v", c.Verbose, "Verbose output")

	return cmd
}
