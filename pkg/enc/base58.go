package enc

import (
	"io"

	"github.com/mr-tron/base58"
	"github.com/spf13/cobra"
)

func newBase58Command(e *encoder) *cobra.Command {
	c := cobra.Command{
		Use:     "base58",
		Aliases: []string{"b58"},
		Short:   "Base58",
		RunE:    e.choose(e.b58Encode, e.b58Decode),
	}

	return &c
}

func (e *encoder) b58Decode(dst io.Writer, src io.Reader) error {
	bs, err := io.ReadAll(src)
	if err != nil {
		return err
	}

	out, err := base58.Decode(string(bs))
	if err != nil {
		return err
	}

	if _, err := dst.Write([]byte(out)); err != nil {
		return err
	}
	return nil
}

func (e *encoder) b58Encode(dst io.Writer, src io.Reader) error {
	bs, err := io.ReadAll(src)
	if err != nil {
		return err
	}

	if _, err := dst.Write([]byte(base58.Encode(bs))); err != nil {
		return err
	}
	return nil
}
