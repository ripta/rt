package display

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"unicode"

	"golang.org/x/text/unicode/runenames"
)

type displayFunc func(rune) string

var displayFuncs = map[string]displayFunc{
	"id": func(r rune) string {
		return fmt.Sprintf("%U", r)
	},
	"rune": func(r rune) string {
		v := string(r)
		if unicode.IsControl(r) {
			v = fmt.Sprintf("%q", string(r))
		}
		return v
	},
	"hexbytes": func(r rune) string {
		return fmt.Sprintf("[%s]", RuneToHexBytes(r))
	},
	"cats": func(r rune) string {
		return fmt.Sprintf("<%s>", RuneToCategories(r))
	},
	"categories": func(r rune) string {
		return fmt.Sprintf("<%s>", RuneToCategories(r))
	},
	"name": func(r rune) string {
		return runenames.Name(r)
	},
}

type Config struct {
	columns []string
}

var (
	ErrNoColumnsSpecified = errors.New("no columns specified")
	ErrInvalidColumn      = errors.New("invalid column name")
)

func New(cols []string) (*Config, error) {
	if len(cols) == 0 {
		return nil, ErrNoColumnsSpecified
	}

	for _, col := range cols {
		if _, ok := displayFuncs[col]; !ok {
			names := slices.Sorted(maps.Keys(displayFuncs))
			return nil, fmt.Errorf("%w: %q, expecting one of %v", ErrInvalidColumn, col, names)
		}
	}

	return &Config{
		columns: cols,
	}, nil
}

func (d *Config) Generate(r rune) []string {
	disp := []string{}
	for _, col := range d.columns {
		if f, ok := displayFuncs[col]; ok {
			disp = append(disp, f(r))
		}
	}

	return disp
}
