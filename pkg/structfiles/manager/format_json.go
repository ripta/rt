package manager

import (
	"encoding/json"
	"io"
	"strings"
)

func init() {
	enc := &JSONEncoder{}
	RegisterFormatWithOptions("json", []string{".json"}, enc.EncodeTo, enc, JSONDecoder, nil)
}

func JSONDecoder(r io.Reader) Decoder {
	j := json.NewDecoder(r)
	return j
}

type JSONEncoder struct {
	Indent   int  `json:"indent,string"`
	NoIndent bool `json:"no_indent,string"`
}

func (e *JSONEncoder) EncodeTo(w io.Writer) (Encoder, Closer) {
	indent := 2
	if e.Indent > 0 {
		indent = e.Indent
	}

	j := json.NewEncoder(w)
	if !e.NoIndent {
		j.SetIndent("", strings.Repeat(" ", indent))
	}
	return j, func() error { return nil }
}
