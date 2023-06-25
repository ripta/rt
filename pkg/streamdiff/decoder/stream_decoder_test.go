package decoder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type streamDecoderTest struct {
	label        string
	jsonInput    string
	splitter     SplitterFunc
	expectedErr  error
	expectedObjs []any
}

var streamDecoderTests = []streamDecoderTest{
	{
		label:        "no_doc",
		jsonInput:    ``,
		splitter:     nil,
		expectedErr:  nil,
		expectedObjs: []any{},
	},
	{
		label:       "single_doc",
		jsonInput:   `{"word":"hello"}`,
		splitter:    nil,
		expectedErr: nil,
		expectedObjs: []any{
			map[string]any{"word": "hello"},
		},
	},
	{
		label:       "multi_doc",
		jsonInput:   `{"words":[{"word":"hello"},{"word":"goodbye"}]}`,
		splitter:    GenerateSplitter("words"),
		expectedErr: nil,
		expectedObjs: []any{
			map[string]any{"word": "hello"},
			map[string]any{"word": "goodbye"},
		},
	},
}

func TestStreamDecoder(t *testing.T) {
	for i := range streamDecoderTests {
		test := streamDecoderTests[i]
		t.Run(test.label, func(t *testing.T) {
			dec := NewStream(strings.NewReader(test.jsonInput), test.splitter)
			objs := []any{}
			for dec.More() {
				var obj any
				if err := dec.Decode(&obj); err != nil {
					assert.Equal(t, test.expectedErr, err)
					return
				}

				objs = append(objs, obj)
			}

			assert.Equal(t, test.expectedObjs, objs)
		})
	}
}
