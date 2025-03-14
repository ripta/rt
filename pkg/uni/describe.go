package uni

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/uni/display"
)

func newDescribeCommand() *cobra.Command {
	d := &describer{
		output: display.DefaultColumns(),
		table:  unicode.Version,
	}

	c := cobra.Command{
		Use:                   "describe [<CHARACTERS...]",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		Aliases:               []string{"d", "desc"},

		Short: "Describe characters, either as arguments or from STDIN",
		Args:  d.validate,
		RunE:  d.run,
	}

	c.Flags().StringSliceVarP(&d.output, "output", "o", d.output, "Output columns")
	c.Flags().StringVarP(&d.table, "table", "t", d.table, "Unicode Table version")
	return &c
}

type describer struct {
	output []string
	table  string
}

func (d *describer) run(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return d.display(strings.Join(args, ""))
	}

	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading from stdin: %w", err)
	}

	return d.display(string(in))
}

func (d *describer) display(in string) error {
	disp, err := display.New(d.output)
	if err != nil {
		return err
	}

	for _, r := range []rune(in) {
		if r == utf8.RuneError {
			return errors.New("not valid utf8 encoding")
		}
		if cols := disp.Generate(r); len(cols) > 0 {
			fmt.Println(strings.Join(cols, "\t"))
		}
	}

	return nil
}

func (d *describer) validate(_ *cobra.Command, _ []string) error {
	return nil
}
