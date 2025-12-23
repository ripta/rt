package mapscheme

import "unicode"

func init() {
	registry["lower"] = unicode.ToLower
	registry["title"] = unicode.ToTitle
	registry["upper"] = unicode.ToUpper
	registry["toggle"] = func(r rune) rune {
		if unicode.IsLower(r) {
			return unicode.ToUpper(r)
		} else if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return r
	}
}
