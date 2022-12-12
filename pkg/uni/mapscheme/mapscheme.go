package mapscheme

import (
	"errors"
	"fmt"
	"sort"
)

var (
	ErrMapschemeAlreadyRegistered = errors.New("mapscheme already registered")

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

	sort.Strings(names)
	return names
}

func MustRegister(name string, fn func(rune) rune) {
	if err := Register(name, fn); err != nil {
		panic(err)
	}
}

func Register(name string, fn func(rune) rune) error {
	if Has(name) {
		return fmt.Errorf("cannot register mapscheme %q: %w", name, ErrMapschemeAlreadyRegistered)
	}

	registry[name] = fn
	return nil
}
