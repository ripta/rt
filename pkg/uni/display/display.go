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
	"script": func(r rune) string {
		return fmt.Sprintf("<%s>", RuneToScript(r))
	},
	"name": func(r rune) string {
		return runenames.Name(r)
	},
}

var displayGroups = map[string][]string{
	"all":     {"id", "rune", "hexbytes", "cats", "script", "name"},
	"bytes":   {"id", "rune", "hexbytes"},
	"default": {"id", "rune", "hexbytes", "cats", "name"},
	"short":   {"id", "rune", "hexbytes", "name"},
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

	cleanCols := []string{}
	for _, col := range cols {
		if group, ok := displayGroups[col]; ok {
			for _, gcol := range group {
				if _, ok := displayFuncs[gcol]; !ok {
					names := slices.Sorted(maps.Keys(displayFuncs))
					return nil, fmt.Errorf("%w: %q, expecting one of %v", ErrInvalidColumn, gcol, names)
				}

				cleanCols = append(cleanCols, gcol)
			}
			continue
		}

		if _, ok := displayFuncs[col]; !ok {
			names := slices.Sorted(maps.Keys(displayFuncs))
			return nil, fmt.Errorf("%w: %q, expecting one of %v", ErrInvalidColumn, col, names)
		}

		cleanCols = append(cleanCols, col)
	}

	return &Config{
		columns: cleanCols,
	}, nil
}

func Default() *Config {
	cfg, err := New(DefaultColumns())
	if err != nil {
		panic(err)
	}

	return cfg
}

func DefaultColumns() []string {
	return []string{"id", "rune", "hexbytes", "cats", "name"}
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
