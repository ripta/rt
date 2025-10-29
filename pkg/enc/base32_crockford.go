package enc

import (
	"encoding/base32"
	"io"

	"github.com/spf13/cobra"
)

// Crockford's Base32 alphabet, which excludes I, L, O, and U.
const b32CrockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var b32CrockfordEncoding = base32.NewEncoding(b32CrockfordAlphabet)

func newBase32CrockfordCommand(e *encoder) *cobra.Command {
	c := cobra.Command{
		Use:     "crockford",
		Aliases: []string{"b32_crockford", "b32c"},
		Short:   "Base32 with Crockford's alphabet",
		RunE:    e.choose(e.b32CrockfordEncode, e.b32CrockfordDecode),
	}

	return &c
}

func (e *encoder) b32CrockfordDecode(dst io.Writer, src io.Reader) error {
	br := base32.NewDecoder(b32CrockfordEncoding.WithPadding(base32.NoPadding), src)
	if _, err := io.Copy(dst, br); err != nil {
		return err
	}

	return nil
}

func (e *encoder) b32CrockfordEncode(dst io.Writer, src io.Reader) error {
	bw := base32.NewEncoder(b32CrockfordEncoding, dst)
	defer bw.Close()

	if _, err := io.Copy(bw, src); err != nil {
		return err
	}

	return nil
}
