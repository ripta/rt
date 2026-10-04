package mcp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestApprovalSchemaFieldOrder pins the wire order of the approval form so the
// rule field renders above the remember toggle. A property map would marshal its
// keys alphabetically and float "remember" first, so the typed schema's
// PropertyOrder is what keeps this true.
func TestApprovalSchemaFieldOrder(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(approvalSchema([]string{"go", "test"}, "/tmp/.cg.yaml"))
	if err != nil {
		t.Fatalf("marshalling schema: %v", err)
	}
	wire := string(raw)

	idxRule := strings.Index(wire, `"rule"`)
	idxRemember := strings.Index(wire, `"remember"`)
	if idxRule < 0 || idxRemember < 0 {
		t.Fatalf("schema missing a field: %s", wire)
	}
	if idxRule > idxRemember {
		t.Errorf("rule must serialize before remember:\n%s", wire)
	}
}

// renderDivergenceTest drives the tiered divergence rendering: a small change
// stays a unified diff, a taller one collapses to changed lines, and an even
// larger one collapses to a count summary.
type renderDivergenceTest struct {
	name     string
	snapshot string
	current  string
	maxLines int
	contains []string
	absent   []string
}

var renderDivergenceTests = []renderDivergenceTest{
	{
		name:     "small change keeps unified diff",
		snapshot: "a\nb\nc\n",
		current:  "a\nb\nc\nd\n",
		maxLines: maxDiffLines,
		contains: []string{"--- loaded", "+++ on disk", "+d"},
	},
	{
		name:     "tall change collapses to changed lines",
		snapshot: "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n",
		current:  "a\nX1\nb\nc\nd\ne\nY1\nf\ng\nh\ni\nZ1\nj\nk\nl\n",
		maxLines: maxDiffLines,
		contains: []string{"+X1", "+Y1", "+Z1"},
		absent:   []string{"@@", "loaded", "on disk"},
	},
	{
		name:     "huge change collapses to summary",
		snapshot: "a\n",
		current:  "a\nn01\nn02\nn03\nn04\nn05\nn06\nn07\nn08\nn09\nn10\nn11\nn12\n",
		maxLines: 1,
		contains: []string{"12 line(s) added", "0 line(s) removed", "too large"},
		absent:   []string{"@@", "+n01"},
	},
}

func TestRenderDivergence(t *testing.T) {
	t.Parallel()

	for _, test := range renderDivergenceTests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := renderDivergence([]byte(test.snapshot), []byte(test.current))
			if n := countLines(got); n > test.maxLines {
				t.Errorf("body has %d lines, want <= %d:\n%s", n, test.maxLines, got)
			}
			for _, want := range test.contains {
				if !strings.Contains(got, want) {
					t.Errorf("body missing %q:\n%s", want, got)
				}
			}
			for _, bad := range test.absent {
				if strings.Contains(got, bad) {
					t.Errorf("body should not contain %q:\n%s", bad, got)
				}
			}
		})
	}
}

func accept(content map[string]any) *mcpsdk.ElicitResult {
	return &mcpsdk.ElicitResult{Action: "accept", Content: content}
}

func projectFile(root string) string {
	return filepath.Join(root, ".cg.yaml")
}
