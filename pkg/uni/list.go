package uni

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/rangetable"
	"golang.org/x/text/unicode/runenames"
)

var (
	ErrNoUnicodeTable = errors.New("unicode table does not exist")
)

func newListCommand() *cobra.Command {
	l := &lister{
		output: []string{"id", "rune", "name"},
		table:  unicode.Version,
	}

	c := cobra.Command{
		Use:                   "list [--all | <RUNE-NAME>]",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,

		Short: "List Unicode characters",
		Args:  l.validate,
		RunE:  l.run,
	}

	c.Flags().BoolVarP(&l.all, "all", "A", l.all, "List all")
	c.Flags().BoolVarP(&l.count, "count", "c", l.count, "Show count of matches")
	c.Flags().StringSliceVarP(&l.output, "output", "o", l.output, "Output columns")
	c.Flags().StringVarP(&l.table, "table", "t", l.table, "Unicode Table version")
	return &c
}

type lister struct {
	all    bool
	count  bool
	output []string
	table  string
}

func (l *lister) run(c *cobra.Command, args []string) error {
	t := rangetable.Assigned(l.table)
	if t == nil {
		return fmt.Errorf("unicode table version %s: %w", l.table, ErrNoUnicodeTable)
	}

	cols := make(map[string]bool)
	for _, o := range l.output {
		cols[o] = true
	}

	count := 0
	norm := strings.Split(strings.ToUpper(strings.Join(args, ":")), ":")
	rangetable.Visit(t, func(r rune) {
		name := runenames.Name(r)
		if runeMatches(norm, name) {
			disp := []string{}
			if cols["id"] {
				disp = append(disp, fmt.Sprintf("%U", r))
			}
			if cols["rune"] {
				v := string(r)
				if unicode.IsControl(r) {
					v = fmt.Sprintf("%q", string(r))
				}
				disp = append(disp, v)
			}
			if cols["name"] {
				disp = append(disp, name)
			}
			if len(disp) > 0 {
				fmt.Println(strings.Join(disp, "\t"))
			}
			count++
		}
	})

	if l.count {
		fmt.Printf("Matched %d runes\n", count)
	}
	return nil
}

func (l *lister) validate(_ *cobra.Command, args []string) error {
	if len(args) < 1 && !l.all {
		return errors.New("at least one argument, or an explicit --all flag is required")
	}
	return nil
}

func runeMatches(normalized []string, name string) bool {
	for _, n := range normalized {
		if !strings.Contains(name, n) {
			return false
		}
	}
	return true
}
