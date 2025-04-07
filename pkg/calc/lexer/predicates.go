package lexer

import (
	"strings"
	"unicode"
)

func IsAlnum(r rune) bool {
	if unicode.IsLetter(r) {
		return true
	}
	if unicode.IsDigit(r) {
		return true
	}
	return false
}

func IsNumeric(r rune) bool {
	if r >= unicode.MaxLatin1 {
		return false
	}

	return (r >= '0' && r <= '9') || r == '.' || r == '_'
}

func StringPredicate(valid string) func(rune) bool {
	return func(r rune) bool {
		return strings.ContainsRune(valid, r)
	}
}
