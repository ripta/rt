package uni

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/norm"
)

func newNFCCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "nfc",
		Aliases: []string{"com", "comp", "compose"},
		Short:   "Output the canonical composition form for input",
		RunE:    generateForm(norm.NFC),
	}
}

func newNFDCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "nfd",
		Aliases: []string{"dec", "decompose"},
		Short:   "Output the canonical decomposition form for input",
		RunE:    generateForm(norm.NFD),
	}
}

func newNFKCCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "nfkc",
		Short: "Output the compatibility composition form for input",
		RunE:  generateForm(norm.NFKC),
	}
}

func newNFKDCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "nfkd",
		Short: "Output the compatibility decomposition form for input",
		RunE:  generateForm(norm.NFKD),
	}
}

func generateForm(form norm.Form) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, args []string) error {
		in, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading from stdin: %w", err)
		}

		n := norm.Iter{}
		n.Init(form, in)
		for !n.Done() {
			fmt.Print(string(n.Next()))
		}

		return nil
	}
}
