package manager

import (
	"io"
	"olympos.io/encoding/edn"
	"strings"
)

func init() {
	enc := &EDNEncoder{}
	RegisterFormatWithOptions("edn", []string{".edn"}, enc.EncodeTo, enc, nil, nil)
}

type EDNEncoder struct {
	Prefix string `json:"prefix"`
	Indent int    `json:"indent,string"`
}

func (e *EDNEncoder) EncodeTo(w io.Writer) (Encoder, Closer) {
	if e.Indent == 0 && e.Prefix == "" {
		return edn.NewEncoder(w), noCloser
	}

	enc := edn.NewEncoder(w)
	return EncoderFunc(func(v any) error {
		return enc.EncodeIndent(v, e.Prefix, strings.Repeat(" ", e.Indent))
	}), noCloser
}
