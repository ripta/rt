package manager

import "io"

type Closer func() error

func noCloser() error {
	return nil
}

type Encoder interface {
	Encode(v any) error
}

type EncoderFactory func(io.Writer) (Encoder, Closer)
