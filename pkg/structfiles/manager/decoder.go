package manager

import "io"

type Decoder interface {
	// Decode reads the next encoded value from its input and stores it in
	// the value pointed to by v.
	Decode(v any) error
}

type DecoderFactory func(io.Reader) Decoder

type DecoderFunc func(any) error

func (f DecoderFunc) Decode(v any) error {
	return f(v)
}

var _ Decoder = DecoderFunc(nil)
