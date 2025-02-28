package enc

import (
	"encoding/hex"
	"io"

	"github.com/spf13/cobra"
)

func newHexCommand(e *encoder) *cobra.Command {
	c := cobra.Command{
		Use:   "hex",
		Short: "Hexadecimal",
		RunE:  e.choose(e.hexEncode, e.hexDecode),
	}

	return &c
}

func (e *encoder) hexDecode(dst io.Writer, src io.Reader) error {
	br := hex.NewDecoder(src)
	if _, err := io.Copy(dst, br); err != nil {
		return err
	}

	return nil
}

func (e *encoder) hexEncode(dst io.Writer, src io.Reader) error {
	bw := hex.NewEncoder(dst)
	if _, err := io.Copy(bw, src); err != nil {
		return err
	}

	return nil
}
