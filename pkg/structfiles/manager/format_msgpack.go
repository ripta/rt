package manager

import (
	"io"

	"github.com/vmihailenco/msgpack/v5"
)

func init() {
	RegisterFormat("msgpack", []string{".mpk", ".msgpack"}, MsgPackEncoder, MsgPackDecoder)
}

func MsgPackDecoder(r io.Reader) Decoder {
	return msgpack.NewDecoder(r)
}

func MsgPackEncoder(w io.Writer) (Encoder, Closer) {
	return msgpack.NewEncoder(w), noCloser
}
