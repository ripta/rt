package manager

import (
	"encoding/csv"
	"fmt"
	"io"
	"reflect"

	"github.com/ripta/rt/pkg/structfiles/csvmap"
)

func init() {
	opts := &CSVOptions{}
	RegisterFormatWithOptions("csv", []string{".csv"}, opts.CSVEncoder, opts, opts.CSVDecoder, opts)
}

type CSVOptions struct {
	Separator string `json:"sep"`
}

func (opts *CSVOptions) Validate() error {
	if len(opts.Separator) > 1 {
		return fmt.Errorf("CSV separator must be at most a single rune, got %d runes (%q)", len(opts.Separator), opts.Separator)
	}
	return nil
}

func (opts *CSVOptions) CSVDecoder(r io.Reader) Decoder {
	h := func(cr *csv.Reader) {
		if opts.Separator != "" {
			cr.Comma = rune(opts.Separator[0])
		}
	}

	d, err := csvmap.CustomDecode(r, h)

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

func (opts *CSVOptions) CSVEncoder(w io.Writer) (Encoder, Closer) {
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
		h := func(cw *csv.Writer) {
			if opts.Separator != "" {
				cw.Comma = rune(opts.Separator[0])
			}
		}

		return csvmap.CustomEncode(w, h, d)
	}
}
