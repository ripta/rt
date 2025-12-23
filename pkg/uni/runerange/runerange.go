package runerange

type Range interface {
	Contains(r rune) bool
	Len() int
	Runes() []rune
}

// runeRange represents a range of runes from From to To, inclusive, without
// the distinction of 16-bit and 32-bit runes that unicode.RangeTable has
type runeRange struct {
	// From is the starting rune of the range.
	From rune
	// To is the ending rune of the range.
	To rune
}

var _ Range = runeRange{}

func (rr runeRange) Contains(r rune) bool {
	return r >= rr.From && r <= rr.To
}

func (rr runeRange) Len() int {
	return int(rr.To - rr.From + 1)
}

func (rr runeRange) Runes() []rune {
	runes := make([]rune, 0, rr.Len())
	for r := rr.From; r <= rr.To; r++ {
		runes = append(runes, r)
	}

	return runes
}

type runeRanges []Range

var _ Range = runeRanges{}

func (rrs runeRanges) Contains(r rune) bool {
	for _, rr := range rrs {
		if rr.Contains(r) {
			return true
		}
	}

	return false
}

func (rrs runeRanges) Len() int {
	total := 0
	for _, rr := range rrs {
		total += rr.Len()
	}

	return total
}

func (rrs runeRanges) Runes() []rune {
	runes := make([]rune, 0, rrs.Len())
	for _, rr := range rrs {
		runes = append(runes, rr.Runes()...)
	}

	return runes
}

func FromRune(r rune) Range {
	return runeRange{
		From: r,
		To:   r,
	}
}

func FromRunes(runes ...rune) Range {
	ranges := make([]Range, 0, len(runes))
	for _, r := range runes {
		ranges = append(ranges, FromRune(r))
	}

	return runeRanges(ranges)
}

func FromRuneRange(from, to rune) Range {
	if from > to {
		from, to = to, from
	}

	return runeRange{
		From: from,
		To:   to,
	}
}

func CombineRuneRanges(ranges ...Range) Range {
	return runeRanges(ranges)
}
