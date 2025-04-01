package manager

import (
	"io"
	"strings"

	"github.com/BurntSushi/toml"
)

func init() {
	enc := &TOMLEncoder{}
	RegisterFormatWithOptions("toml", []string{".toml"}, enc.EncodeTo, enc, TOMLDecoder, nil)
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

type TOMLEncoder struct {
	Indent int `json:"indent,string"`
}

func (e *TOMLEncoder) EncodeTo(w io.Writer) (Encoder, Closer) {
	indent := 2
	if e.Indent > 0 {
		indent = e.Indent
	}

	t := toml.NewEncoder(w)
	t.Indent = strings.Repeat(" ", indent)
	return t, noCloser
}
