package manager

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

func init() {
	enc := &YAMLEncoder{}
	RegisterFormatWithOptions("yaml", []string{".yml", ".yaml"}, enc.EncodeTo, YAMLDecoder, enc)
}

func YAMLDecoder(r io.Reader) Decoder {
	y := yaml.NewDecoder(r)
	return y
}

type YAMLEncoder struct {
	Indent int `json:"indent,string"`
}

func (e *YAMLEncoder) EncodeTo(w io.Writer) (Encoder, Closer) {
	indent := 2
	if e.Indent > 0 {
		indent = e.Indent
	}

	fmt.Fprintln(w, "---")

	y := yaml.NewEncoder(w)
	y.SetIndent(indent)
	return y, y.Close
}
