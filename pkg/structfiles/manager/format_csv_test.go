package manager

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCSV(t *testing.T) {
	csvString := "name,age\njohn,30\njane,25\n"
	csvStructs := []map[string]interface{}{
		{"name": "john", "age": "30"},
		{"name": "jane", "age": "25"},
	}

	opts := &CSVOptions{}
	dec := opts.CSVDecoder(strings.NewReader(csvString))
	for {
		var m any
		if err := dec.Decode(&m); err != nil {
			if err != io.EOF {
				assert.NoError(t, err)
			}
			break
		}

		if !assert.NotEmpty(t, csvStructs) {
			break
		}

		assert.Equal(t, csvStructs[0], m)
		csvStructs = csvStructs[1:]
	}
}
