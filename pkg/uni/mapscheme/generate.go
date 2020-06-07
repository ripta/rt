package mapscheme

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrGenerateMismatchedLengths = errors.New("len(from) does not match len(to)")
)

func GenerateFromMap(m map[rune]rune) func(rune) rune {
	return func(s rune) rune {
		if t, ok := m[s]; ok {
			return t
		}
		return s
	}
}

func GenerateFromTransform(fn func(rune) rune) func(string) string {
	return func(s string) string {
		return strings.Map(fn, s)
	}
}

func GenerateFromString(from, to string) (func(rune) rune, error) {
	rfrom := []rune(from)
	rto := []rune(to)
	if len(rfrom) != len(rto) {
		return nil, fmt.Errorf("%w: from=%d to=%d", ErrGenerateMismatchedLengths, len(rfrom), len(rto))
	}

	dfrom := make(map[rune]rune)
	for i, r := range rfrom {
		dfrom[r] = rto[i]
	}

	return GenerateFromMap(dfrom), nil
}

func MustGenerateFromString(from, to string) func(rune) rune {
	fn, err := GenerateFromString(from, to)
	if err != nil {
		panic(err)
	}
	return fn
}
