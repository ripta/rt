package uni

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/norm"
)

func newNFCCommand() *cobra.Command {
	c := cobra.Command{
		Use:   "nfc",
		Aliases: []string{"com", "comp", "compose"},
		Short: "Output the canonical decomposition form for input",
		RunE:   runNFC,
	}

	return &c
}

func runNFC(cmd *cobra.Command, args []string) error {
	in, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading from stdin: %w", err)
	}

	n := norm.Iter{}
	n.Init(norm.NFC, in)
	for !n.Done() {
		fmt.Print(string(n.Next()))
	}

	return nil
}
