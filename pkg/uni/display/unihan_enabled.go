//go:build unihan

package display

import (
	"github.com/ripta/unihan/pkg/data"
	"github.com/ripta/unihan/pkg/definitions"
)

func init() {
	displayFuncs["cantonese"] = unihan(definitions.Cantonese)
	displayFuncs["hangul"] = unihan(definitions.Hangul)
	displayFuncs["japanese"] = unihan(definitions.Japanese)
	displayFuncs["korean"] = unihan(definitions.Korean)
	displayFuncs["mandarin"] = unihan(definitions.Mandarin)
	displayFuncs["unihan"] = unihan(definitions.Definition)
	displayFuncs["vietnamese"] = unihan(definitions.Vietnamese)

	displayGroups["cjk"] = []string{
		"id",
		"rune",
		"cantonese",
		"japanese",
		"korean",
		"mandarin",
		"unihan",
	}
}

func unihan(def definitions.UnihanReadingKeyPosition) displayFunc {
	return func(r rune) string {
		reading, ok := data.UnihanReadings[r]
		if !ok {
			return ""
		}

		return reading[def]
	}
}
