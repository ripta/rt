package uni

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/norm"
)

func newNFDCommand() *cobra.Command {
	c := cobra.Command{
		Use:     "nfd",
		Aliases: []string{"dec", "decompose"},
		Short:   "Output the canonical decomposition form for input",
		RunE:    runNFD,
	}

	return &c
}

func runNFD(cmd *cobra.Command, args []string) error {
	in, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading from stdin: %w", err)
	}

	n := norm.Iter{}
	n.Init(norm.NFD, in)
	for !n.Done() {
		fmt.Print(string(n.Next()))
	}

	return nil
}
