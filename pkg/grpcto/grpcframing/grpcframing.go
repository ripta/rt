package grpcframing

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

type Payload struct {
	encoding uint8
	length   uint32
	message  []byte
}

func (p Payload) Compressed() bool {
	return p.encoding == 1
}

func (p Payload) Len() int {
	return int(p.length)
}

func (p Payload) Message() []byte {
	return p.message
}

func (p Payload) WriteTo(w io.Writer) (int64, error) {
	w.Write([]byte{p.encoding})
	w.Write(binary.BigEndian.AppendUint32(nil, p.length))

	n, err := w.Write(p.message)
	return int64(n) + 5, err
}

func Decode(bs []byte, maxBytes int) (Payload, error) {
	return DecodeReader(bytes.NewReader(bs), maxBytes)
}

func DecodeReader(r io.Reader, maxBytes int) (Payload, error) {
	var header [5]byte
	if _, err := r.Read(header[:]); err != nil {
		return Payload{}, err
	}

	p := Payload{
		encoding: header[0],
		length:   binary.BigEndian.Uint32(header[1:]),
	}

	if l := int(p.length); l > maxBytes {
		return Payload{}, fmt.Errorf("payload size %d bytes is larger than the allowed %d bytes", l, maxBytes)
	}

	p.message = make([]byte, int(p.length))
	if _, err := r.Read(p.message); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return Payload{}, err
	}

	return p, nil
}

var (
	maxPossibleLen = math.MaxUint32

	ErrMessageTooLarge = errors.New("message size is too large")
)

func New(message []byte) (Payload, error) {
	if len(message) > maxPossibleLen {
		return Payload{}, fmt.Errorf("message length %d bytes is larger than allowed %d bytes: %w", len(message), maxPossibleLen, ErrMessageTooLarge)
	}

	return Payload{
		encoding: 0,
		length:   uint32(len(message)),
		message:  message,
	}, nil
}

func Encode(p Payload) ([]byte, error) {
	w := &bytes.Buffer{}
	if _, err := p.WriteTo(w); err != nil {
		return nil, err
	}

	return w.Bytes(), nil
}
