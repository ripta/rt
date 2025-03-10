package csvmap

import "errors"

var ErrInvalidRecord = errors.New("invalid record")

type Document struct {
	Header []string
	Rows   [][]any
}

func (d *Document) Len() int {
	return len(d.Rows)
}

func (d *Document) Record(i int) map[string]any {
	rec := map[string]any{}
	for j, h := range d.Header {
		rec[h] = d.Rows[i][j]
	}

	return rec
}

func (d *Document) Append(rec map[string]any) error {
	if len(d.Header) != len(rec) {
		return ErrInvalidRecord
	}

	row := make([]any, len(d.Header))
	for j, h := range d.Header {
		if v, ok := rec[h]; ok {
			row[j] = v
		}
	}

	d.Rows = append(d.Rows, row)
	return nil
}
