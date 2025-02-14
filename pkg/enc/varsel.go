package enc

import (
	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/enc/varsel"
)

func newVarselCommand(e *encoder) *cobra.Command {
	c := cobra.Command{
		Use:     "varsel",
		Aliases: []string{"vs", "variation-selector"},
		Short:   "Variation selectors",
		Long:    "https://paulbutler.org/2025/smuggling-arbitrary-data-through-an-emoji/",
		RunE:    e.choose(varsel.Encode, varsel.Decode),
	}

	return &c
}
