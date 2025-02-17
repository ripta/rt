package display

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func RuneToHexBytes(r rune) string {
	bytes := make([]byte, utf8.UTFMax)
	utf8.EncodeRune(bytes, r)

	hexbytes := []string{}
	for _, b := range bytes {
		if b == 0 {
			hexbytes = append(hexbytes, "  ")
			continue
		}
		hexbytes = append(hexbytes, fmt.Sprintf("%02X", b))
	}

	return strings.Join(hexbytes, " ")
}

func RuneToCategories(r rune) string {
	cats := []string{}
	for cat, rt := range unicode.Categories {
		if unicode.Is(rt, r) {
			cats = append(cats, cat)
		}
	}
	sort.Strings(cats)
	return strings.Join(cats, ",")
}

func RuneToScript(r rune) string {
	for name, rt := range unicode.Scripts {
		if unicode.Is(rt, r) {
			return name
		}
	}
	return ""
}
