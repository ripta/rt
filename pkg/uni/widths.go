package uni

import (
	"fmt"
	"os"
	"unicode"

	"github.com/liggitt/tabwriter"
	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/rangetable"
	"golang.org/x/text/width"

	"github.com/ripta/rt/pkg/uni/display"
)

func newWidthsCommand() *cobra.Command {
	c := cobra.Command{
		Use:           "widths",
		Aliases:       []string{"width"},
		Short:         "List East Asian Width values and their rune counts",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runWidths,
	}

	return &c
}

func runWidths(_ *cobra.Command, _ []string) error {
	counts := map[width.Kind]int{}
	rangetable.Visit(rangetable.Assigned(unicode.Version), func(r rune) {
		counts[width.LookupRune(r).Kind()]++
	})

	t := tabwriter.NewWriter(os.Stdout, 6, 4, 3, ' ', tabwriter.RememberWidths)
	defer t.Flush()

	fmt.Fprintf(t, "%s\t%s\t%s\n", "KEY", "NAME", "RUNE COUNT")
	for _, w := range display.Widths {
		fmt.Fprintf(t, "%s\t%s\t%d\n", w.Abbrev, w.Name, counts[w.Kind])
	}

	return nil
}
