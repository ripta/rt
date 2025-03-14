package manager

import (
	"fmt"
	"io"
	"reflect"

	"github.com/ripta/rt/pkg/structfiles/csvmap"
)

func init() {
	RegisterFormat("csv", []string{".csv"}, CSVEncoder, CSVDecoder)
}

func CSVDecoder(r io.Reader) Decoder {
	d, err := csvmap.Decode(r)

	num := 0
	return DecoderFunc(func(v any) error {
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Ptr || rv.IsNil() {
			return fmt.Errorf("expected a pointer to a struct, got %T", v)
		}

		if err != nil {
			return err
		}
		if num >= d.Len() {
			return io.EOF
		}

		rv.Elem().Set(reflect.ValueOf(d.Record(num)))
		num++

		return nil
	})
}

func CSVEncoder(w io.Writer) (Encoder, Closer) {
	d := &csvmap.Document{}
	e := func(v any) error {
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Ptr || rv.IsNil() {
			return fmt.Errorf("expected a pointer, got %T", v)
		}

		val := rv.Elem().Interface()
		rec, ok := val.(map[string]any)
		if !ok {
			return fmt.Errorf("expected pointer to map[string]any, got %T", v)
		}

		if len(d.Header) == 0 {
			for k := range rec {
				d.Header = append(d.Header, k)
			}
		}

		return d.Append(rec)
	}

	return EncoderFunc(e), func() error {
		return csvmap.Encode(w, d)
	}
}
