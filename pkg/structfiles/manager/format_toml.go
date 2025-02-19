package manager

import (
	"github.com/BurntSushi/toml"
	"io"
)

func init() {
	RegisterFormat("toml", []string{".toml"}, TOMLEncoder, TOMLDecoder)
}

func TOMLDecoder(r io.Reader) Decoder {
	t := toml.NewDecoder(r)
	d := func(v any) error {
		_, err := t.Decode(v)
		return err
	}

	return DecoderFunc(d)
}

func TOMLEncoder(w io.Writer) (Encoder, Closer) {
	return toml.NewEncoder(w), noCloser
}
