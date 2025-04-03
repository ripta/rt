package manager

import (
	"fmt"
	"io"
	"reflect"
	"strings"

	"olympos.io/encoding/edn"
)

func init() {
	enc := &EDNEncoder{}
	RegisterFormatWithOptions("edn", []string{".edn"}, enc.EncodeTo, enc, EDNDecoder, nil)
}

// EDNDecoder currently decodes any key, not just string, so it behaves differently
// from other decoders.
func EDNDecoder(r io.Reader) Decoder {
	dec := edn.NewDecoder(r)
	return DecoderFunc(func(v any) error {
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Ptr || rv.IsNil() {
			return fmt.Errorf("expected a pointer to a struct, got %T", v)
		}

		av := map[any]any{}
		if err := dec.Decode(&av); err != nil {
			return err
		}

		sv := map[string]any{}
		ConvertAnyMapToStringMapRecursive(sv, av)
		rv.Elem().Set(reflect.ValueOf(sv))

		return nil
	})
}

type EDNEncoder struct {
	Prefix string `json:"prefix"`
	Indent int    `json:"indent,string"`
}

func (e *EDNEncoder) EncodeTo(w io.Writer) (Encoder, Closer) {
	if e.Indent == 0 && e.Prefix == "" {
		return edn.NewEncoder(w), noCloser
	}

	enc := edn.NewEncoder(w)
	return EncoderFunc(func(v any) error {
		return enc.EncodeIndent(v, e.Prefix, strings.Repeat(" ", e.Indent))
	}), noCloser
}
