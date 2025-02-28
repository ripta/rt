package enc

import (
	"encoding/base32"
	"io"

	"github.com/spf13/cobra"
)

func newBase32Command(e *encoder) *cobra.Command {
	c := cobra.Command{
		Use:     "base32",
		Aliases: []string{"b32"},
		Short:   "Base32",
		RunE:    e.choose(e.b32Encode, e.b32Decode),
	}

	return &c
}

func (e *encoder) b32Decode(dst io.Writer, src io.Reader) error {
	br := base32.NewDecoder(base32.StdEncoding.WithPadding(base32.NoPadding), src)
	if _, err := io.Copy(dst, br); err != nil {
		return err
	}

	return nil
}

func (e *encoder) b32Encode(dst io.Writer, src io.Reader) error {
	bw := base32.NewEncoder(base32.StdEncoding, dst)
	defer bw.Close()

	if _, err := io.Copy(bw, src); err != nil {
		return err
	}

	return nil
}
