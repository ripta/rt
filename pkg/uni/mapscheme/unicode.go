package mapscheme

import "unicode"

func init() {
	registry["lower"] = unicode.ToLower
	registry["title"] = unicode.ToTitle
	registry["upper"] = unicode.ToUpper
}
