package manager

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogfmt(t *testing.T) {
	logfmtString := "name=john age=30\nname=jane age=25 error=\"nothing to see here\"\n"
	logfmtStructs := []map[string]interface{}{
		{"name": "john", "age": "30"},
		{"name": "jane", "age": "25", "error": "nothing to see here"},
	}

	opts := &LogfmtOptions{}
	dec := opts.LogfmtDecoder(strings.NewReader(logfmtString))
	for {
		var m any
		if err := dec.Decode(&m); err != nil {
			if err != io.EOF {
				assert.NoError(t, err)
			}
			break
		}

		if !assert.NotEmpty(t, logfmtStructs) {
			break
		}

		assert.Equal(t, logfmtStructs[0], m)
		logfmtStructs = logfmtStructs[1:]
	}
}
