package manager

import (
	"fmt"
	"io"
	"reflect"

	"github.com/go-logfmt/logfmt"
)

func init() {
	opts := &LogfmtOptions{}
	RegisterFormatWithOptions("logfmt", []string{".logfmt"}, opts.LogfmtEncoder, opts, opts.LogfmtDecoder, opts)
}

type LogfmtOptions struct{}

func (opts *LogfmtOptions) Validate() error {
	return nil
}

func (opts *LogfmtOptions) LogfmtDecoder(r io.Reader) Decoder {
	l := logfmt.NewDecoder(r)
	return DecoderFunc(func(v any) error {
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Ptr || rv.IsNil() {
			return fmt.Errorf("expected a pointer to a struct, got %T", v)
		}

		if err := l.Err(); err != nil {
			return fmt.Errorf("decoder error: %w", err)
		}

		if !l.ScanRecord() {
			if err := l.Err(); err != nil {
				return fmt.Errorf("decoder error while scanning record: %w", err)
			}
			return io.EOF
		}

		m := map[string]any{}
		for l.ScanKeyval() {
			m[string(l.Key())] = string(l.Value())
		}

		rv.Elem().Set(reflect.ValueOf(m))
		return nil
	})
}

func (opts *LogfmtOptions) LogfmtEncoder(w io.Writer) (Encoder, Closer) {
	l := logfmt.NewEncoder(w)
	e := func(v any) error {
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Ptr || rv.IsNil() {
			return fmt.Errorf("expected a pointer, got %T", v)
		}

		val := rv.Elem().Interface()
		rec, ok := val.(map[string]any)
		if !ok {
			return fmt.Errorf("expected a map[string]any, got %T", val)
		}

		for k, v := range rec {
			if err := l.EncodeKeyval(k, v); err != nil {
				return fmt.Errorf("error encoding key type %T value type %T: %w", k, v, err)
			}
		}

		return l.EndRecord()
	}

	return EncoderFunc(e), noCloser
}
