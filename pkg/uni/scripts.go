package uni

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"unicode"

	"github.com/liggitt/tabwriter"
	"github.com/spf13/cobra"
)

func newScriptsCommand() *cobra.Command {
	c := cobra.Command{
		Use:           "scripts",
		Short:         "List Unicode script names and their rune counts",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runScripts,
	}

	return &c
}

func runScripts(_ *cobra.Command, _ []string) error {
	t := tabwriter.NewWriter(os.Stdout, 6, 4, 3, ' ', tabwriter.RememberWidths)
	defer t.Flush()

	fmt.Fprintf(t, "%s\t%s\n", "NAME", "RUNE COUNT")
	for _, name := range slices.Sorted(maps.Keys(unicode.Scripts)) {
		fmt.Fprintf(t, "%s\t%d\n", name, countCharactersIn(unicode.Scripts[name]))
	}

	return nil
}
