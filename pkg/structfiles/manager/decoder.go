package manager

import (
	"fmt"
	"io"
	"reflect"
	"sync/atomic"
)

type Decoder interface {
	// Decode reads the next encoded value from its input and stores it in
	// the value pointed to by v.
	Decode(v any) error
}

type DecoderFactory func(io.Reader) Decoder

// DecoderFunc is an adapter to allow the use of ordinary functions as Decoders.
type DecoderFunc func(any) error

func (f DecoderFunc) Decode(v any) error {
	return f(v)
}

var _ Decoder = DecoderFunc(nil)

// OnceDecoder is a decoder that only decodes the first value from its input.
// Subsequent calls to Decode will return io.EOF. This is useful when handling
// formats that do not have multidocument support, e.g., TOML.
type OnceDecoder struct {
	Decoder

	once atomic.Bool
}

func (d *OnceDecoder) Decode(v any) error {
	if !d.once.CompareAndSwap(false, true) {
		return io.EOF
	}

	return d.Decoder.Decode(v)
}

var _ Decoder = (*OnceDecoder)(nil)

// ToDecoder converts a function that reads from an io.Reader into a Decoder.
func ToDecoder(f func(io.Reader) (any, error), r io.Reader) Decoder {
	return DecoderFunc(func(v any) error {
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Ptr || rv.IsNil() {
			return fmt.Errorf("expected a pointer to a struct, got %T", v)
		}

		out, err := f(r)
		if err != nil {
			return fmt.Errorf("converting: %w", err)
		}

		rv.Elem().Set(reflect.ValueOf(out))
		return nil
	})
}
