package manager

import "io"

type Decoder interface {
	// Decode reads the next encoded value from its input and stores it in
	// the value pointed to by v.
	Decode(v any) error
}

type DecoderFactory func(io.Reader) Decoder
