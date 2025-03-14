package manager

import (
	"encoding/gob"
	"io"
)

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	RegisterFormat("gob", []string{".gob"}, GobEncoder, GobDecoder)
}

func GobDecoder(r io.Reader) Decoder {
	return gob.NewDecoder(r)
}

func GobEncoder(w io.Writer) (Encoder, Closer) {
	return gob.NewEncoder(w), noCloser
}
