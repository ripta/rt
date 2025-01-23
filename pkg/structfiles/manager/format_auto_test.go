package manager

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

type autoDecoderTest struct {
	Label string
	Input string
	Docs  []*Document
}

func anyizer(v map[string]any) *Document {
	d := Document(v)
	return &d
}

var autoDecoderTests = []autoDecoderTest{
	{
		Label: "empty",
	},
	{
		Label: "single json document",
		Input: `{"a": 1}`,
		Docs: []*Document{
			anyizer(map[string]any{"a": 1.0}),
		},
	},
	{
		Label: "single json document with trailing newline",
		Input: `{"a": 1}` + "\n",
		Docs: []*Document{
			anyizer(map[string]any{"a": 1.0}),
		},
	},
	{
		Label: "one json document per line",
		Input: `{"a": 1}` + "\n" + `{"b": 2}` + "\n" + `{"c": "foo"}` + "\n",
		Docs: []*Document{
			anyizer(map[string]any{"a": 1.0}),
			anyizer(map[string]any{"b": 2.0}),
			anyizer(map[string]any{"c": "foo"}),
		},
	},
	{
		Label: "one complex json document per line",
		Input: `{"a": 1, "b": [2, 3, 4]}` + "\n" + `{"c": "foo", "d": "bar", "e": {"hello": ["world", "universe"]}}` + "\n",
		Docs: []*Document{
			anyizer(map[string]any{
				"a": 1.0,
				"b": []any{2.0, 3.0, 4.0},
			}),
			anyizer(map[string]any{
				"c": "foo",
				"d": "bar",
				"e": map[string]any{
					"hello": []any{"world", "universe"},
				},
			}),
		},
	},
	{
		Label: "one json document with null value",
		Input: `{"a": 1, "b": null, "c": "foo"}` + "\n",
		Docs: []*Document{
			anyizer(map[string]any{
				"a": 1.0,
				"b": nil,
				"c": "foo",
			}),
		},
	},
	//{
	//	Label: "one json document per line with empty document",
	//	Input: `{"a": 1}` + "\n" + `null` + "\n" + `{"c": "foo"}` + "\n",
	//	Docs: []*Document{
	//		anyizer(map[string]any{"a": 1.0}),
	//		nil, // ugh, how to represent an empty document?
	//		anyizer(map[string]any{"c": "foo"}),
	//	},
	//},
	{
		Label: "single yaml document",
		Input: "a: 1\n",
		Docs: []*Document{
			anyizer(map[string]any{"a": 1}),
		},
	},
	{
		Label: "multiple yaml documents",
		Input: "a: 1\n---\nb: 2\n",
		Docs: []*Document{
			anyizer(map[string]any{"a": 1}),
			anyizer(map[string]any{"b": 2}),
		},
	},
	//{
	//	Label: "multiple yaml documents with an empty document",
	//	Input: "a: 1\n---\n---\nb: 2\n",
	//	Docs: []*Document{
	//		anyizer(map[string]any{"a": 1}),
	//		(*Document)(nil), // ugh, how to represent an empty document?
	//		anyizer(map[string]any{"b": 2}),
	//	},
	//},
}

func TestAutoDecoder(t *testing.T) {
	for _, test := range autoDecoderTests {
		t.Run(test.Label, func(t *testing.T) {
			dec := AutoDecoder(bytes.NewBufferString(test.Input))

			docs := ([]*Document)(nil)
			for {
				var doc Document
				if err := dec.Decode(&doc); err != nil {
					if errors.Is(err, io.EOF) {
						break
					}

					t.Fatalf("unexpected error: %v", err)
				}

				docs = append(docs, &doc)
			}

			//raw, _ := json.MarshalIndent(docs, "", "  ")
			//fmt.Printf("docs %s:\n%s\n", test.Label, string(raw))
			assert.Equal(t, test.Docs, docs)
		})
	}
}
