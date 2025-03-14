package csvmap

import (
	"encoding/csv"
	"fmt"
	"io"
)

func Decode(r io.Reader) (*Document, error) {
	return CustomDecode(r, nil)
}

func CustomDecode(r io.Reader, hook func(*csv.Reader)) (*Document, error) {
	cr := csv.NewReader(r)
	if hook != nil {
		hook(cr)
	}

	hs, err := cr.Read()
	if err != nil {
		return nil, err
	}

	d := Document{
		Header: hs,
	}
	for {
		row, err := cr.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		if len(row) != len(hs) {
			return nil, fmt.Errorf("%w: expected %d columns, got %d", ErrInvalidRecord, len(hs), len(row))
		}

		rec := []any{}
		for i := range hs {
			rec = append(rec, row[i])
		}

		d.Rows = append(d.Rows, rec)
	}

	return &d, nil
}

func Encode(w io.Writer, d *Document) error {
	return CustomEncode(w, nil, d)
}

func CustomEncode(w io.Writer, hook func(*csv.Writer), d *Document) error {
	cw := csv.NewWriter(w)
	if hook != nil {
		hook(cw)
	}

	if err := cw.Write(d.Header); err != nil {
		return err
	}

	for _, rec := range d.Rows {
		if len(rec) != len(d.Header) {
			return fmt.Errorf("%w: expected %d columns, got %d", ErrInvalidRecord, len(d.Header), len(rec))
		}

		row := []string{}
		for i := range d.Header {
			row = append(row, rec[i].(string))
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}

	cw.Flush()
	return cw.Error()
}
