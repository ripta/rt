package uni

import (
	"bufio"
	"fmt"
	"os"
	"slices"

	"github.com/spf13/cobra"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

func newSortCommand() *cobra.Command {
	s := &sorter{
		langStr: "en-US",
	}

	c := cobra.Command{
		Use:                   "sort",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,

		Short: "Sort",
		Args:  s.validate,
		RunE:  s.run,
	}

	c.Flags().StringVarP(&s.langStr, "language", "l", s.langStr, "Collation language tag")
	c.Flags().BoolVarP(&s.reverse, "reverse", "r", s.reverse, "Reverse sort order")

	cl := cobra.Command{
		Use:                   "list",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,

		RunE: doCollationList,
	}
	c.AddCommand(&cl)

	return &c
}

func doCollationList(_ *cobra.Command, _ []string) error {
	for _, tag := range collate.Supported() {
		fmt.Printf("%s\n", tag.String())
	}
	return nil
}

type sorter struct {
	langStr string
	langTag language.Tag
	reverse bool
}

func (s *sorter) validate(_ *cobra.Command, _ []string) error {
	tag, err := language.Parse(s.langStr)
	if err != nil {
		return fmt.Errorf("invalid language tag %q: %w", s.langStr, err)
	}

	if tag.String() != s.langStr {
		fmt.Fprintf(os.Stderr, "Warning: interpreted collation %q as %q\n", s.langStr, tag.String())
	}

	s.langTag = tag
	return nil
}

func (s *sorter) run(_ *cobra.Command, _ []string) error {
	sc := bufio.NewScanner(os.Stdin)
	lines := []string{}
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	coll := collate.New(s.langTag)
	coll.SortStrings(lines)
	if s.reverse {
		slices.Reverse(lines)
	}

	for _, line := range lines {
		fmt.Println(line)
	}

	return nil
}
