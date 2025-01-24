package manager

import (
	"encoding/json"
	"io"
)

func init() {
	RegisterFormat("json", []string{".json"}, JSONEncoder, JSONDecoder)
}

func JSONDecoder(r io.Reader) Decoder {
	j := json.NewDecoder(r)
	return j
}

func JSONEncoder(w io.Writer) (Encoder, Closer) {
	j := json.NewEncoder(w)
	j.SetIndent("", "  ")
	return j, func() error { return nil }
}
