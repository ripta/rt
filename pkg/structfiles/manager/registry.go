package manager

type registryEntry struct {
	name string
	exts []string

	enc EncoderFactory
	dec DecoderFactory
}

var registry = map[string]registryEntry{}

func RegisterFormat(name string, exts []string, enc EncoderFactory, dec DecoderFactory) {
	registerFormat(name, exts, enc, dec, false)
}

func ReplaceFormat(name string, exts []string, enc EncoderFactory, dec DecoderFactory) {
	registerFormat(name, exts, enc, dec, true)
}

func UnregisterFormat(name string) {
	delete(registry, name)
}

func registerFormat(name string, exts []string, enc EncoderFactory, dec DecoderFactory, override bool) {
	if !override {
		if _, dup := registry[name]; dup {
			panic("structfiles: Register called twice for " + name)
		}
	}

	registry[name] = registryEntry{
		name: name,
		exts: exts,
		enc:  enc,
		dec:  dec,
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

func GetEncoderFactory(name string) EncoderFactory {
	if _, ok := registry[name]; !ok {
		return nil
	}

	return registry[name].enc
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

	return formats
}
