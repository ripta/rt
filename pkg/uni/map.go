package uni

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/uni/mapscheme"
)

var (
	ErrMapSchemeRequired = errors.New("map scheme required")
	ErrMapSchemeUnknown  = errors.New("unknown map scheme")
)

func newMapCommand() *cobra.Command {
	m := &mapper{}

	c := cobra.Command{
		Use:                   "map",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,

		Short: "Map characters",
		Args:  m.validate,
		RunE:  m.run,
	}

	c.Flags().BoolVarP(&m.ShowAll, "all", "a", m.ShowAll, "Show all schemes")

	return &c
}

type mapper struct {
	ShowAll bool
}

func (m *mapper) run(c *cobra.Command, args []string) error {
	if m.ShowAll {
		return m.runAll(os.Stdin)
	}
	return m.runOne(os.Stdin, args[0])
}

func (m *mapper) runAll(r io.Reader) error {
	in, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	lines := strings.Split(strings.TrimSpace(string(in)), "\n")
	if len(lines) != 1 {
		return fmt.Errorf("expected one line of input, but received %d", len(lines))
	}

	orig := strings.TrimSpace(lines[0])
	if orig == "" {
		orig = "The lazy brown fox jumps over the quick dog" // 😉
	}

	fmt.Printf("original: %s\n", orig)

	for _, name := range mapscheme.Names() {
		scheme := mapscheme.Get(name)
		fmt.Printf("%s: %s\n", name, strings.Map(scheme, orig))
	}

	return nil
}

func (m *mapper) runOne(r io.Reader, name string) error {
	scheme, err := mapscheme.Find(name)
	if err != nil {
		return err
	}

	in, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	out := strings.Map(scheme, string(in))
	fmt.Print(out)
	return nil
}

func (m *mapper) validate(_ *cobra.Command, args []string) error {
	if m.ShowAll {
		return nil
	}

	if len(args) != 1 {
		return fmt.Errorf("%w, available %+v", ErrMapSchemeRequired, mapscheme.Names())
	}

	name := args[0]
	if _, err := mapscheme.Find(name); err != nil {
		return err
	}

	return nil
}
