package mapscheme

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrMapschemeAlreadyRegistered = errors.New("mapscheme already registered")

	ErrMapschemeNotFound  = errors.New("mapscheme not found")
	ErrMapschemeNotUnique = errors.New("mapscheme not unique")

	registry = make(map[string]func(rune) rune)
)

func Find(name string) (func(rune) rune, error) {
	if fn, ok := registry[name]; ok {
		return fn, nil
	}

	cands := []string{}
	for k := range registry {
		if strings.Contains(k, name) {
			cands = append(cands, k)
		}
	}

	if len(cands) == 0 {
		return nil, fmt.Errorf("mapscheme %q: %w", name, ErrMapschemeNotFound)
	}
	if len(cands) == 1 {
		return registry[cands[0]], nil
	}

	return nil, fmt.Errorf("mapscheme %q: %w (candidates: %s)", name, ErrMapschemeNotUnique, strings.Join(cands, ", "))
}

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
