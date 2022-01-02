package enc

import (
	"encoding/base64"
	"io"

	"github.com/spf13/cobra"
)

func newBase64Command(e *encoder) *cobra.Command {
	c := cobra.Command{
		Use:     "base64",
		Aliases: []string{"b64"},
		Short:   "Base64",
		RunE:    e.choose(e.b64Encode, e.b64Decode),
	}

	return &c
}

func (e *encoder) b64Decode(dst io.Writer, src io.Reader) error {
	br := base64.NewDecoder(base64.StdEncoding.WithPadding(base64.NoPadding), src)
	if _, err := io.Copy(dst, br); err != nil {
		return err
	}

	return nil
}

func (e *encoder) b64Encode(dst io.Writer, src io.Reader) error {
	bw := base64.NewEncoder(base64.StdEncoding, dst)
	defer bw.Close()

	if _, err := io.Copy(bw, src); err != nil {
		return err
	}

	return nil
}
