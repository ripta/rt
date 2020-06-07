package mapscheme

var (
	registry = make(map[string]func(rune) rune)
)

func Get(name string) func(rune) rune {
	return registry[name]
}

func Has(name string) bool {
	_, ok := registry[name]
	return ok
}

func Lookup(name string) (func(rune) rune, bool) {
	fn, ok := registry[name]
	return fn, ok
}

func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
