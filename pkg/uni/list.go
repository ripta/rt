package uni

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/rangetable"
	"golang.org/x/text/unicode/runenames"

	"github.com/ripta/rt/pkg/uni/display"
)

var (
	ErrNoUnicodeTable = errors.New("unicode table does not exist")
)

func newListCommand() *cobra.Command {
	l := &lister{
		output: []string{"id", "rune", "hexbytes", "categories", "name"},
		table:  unicode.Version,
	}

	c := cobra.Command{
		Use:                   "list [--categories <CATEGORIES>] [--scripts <SCRIPTS>] [--all | <RUNE-NAME-FILTER...>]",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,

		Short: "List Unicode characters",
		Args:  l.validate,
		RunE:  l.run,
	}

	c.Flags().BoolVarP(&l.all, "all", "A", l.all, "List all")
	c.Flags().StringSliceVarP(&l.cats, "categories", "C", l.cats, "Categories to limit to (see `uni catsegories`); by default all categories")
	c.Flags().BoolVarP(&l.count, "count", "c", l.count, "Show count of matches")
	c.Flags().StringSliceVarP(&l.output, "output", "o", l.output, "Output columns")
	c.Flags().StringSliceVarP(&l.scripts, "scripts", "S", l.scripts, "Scripts to limit to (see `uni scripts`); by default all scripts")
	c.Flags().StringVarP(&l.table, "table", "t", l.table, "Unicode Table version")
	return &c
}

type lister struct {
	all     bool
	cats    []string
	count   bool
	output  []string
	scripts []string
	table   string
}

func (l *lister) run(c *cobra.Command, args []string) error {
	disp, err := display.New(l.output)
	if err != nil {
		return err
	}

	t := rangetable.Assigned(l.table)
	if t == nil {
		return fmt.Errorf("unicode table version %s: %w", l.table, ErrNoUnicodeTable)
	}

	excludes := []*unicode.RangeTable{}
	if len(l.cats) > 0 {
		rts := []*unicode.RangeTable{}
		for _, cat := range l.cats {
			rt, ok := unicode.Categories[cat]
			if !ok {
				if strings.HasPrefix(cat, "!") {
					cat = strings.TrimPrefix(cat, "!")
					if unrt, ok := unicode.Categories[cat]; ok {
						excludes = append(excludes, unrt)
						continue
					}
				}

				return fmt.Errorf("unicode category %q does not exist; see `uni categories`", cat)
			}

			rts = append(rts, rt)
		}

		t = rangetable.Merge(rts...)
	}

	scriptFilter := []*unicode.RangeTable{}
	if len(l.scripts) > 0 {
		for _, s := range l.scripts {
			rt, ok := unicode.Scripts[s]
			if !ok {
				return fmt.Errorf("unicode script %q does not exist; see `uni scripts`", s)
			}

			scriptFilter = append(scriptFilter, rt)
		}
	}

	count := 0
	norm := strings.Split(strings.ToUpper(strings.Join(args, ":")), ":")
	rangetable.Visit(t, func(r rune) {
		if len(excludes) > 0 {
			for _, exclude := range excludes {
				if unicode.Is(exclude, r) {
					return
				}
			}
		}

		if len(scriptFilter) > 0 {
			for _, script := range scriptFilter {
				if !unicode.Is(script, r) {
					return
				}
			}
		}

		name := runenames.Name(r)
		if runeMatches(norm, name) {
			if cols := disp.Generate(r); len(cols) > 0 {
				fmt.Println(strings.Join(cols, "\t"))
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
	if len(args) < 1 && !l.all && len(l.cats) == 0 {
		return errors.New("at least one argument, or an explicit --all flag, or one or more --categories is required")
	}
	return nil
}

func runeMatches(normalized []string, name string) bool {
	for _, n := range normalized {
		if post, ok := strings.CutPrefix(n, "!"); ok {
			if strings.Contains(name, post) {
				return false
			}
			continue
		}

		if !strings.Contains(name, n) {
			return false
		}
	}
	return true
}
