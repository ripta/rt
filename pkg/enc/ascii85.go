package enc

import (
	"encoding/ascii85"
	"io"

	"github.com/spf13/cobra"
)

func newAscii85Command(e *encoder) *cobra.Command {
	c := cobra.Command{
		Use:     "ascii85",
		Aliases: []string{"a85"},
		Short:   "ASCII-85",
		RunE:    e.choose(e.a85Encode, e.a85Decode),
	}

	return &c
}

func (e *encoder) a85Decode(dst io.Writer, src io.Reader) error {
	br := ascii85.NewDecoder(src)
	if _, err := io.Copy(dst, br); err != nil {
		return err
	}

	return nil
}

func (e *encoder) a85Encode(dst io.Writer, src io.Reader) error {
	bw := ascii85.NewEncoder(dst)
	defer bw.Close()

	if _, err := io.Copy(bw, src); err != nil {
		return err
	}

	return nil
}

