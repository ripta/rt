package manager

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

func init() {
	RegisterFormat("yaml", []string{".yml", ".yaml"}, YAMLEncoder, YAMLDecoder)
}

func YAMLDecoder(r io.Reader) Decoder {
	y := yaml.NewDecoder(r)
	return y
}

func YAMLEncoder(w io.Writer) (Encoder, Closer) {
	fmt.Fprintln(w, "---")

	y := yaml.NewEncoder(w)
	y.SetIndent(2)
	return y, y.Close
}
