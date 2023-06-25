package decoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type kubernetesListSplitterTest struct {
	label         string
	input         any
	expectedFail  bool
	expectedSplit []any
}

var kubernetesListSplitterTests = []kubernetesListSplitterTest{
	{
		label:         "empty_must_not_panic",
		input:         nil,
		expectedFail:  true,
		expectedSplit: nil,
	},
	{
		label:         "nothing_to_split",
		input:         map[string]any{},
		expectedFail:  true,
		expectedSplit: nil,
	},
	{
		label: "non_list_resource",
		input: map[string]any{
			"apiVersion": "v1",
			"kind":       "Node",
			"metadata": map[string]any{
				"name": "foo-bar",
			},
		},
		expectedFail:  true,
		expectedSplit: nil,
	},
	{
		label: "list_resource",
		input: map[string]any{
			"apiVersion": "v1",
			"kind":       "NodeList",
			"items": []any{
				map[string]any{
					"apiVersion": "v1",
					"kind":       "Node",
					"metadata": map[string]any{
						"name": "foo",
					},
				},
				map[string]any{
					"apiVersion": "v1",
					"kind":       "Node",
					"metadata": map[string]any{
						"name": "bar",
					},
				},
			},
		},
		expectedFail: false,
		expectedSplit: []any{
			map[string]any{
				"apiVersion": "v1",
				"kind":       "Node",
				"metadata": map[string]any{
					"name": "foo",
				},
			},
			map[string]any{
				"apiVersion": "v1",
				"kind":       "Node",
				"metadata": map[string]any{
					"name": "bar",
				},
			},
		},
	},
}

func TestKubernetesListSplitter(t *testing.T) {
	for i := range kubernetesListSplitterTests {
		test := kubernetesListSplitterTests[i]
		t.Run(test.label, func(t *testing.T) {
			actual, ok := KubernetesListSplitter(test.input)
			assert.Equal(t, test.expectedFail, !ok)
			if ok {
				assert.Equal(t, test.expectedSplit, actual)
			}
		})
	}
}
