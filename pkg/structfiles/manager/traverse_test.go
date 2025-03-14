package manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type traversePathTest struct {
	Path     string
	Struct   any
	Expected any
	Err      error
}

type fakeStruct struct {
	First string
	Age   int
}

var traversePathCommonStruct = map[string]any{
	"foo": "bar",
	"baz": "quux",
	"qux": map[string]string{
		"10": "abc",
		"20": "def",
	},
	"corge":  []any{"hoge", "fuga"},
	"grault": []int{1, 2, 3},
	"garply": fakeStruct{
		First: "waldo",
		Age:   42,
	},
}

var traversePathTests = []traversePathTest{
	{
		Struct:   traversePathCommonStruct,
		Expected: traversePathCommonStruct,
	},
	{
		Path:     "foo",
		Struct:   traversePathCommonStruct,
		Expected: "bar",
	},
	{
		Path:     "qux.20",
		Struct:   traversePathCommonStruct,
		Expected: "def",
	},
	{
		Path:     "corge.0",
		Struct:   traversePathCommonStruct,
		Expected: "hoge",
	},
	{
		Path:   "corge.2",
		Struct: traversePathCommonStruct,
		Err:    ErrKeyNotFound,
	},
	{
		Path:     "corge.-1",
		Struct:   traversePathCommonStruct,
		Expected: "fuga",
	},
	{
		Path:     "garply.First",
		Struct:   traversePathCommonStruct,
		Expected: "waldo",
	},
	{
		Path:     "garply.Age",
		Struct:   traversePathCommonStruct,
		Expected: 42,
	},
	{
		Path:   "garply.MissingField",
		Struct: traversePathCommonStruct,
		Err:    ErrKeyNotFound,
	},
	{
		Path:     "grault.1",
		Struct:   traversePathCommonStruct,
		Expected: 2,
	},
}

func TestTraversePath(t *testing.T) {
	for _, test := range traversePathTests {
		t.Run(test.Path, func(t *testing.T) {
			actual, err := TraversePath(test.Struct, test.Path)
			if test.Err != nil {
				assert.ErrorIs(t, err, test.Err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, test.Expected, actual)
		})
	}
}
