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

	encOpts any
	decOpts any
}

var registry = map[string]registryEntry{}

func RegisterFormat(name string, exts []string, enc EncoderFactory, dec DecoderFactory) {
	registerFormat(name, exts, enc, dec, false, nil, nil)
}

func RegisterFormatWithOptions(name string, exts []string, enc EncoderFactory, encOpts any, dec DecoderFactory, decOpts any) {
	registerFormat(name, exts, enc, dec, false, encOpts, decOpts)
}

func ReplaceFormat(name string, exts []string, enc EncoderFactory, dec DecoderFactory) {
	registerFormat(name, exts, enc, dec, true, nil, nil)
}

func UnregisterFormat(name string) {
	delete(registry, name)
}

func registerFormat(name string, exts []string, enc EncoderFactory, dec DecoderFactory, override bool, encOpts, decOpts any) {
	if !override {
		if _, dup := registry[name]; dup {
			panic("structfiles: Register called twice for " + name)
		}
	}

	// We need to check in order that options is not nil, that it's a pointer,
	// that its non-nil interface doesn't have an underlying nil value, and
	// that it's a struct
	if encOpts == nil {
		encOpts = nil
	} else if reflect.ValueOf(encOpts).Kind() != reflect.Ptr {
		panic("structfiles: encoder options for format '" + name + "' must be nil or a pointer")
	} else if reflect.ValueOf(encOpts).IsNil() {
		encOpts = nil
	} else if reflect.ValueOf(encOpts).Elem().Kind() != reflect.Struct {
		panic("structfiles: encoder options for format '" + name + "' must be a pointer to a struct")
	}

	if decOpts == nil {
		decOpts = nil
	} else if reflect.ValueOf(decOpts).Kind() != reflect.Ptr {
		panic("structfiles: decoder options for format '" + name + "' must be nil or a pointer")
	} else if reflect.ValueOf(decOpts).IsNil() {
		decOpts = nil
	} else if reflect.ValueOf(decOpts).Elem().Kind() != reflect.Struct {
		panic("structfiles: decoder options for format '" + name + "' must be a pointer to a struct")
	}

	registry[name] = registryEntry{
		name: name,
		exts: exts,
		enc:  enc,
		dec:  dec,

		encOpts: encOpts,
		decOpts: decOpts,
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

func GetDecoderFactory(name string, opts map[string]string) (DecoderFactory, error) {
	if _, ok := registry[name]; !ok {
		return nil, nil
	}

	dec := registry[name].dec
	if len(opts) == 0 || registry[name].decOpts == nil {
		return dec, nil
	}

	bs, err := json.Marshal(opts)
	if err != nil {
		return nil, err
	}

	d := json.NewDecoder(bytes.NewReader(bs))
	d.DisallowUnknownFields()

	if err := d.Decode(registry[name].decOpts); err != nil {
		return nil, err
	}

	if v, ok := registry[name].decOpts.(Validator); ok {
		if err := v.Validate(); err != nil {
			return nil, err
		}
	}

	return dec, nil
}

func GetEncoderFactory(name string, opts map[string]string) (EncoderFactory, error) {
	if _, ok := registry[name]; !ok {
		return nil, nil
	}

	enc := registry[name].enc
	if len(opts) == 0 || registry[name].encOpts == nil {
		return enc, nil
	}

	bs, err := json.Marshal(opts)
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(bs))
	dec.DisallowUnknownFields()

	if err := dec.Decode(registry[name].encOpts); err != nil {
		return nil, err
	}

	if v, ok := registry[name].encOpts.(Validator); ok {
		if err := v.Validate(); err != nil {
			return nil, err
		}
	}

	return enc, nil
}

func GetDecoderOptions(name string) map[string]string {
	if _, ok := registry[name]; !ok {
		return nil
	}
	if registry[name].decOpts == nil {
		return nil
	}

	return optsToMap(registry[name].decOpts)
}

func GetEncoderOptions(name string) map[string]string {
	if _, ok := registry[name]; !ok {
		return nil
	}
	if registry[name].encOpts == nil {
		return nil
	}

	return optsToMap(registry[name].encOpts)
}

func optsToMap(opts any) map[string]string {
	optsMap := map[string]string{}

	rv := reflect.ValueOf(opts).Elem()
	for i := 0; i < rv.NumField(); i++ {
		fieldName := rv.Type().Field(i).Tag.Get("json")
		if fieldName == "" {
			fieldName = rv.Type().Field(i).Name
		}

		if prefix, _, ok := strings.Cut(fieldName, ","); ok {
			fieldName = prefix
		}

		optsMap[fieldName] = rv.Field(i).Type().String()
	}

	return optsMap
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
