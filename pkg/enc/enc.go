package enc

import (
	"io"
	"os"

	"github.com/spf13/cobra"
)

type encoder struct {
	Decode bool
}

func (e *encoder) choose(enc, dec func(io.Writer, io.Reader) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		w, r := os.Stdout, os.Stdin

		if e.Decode {
			return dec(w, r)
		}
		return enc(w, r)
	}
}

func NewCommand() *cobra.Command {
	e := &encoder{}
	c := cobra.Command{
		Use:   "enc",
		Short: "Encodings (and their decodings)",
	}

	c.PersistentFlags().BoolVarP(&e.Decode, "decode", "d", false, "Decode instead of encode")

	c.AddCommand(newBase64Command(e))
	return &c
}
