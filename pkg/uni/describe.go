package uni

import (
	"fmt"
	"io/ioutil"
	"os"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/runenames"
)

func newDescribeCommand() *cobra.Command {
	d := &describer{
		table: unicode.Version,
	}

	c := cobra.Command{
		Use:                   "describe",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		Aliases:               []string{"d", "desc"},

		Short: "Describe characters",
		Args:  d.validate,
		RunE:  d.run,
	}

	c.Flags().StringVarP(&d.table, "table", "t", d.table, "Unicode Table version")
	return &c
}

type describer struct {
	table string
}

func (d *describer) run(c *cobra.Command, args []string) error {
	in, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading from stdin: %w", err)
	}

	for _, r := range []rune(string(in)) {
		name := runenames.Name(r)
		if unicode.IsControl(r) {
			fmt.Printf("%U\t%q\t%s\n", r, string(r), name)
		} else {
			fmt.Printf("%U\t%s\t%s\n", r, string(r), name)

		}
	}

	return nil
}

func (d *describer) validate(_ *cobra.Command, args []string) error {
	return nil
}
