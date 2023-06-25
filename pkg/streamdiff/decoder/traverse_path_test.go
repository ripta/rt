package decoder

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

type traversePathTest struct {
	label        string
	input        any
	paths        []any
	expectedFail bool
	expectedObj  any
}

var traversePathTests = []traversePathTest{
	{
		label:        "empty_input_must_not_panic",
		input:        nil,
		paths:        []any{"word"},
		expectedFail: true,
	},
	{
		label: "empty_path",
		input: map[string]string{
			"word": "hello",
		},
		paths: nil,
		expectedObj: map[string]string{
			"word": "hello",
		},
	},
	{
		label: "simple_map_found",
		input: map[string]string{
			"word": "hello",
		},
		paths:       []any{"word"},
		expectedObj: "hello",
	},
	{
		label: "simple_map_notfound",
		input: map[string]string{
			"word": "hello",
		},
		paths:        []any{"foobar"},
		expectedFail: true,
	},
	{
		label: "any_map_found",
		input: map[string]any{
			"word": "hello",
		},
		paths:       []any{"word"},
		expectedObj: "hello",
	},
	{
		label: "nested_map_found_map",
		input: map[string]any{
			"word": map[string]any{
				"hello": "world",
			},
		},
		paths: []any{"word"},
		expectedObj: map[string]any{
			"hello": "world",
		},
	},
	{
		label: "nested_map_found_value",
		input: map[string]any{
			"word": map[string]any{
				"hello": "world",
			},
		},
		paths:       []any{"word", "hello"},
		expectedObj: "world",
	},
	{
		label: "map_found_slice",
		input: map[string]any{
			"words": []string{"hello", "world", "fred"},
		},
		paths:       []any{"words"},
		expectedObj: []string{"hello", "world", "fred"},
	},
}

func TestTraversePath(t *testing.T) {
	for i := range traversePathTests {
		test := traversePathTests[i]
		t.Run(test.label, func(t *testing.T) {
			actual, ok := traversePath(reflect.ValueOf(test.input), test.paths)
			assert.Equal(t, test.expectedFail, !ok)
			if ok {
				assert.Equal(t, test.expectedObj, actual.Interface())
			}
		})
	}
}
