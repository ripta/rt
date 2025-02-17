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
	"golang.org/x/text/unicode/runenames"

	"github.com/ripta/rt/pkg/uni/display"
)

func newDescribeCommand() *cobra.Command {
	d := &describer{
		table: unicode.Version,
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

	c.Flags().StringVarP(&d.table, "table", "t", d.table, "Unicode Table version")
	return &c
}

type describer struct {
	table string
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
	for _, r := range []rune(in) {
		if r == utf8.RuneError {
			return errors.New("not valid utf8 encoding")
		}
		name := runenames.Name(r)
		if unicode.IsControl(r) {
			fmt.Printf("%U\t%q\t%s\t%s\n", r, string(r), fmt.Sprintf("[%s]", display.RuneToHexBytes(r)), name)
		} else {
			fmt.Printf("%U\t%s\t%s\t%s\n", r, string(r), fmt.Sprintf("[%s]", display.RuneToHexBytes(r)), name)

		}
	}

	return nil
}

func (d *describer) validate(_ *cobra.Command, _ []string) error {
	return nil
}
