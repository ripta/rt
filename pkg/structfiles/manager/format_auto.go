package manager

import (
	"bytes"
	"fmt"
	"io"
)

var AutoFormat string = ""

type autoDecoder struct {
	decs []DecoderFactory
	dec  Decoder
	buf  *bytes.Buffer
}

func AutoDecoder(r io.Reader) Decoder {
	buf := &bytes.Buffer{}
	if _, err := buf.ReadFrom(r); err != nil {
		return nil
	}

	return &autoDecoder{
		decs: []DecoderFactory{
			JSONDecoder,
			YAMLDecoder,
		},
		buf: buf,
	}
}

func (d *autoDecoder) Decode(v any) error {
	// If a previous decode was successful, reuse the decoder
	if d.dec != nil {
		return d.dec.Decode(v)
	}

	// Try each decoder in order, while there are still possible decoders
	for len(d.decs) > 0 {
		// Initialize a new decoder using a fresh reader to avoid partial reads
		// then attempt to decode a value. If the decode succeeds, store the
		// decoder for future use. Bubble up EOF errors.
		dec := d.decs[0](bytes.NewReader(d.buf.Bytes()))
		if err := dec.Decode(v); err == nil {
			d.dec = dec
			return nil
		} else if err == io.EOF {
			return io.EOF
		}

		d.decs = d.decs[1:]
	}

	// If no decoders were successful, return a final error
	return fmt.Errorf("%w: no known decoder", ErrUnknownFormat)
}
