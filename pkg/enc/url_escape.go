package enc

import (
	"io"
	"net/url"

	"github.com/spf13/cobra"
)

func newURLEscapeCommand(e *encoder) *cobra.Command {
	c := cobra.Command{
		Use:   "url",
		Short: "URL escape/unescape",
		RunE:  e.choose(e.urlEscape, e.urlUnescape),
	}

	return &c
}

func (e *encoder) urlEscape(dst io.Writer, src io.Reader) error {
	in, err := io.ReadAll(src)
	if err != nil {
		return err
	}

	out := url.QueryEscape(string(in))
	if _, err := io.WriteString(dst, out); err != nil {
		return err
	}

	return nil
}

func (e *encoder) urlUnescape(dst io.Writer, src io.Reader) error {
	in, err := io.ReadAll(src)
	if err != nil {
		return err
	}

	out, err := url.QueryUnescape(string(in))
	if err != nil {
		return err
	}
	if _, err := dst.Write([]byte(out)); err != nil {
		return err
	}

	return nil
}
