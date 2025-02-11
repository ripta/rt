package uni

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/rangetable"
	"golang.org/x/text/unicode/runenames"
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
		Use:                   "list [--all | --categories <CATEGORIES> | <RUNE-NAME>]",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,

		Short: "List Unicode characters",
		Args:  l.validate,
		RunE:  l.run,
	}

	c.Flags().BoolVarP(&l.all, "all", "A", l.all, "List all")
	c.Flags().StringSliceVarP(&l.cats, "categories", "C", l.cats, "Categories to limit to")
	c.Flags().BoolVarP(&l.count, "count", "c", l.count, "Show count of matches")
	c.Flags().StringSliceVarP(&l.output, "output", "o", l.output, "Output columns")
	c.Flags().StringVarP(&l.table, "table", "t", l.table, "Unicode Table version")
	return &c
}

type lister struct {
	all    bool
	cats   []string
	count  bool
	output []string
	table  string
}

func (l *lister) run(c *cobra.Command, args []string) error {
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

	cols := map[string]bool{}
	for _, o := range l.output {
		cols[o] = true
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
			if cols["hexbytes"] {
				disp = append(disp, fmt.Sprintf("[%s]", runeToHexBytes(r)))
			}
			if cols["cats"] || cols["categories"] {
				disp = append(disp, fmt.Sprintf("<%s>", runeToCategories(r)))
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

func runeToHexBytes(r rune) string {
	bytes := make([]byte, utf8.UTFMax)
	utf8.EncodeRune(bytes, r)

	hexbytes := []string{}
	for _, b := range bytes {
		if b == 0 {
			hexbytes = append(hexbytes, "  ")
			continue
		}
		hexbytes = append(hexbytes, fmt.Sprintf("%02X", b))
	}

	return strings.Join(hexbytes, " ")
}

func runeToCategories(r rune) string {
	cats := []string{}
	for cat, rt := range unicode.Categories {
		if unicode.Is(rt, r) {
			cats = append(cats, cat)
		}
	}
	sort.Strings(cats)
	return strings.Join(cats, ",")
}
