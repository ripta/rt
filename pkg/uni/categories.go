package uni

import (
	"fmt"
	"os"
	"sort"
	"unicode"

	"github.com/liggitt/tabwriter"
	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/rangetable"
)

var Categories = map[string]string{
	"C":  "Other",
	"Cc": "Control",
	"Cf": "Format",
	"Co": "Private Use",
	"Cs": "Surrrogate",
	"L":  "Letter",
	"Ll": "Lowercase Letter",
	"Lm": "Modifier Letter",
	"Lo": "Other Letter",
	"Lt": "Titlecase Letter",
	"Lu": "Uppercase Letter",
	"M":  "Mark",
	"Mc": "Spacing Mark",
	"Me": "Enclosing Mark",
	"Mn": "Nonspacing Mark",
	"N":  "Number",
	"Nd": "Decimal Number",
	"Nl": "Letter Number",
	"No": "Other Number",
	"P":  "Punctuation",
	"Pc": "Connector Punctuation",
	"Pd": "Dash Punctuation",
	"Pe": "Close Punctuation",
	"Pf": "Final Punctuation",
	"Pi": "Initial Punctuation",
	"Po": "Other Punctuation",
	"Ps": "Open Punctuation",
	"S":  "Symbol",
	"Sc": "Currency Symbol",
	"Sk": "Modifier Symbol",
	"Sm": "Math Symbol",
	"So": "Other Symbol",
	"Z":  "Separator",
	"Zl": "Line Separator",
	"Zp": "Paragraph Separator",
	"Zs": "Space Separator",
}

func init() {
	for cat := range unicode.Categories {
		if _, ok := Categories[cat]; !ok {
			Categories[cat] = cat
		}
	}
}

func newCategoriesCommand() *cobra.Command {
	r := &catter{}
	c := cobra.Command{
		Use:                   "categories",
		Aliases:               []string{"cat", "cats"},
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,

		Short: "List Unicode categories",
		RunE:  r.run,
	}

	c.Flags().StringVarP(&r.table, "table", "t", r.table, "Unicode Table version")
	return &c
}

type catter struct {
	table string
}

func (_ *catter) run(_ *cobra.Command, _ []string) error {
	cats := []string{}
	for cat := range Categories {
		cats = append(cats, cat)
	}
	sort.Strings(cats)

	t := tabwriter.NewWriter(os.Stdout, 6, 4, 3, ' ', tabwriter.RememberWidths)
	defer t.Flush()

	fmt.Fprintf(t, "%s\t%s\t%s\n", "KEY", "NAME", "RUNE COUNT")
	for _, cat := range cats {
		fmt.Fprintf(t, "%s\t%s\t%d\n", cat, Categories[cat], countCharactersIn(unicode.Categories[cat]))
	}

	return nil
}

func countCharactersIn(rt *unicode.RangeTable) int {
	count := 0
	rangetable.Visit(rt, func(_ rune) {
		count += 1
	})
	return count
}
