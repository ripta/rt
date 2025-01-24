package manager

import "io"

type Closer func() error

type Encoder interface {
	Encode(v any) error
}

type EncoderFactory func(io.Writer) (Encoder, Closer)
