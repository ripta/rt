// Emoji Encoder implementation from https://github.com/paulgb/emoji-encoder/blob/main/app/encoding.ts
package varsel

import (
	"bytes"
	"io"
)

var (
	inverseVariationSelectors map[rune]int
	variationSelectors        []rune
)

func init() {
	// Variation selectors 1–16 https://unicode.org/charts/nameslist/n_FE00.html
	for i := 0xFE00; i <= 0xFE0F; i++ {
		variationSelectors = append(variationSelectors, rune(i))
	}
	// Variation selectors supplement 17–256 https://unicode.org/charts/nameslist/n_FE00.html
	for i := 0xE0100; i <= 0xE01EF; i++ {
		variationSelectors = append(variationSelectors, rune(i))
	}

	if len(variationSelectors) != 256 {
		panic("variation selectors length is not 256")
	}

	inverseVariationSelectors = map[rune]int{}
	for i, r := range variationSelectors {
		inverseVariationSelectors[r] = i
	}
}

// Decode decodes the input stream and discards the original runes.
func Decode(dst io.Writer, src io.Reader) error {
	return decode(dst, src, false)
}

// DecodeInterleaved decodes the input stream and interleaves the original runes.
func DecodeInterleaved(dst io.Writer, src io.Reader) error {
	return decode(dst, src, true)
}

func decode(dst io.Writer, src io.Reader, withOriginal bool) error {
	raw, err := io.ReadAll(src)
	if err != nil {
		return err
	}

	out := &bytes.Buffer{}
	for _, r := range bytes.Runes(raw) {
		if i, ok := inverseVariationSelectors[r]; ok {
			out.WriteByte(byte(i))
		} else if withOriginal {
			out.WriteRune(r)
		}
	}

	if _, err := out.WriteTo(dst); err != nil {
		return err
	}
	return nil
}

// DecodeRune decodes a single rune. If the rune is a variation selector, it returns its value,
// which will be between 0 and 255 inclusive. If the rune is not a variation selector, it returns -1.
func DecodeRune(r rune) int {
	i, ok := inverseVariationSelectors[r]
	if !ok {
		return -1
	}
	return i
}

func Encode(dst io.Writer, src io.Reader) error {
	raw, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	if _, err := dst.Write([]byte(string(EncodeBytes(raw)))); err != nil {
		return err
	}
	return nil
}

// EncodeByte encodes a single byte into a variation selector rune.
func EncodeByte(b byte) rune {
	return variationSelectors[b]
}

// EncodeBytes encodes a slice of bytes into a slice of variation selector runes.
func EncodeBytes(b []byte) []rune {
	rs := []rune{}
	for _, b := range b {
		rs = append(rs, variationSelectors[b])
	}
	return rs
}

// EncodeRune encodes a single rune (or rather its bytes) into a slice of variation selector runes.
func EncodeRune(r rune) []rune {
	return EncodeBytes([]byte(string(r)))
}

// EncodeString encodes a string into a slice of variation selector runes.
func EncodeString(s string) []rune {
	return EncodeBytes([]byte(s))
}
