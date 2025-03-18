package manager

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

type registryEntry struct {
	name string
	exts []string

	enc EncoderFactory
	dec DecoderFactory

	opts any
}

var registry = map[string]registryEntry{}

func RegisterFormat(name string, exts []string, enc EncoderFactory, dec DecoderFactory) {
	registerFormat(name, exts, enc, dec, false, nil)
}

func RegisterFormatWithOptions(name string, exts []string, enc EncoderFactory, dec DecoderFactory, opts any) {
	registerFormat(name, exts, enc, dec, false, opts)
}

func ReplaceFormat(name string, exts []string, enc EncoderFactory, dec DecoderFactory) {
	registerFormat(name, exts, enc, dec, true, nil)
}

func UnregisterFormat(name string) {
	delete(registry, name)
}

func registerFormat(name string, exts []string, enc EncoderFactory, dec DecoderFactory, override bool, opts any) {
	if !override {
		if _, dup := registry[name]; dup {
			panic("structfiles: Register called twice for " + name)
		}
	}

	// We need to check in order that options is not nil, that it's a pointer,
	// that its non-nil interface doesn't have an underlying nil value, and
	// that it's a struct
	if opts == nil {
		opts = nil
	} else if reflect.ValueOf(opts).Kind() != reflect.Ptr {
		panic("structfiles: options for format '" + name + "' must be nil or a pointer")
	} else if reflect.ValueOf(opts).IsNil() {
		opts = nil
	} else if reflect.ValueOf(opts).Elem().Kind() != reflect.Struct {
		panic("structfiles: options for format '" + name + "' must be a pointer to a struct")
	}

	registry[name] = registryEntry{
		name: name,
		exts: exts,
		enc:  enc,
		dec:  dec,
		opts: opts,
	}
}

func FindByExtension(ext string) string {
	for name, entry := range registry {
		for _, e := range entry.exts {
			if e == ext {
				return name
			}
		}
	}

	return ""
}

func GetDecoderFactory(name string) DecoderFactory {
	if _, ok := registry[name]; !ok {
		return nil
	}

	return registry[name].dec
}

func GetEncoderFactory(name string, opts map[string]string) (EncoderFactory, error) {
	if _, ok := registry[name]; !ok {
		return nil, nil
	}

	enc := registry[name].enc
	if len(opts) == 0 || registry[name].opts == nil {
		return enc, nil
	}

	bs, err := json.Marshal(opts)
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(bs))
	dec.DisallowUnknownFields()
	return enc, dec.Decode(registry[name].opts)
}

func GetEncoderOptions(name string) map[string]string {
	if _, ok := registry[name]; !ok {
		return nil
	}
	if registry[name].opts == nil {
		return nil
	}

	opts := map[string]string{}

	rv := reflect.ValueOf(registry[name].opts).Elem()
	for i := 0; i < rv.NumField(); i++ {
		fieldName := rv.Type().Field(i).Tag.Get("json")
		if fieldName == "" {
			fieldName = rv.Type().Field(i).Name
		}

		if prefix, _, ok := strings.Cut(fieldName, ","); ok {
			fieldName = prefix
		}

		opts[fieldName] = rv.Field(i).Type().String()
	}

	return opts
}

func GetExtensions(name string) []string {
	if _, ok := registry[name]; !ok {
		return nil
	}

	return registry[name].exts
}

func GetFormats() []string {
	formats := make([]string, 0, len(registry))
	for name := range registry {
		formats = append(formats, name)
	}

	sort.Strings(formats)
	return formats
}
