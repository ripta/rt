package manager

import (
	"io"

	"github.com/BurntSushi/toml"
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

	return &OnceDecoder{
		Decoder: DecoderFunc(d),
	}
}

func TOMLEncoder(w io.Writer) (Encoder, Closer) {
	return toml.NewEncoder(w), noCloser
}
