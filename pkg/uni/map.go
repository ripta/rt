package uni

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/ripta/rt/pkg/uni/mapscheme"
	"github.com/spf13/cobra"
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

	return &c
}

type mapper struct {
}

func (m *mapper) run(c *cobra.Command, args []string) error {
	name := args[0]
	scheme := mapscheme.Get(name)

	in, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading from stdin: %w", err)
	}

	out := strings.Map(scheme, string(in))
	fmt.Print(out)
	return nil
}

func (m *mapper) validate(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w, available %+v", ErrMapSchemeRequired, mapscheme.Names())
	}

	name := args[0]
	if !mapscheme.Has(name) {
		return fmt.Errorf("%w, available %+v", ErrMapSchemeUnknown, mapscheme.Names())
	}

	return nil
}
