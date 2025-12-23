package mapscheme

import (
	"fmt"

	"github.com/ripta/rt/pkg/uni/runerange"
)

var (
	ASCIIUpperRange      = runerange.FromRuneRange('A', 'Z')
	ASCIILowerRange      = runerange.FromRuneRange('a', 'z')
	ASCIIUpperLowerRange = runerange.CombineRuneRanges(ASCIIUpperRange, ASCIILowerRange)
	ASCIIDigitRange      = runerange.FromRuneRange('0', '9')
	ASCIIAllRange        = runerange.CombineRuneRanges(ASCIIUpperLowerRange, ASCIIDigitRange)
)

// GenerateFromRuneRanges generates a rune mapping function from the given rune ranges.
func GenerateFromRuneRanges(from, to runerange.Range) (func(rune) rune, error) {
	fromRunes := from.Runes()
	toRunes := to.Runes()

	if len(fromRunes) != len(toRunes) {
		return nil, fmt.Errorf("%w: from=%d to=%d", ErrGenerateMismatchedLengths, len(fromRunes), len(toRunes))
	}

	mapping := map[rune]rune{}
	for i, r := range fromRunes {
		mapping[r] = toRunes[i]
	}

	return GenerateFromMap(mapping), nil
}

func MustGenerateFromRuneRanges(from, to runerange.Range) func(rune) rune {
	fn, err := GenerateFromRuneRanges(from, to)
	if err != nil {
		panic(err)
	}

	return fn
}
