package display

import (
	"strings"

	"golang.org/x/text/width"
)

// Width describes one East Asian Width property value (UAX #11).
type Width struct {
	Abbrev string
	Name   string
	Kind   width.Kind
}

// Widths lists every East Asian Width property value, keyed by its UAX #11
// abbreviation.
var Widths = []Width{
	{Abbrev: "A", Name: "Ambiguous", Kind: width.EastAsianAmbiguous},
	{Abbrev: "F", Name: "Fullwidth", Kind: width.EastAsianFullwidth},
	{Abbrev: "H", Name: "Halfwidth", Kind: width.EastAsianHalfwidth},
	{Abbrev: "N", Name: "Neutral", Kind: width.Neutral},
	{Abbrev: "Na", Name: "Narrow", Kind: width.EastAsianNarrow},
	{Abbrev: "W", Name: "Wide", Kind: width.EastAsianWide},
}

// LookupWidth finds an East Asian Width value by abbreviation or name, ignoring
// case.
func LookupWidth(s string) (Width, bool) {
	for _, w := range Widths {
		if strings.EqualFold(s, w.Abbrev) || strings.EqualFold(s, w.Name) {
			return w, true
		}
	}
	return Width{}, false
}

// RuneToWidth returns the abbreviation of the rune's East Asian Width.
func RuneToWidth(r rune) string {
	k := width.LookupRune(r).Kind()
	for _, w := range Widths {
		if w.Kind == k {
			return w.Abbrev
		}
	}
	return ""
}
