package mcp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/approve"
)

// TestApprovalSchemaFieldOrder pins the wire order of the approval form so the
// rule field renders above the remember toggle. A property map would marshal its
// keys alphabetically and float "remember" first, so the typed schema's
// PropertyOrder is what keeps this true.
func TestApprovalSchemaFieldOrder(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(approvalSchema(approve.Suggestion{Kind: approve.KindPrefix, Tokens: []string{"go", "test"}}, false, "/tmp/.cg.yaml"))
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

type approvalMessageTest struct {
	Name  string
	Argvs [][]string
	Cwd   string
	Pos   int
	Total int
	Want  string
}

var approvalMessageTests = []approvalMessageTest{
	{
		Name:  "one command with no position",
		Argvs: [][]string{{"echo", "a b"}},
		Cwd:   "/work",
		Pos:   1,
		Total: 1,
		Want:  "Allow this command in /work?\necho 'a b'",
	},
	{
		Name:  "one command with a position",
		Argvs: [][]string{{"echo", "a"}},
		Cwd:   "/work",
		Pos:   1,
		Total: 2,
		Want:  "Approval 1 of 2: allow this command in /work?\necho a",
	},
	{
		Name:  "several commands with a position",
		Argvs: [][]string{{"go", "test", "./a"}, {"go", "test", "./b"}},
		Cwd:   "/work",
		Pos:   2,
		Total: 3,
		Want:  "Approval 2 of 3: allow 2 commands in /work?\ngo test ./a\ngo test ./b",
	},
	{
		Name:  "no working directory names the server's",
		Argvs: [][]string{{"echo"}},
		Pos:   1,
		Total: 1,
		Want:  "Allow this command in (server cwd)?\necho",
	},
}

type approvalControlTest struct {
	Name  string
	Argvs [][]string
	Cwd   string

	WantContains []string
}

var approvalControlTests = []approvalControlTest{
	{
		Name:         "a newline in an argument",
		Argvs:        [][]string{{"sh", "-c", "true\nrm -rf x"}, {"echo", "b"}},
		Cwd:          "/work",
		WantContains: []string{`sh -c $'true\nrm -rf x'`},
	},
	{
		Name:         "an escape in an argument",
		Argvs:        [][]string{{"printf", "\x1b[2J\x7f\u0085"}, {"echo", "b"}},
		Cwd:          "/work",
		WantContains: []string{`printf $'\x1b[2J\x7f\u0085'`},
	},
	{
		Name:         "a bidi override in an argument",
		Argvs:        [][]string{{"echo", "a\u202eb"}, {"echo", "b"}},
		Cwd:          "/work",
		WantContains: []string{`echo $'a\u202eb'`},
	},
	{
		Name:         "a zero-width space in an argument",
		Argvs:        [][]string{{"echo", "a\u200bb"}, {"echo", "b"}},
		Cwd:          "/work",
		WantContains: []string{`echo $'a\u200bb'`},
	},
	{
		Name:         "a tag character above the basic plane",
		Argvs:        [][]string{{"echo", "a\U000e0001b"}, {"echo", "b"}},
		Cwd:          "/work",
		WantContains: []string{`echo $'a\U000e0001b'`},
	},
	{
		Name:         "a newline in the working directory",
		Argvs:        [][]string{{"echo", "a"}, {"echo", "b"}},
		Cwd:          "/work\nrm -rf x",
		WantContains: []string{`in $'/work\nrm -rf x'?`},
	},
	{
		Name:         "quotes and backslashes around a control character",
		Argvs:        [][]string{{"echo", "it's\\\t"}, {"echo", "b"}},
		Cwd:          "/work",
		WantContains: []string{`echo $'it\'s\\\t'`},
	},
}

func TestApprovalMessageKeepsControlCharactersOnOneLine(t *testing.T) {
	t.Parallel()

	for _, test := range approvalControlTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()

			msg := approvalMessage(test.Argvs, test.Cwd, 1, 2)
			if got := countLines(msg); got != visibleMessageLines {
				t.Errorf("form has %d lines, want %d:\n%q", got, visibleMessageLines, msg)
			}
			if strings.ContainsAny(msg, "\x1b\x7f\r\t\u0085\u202e\u200b\U000e0001") {
				t.Errorf("form carries a raw control character: %q", msg)
			}
			for _, want := range test.WantContains {
				if !strings.Contains(msg, want) {
					t.Errorf("form missing %q:\n%q", want, msg)
				}
			}
		})
	}
}

// TestApprovalMessageFitsVisibleLines pins the layout to the client's visible-line cap. A fuller
// form would hide its last commands from the person approving them.
func TestApprovalMessageFitsVisibleLines(t *testing.T) {
	t.Parallel()

	argvs := make([][]string, maxFormCommands)
	for i := range argvs {
		argvs[i] = []string{"echo", "a"}
	}

	msg := approvalMessage(argvs, "/work", 1, 2)
	if got := countLines(msg); got != visibleMessageLines {
		t.Errorf("a full form has %d lines, want %d:\n%s", got, visibleMessageLines, msg)
	}
	if strings.Contains(msg, "\n\n") {
		t.Errorf("a form has a blank line, which spends a visible line:\n%s", msg)
	}
}

func TestApprovalMessage(t *testing.T) {
	t.Parallel()

	for _, test := range approvalMessageTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()

			if got := approvalMessage(test.Argvs, test.Cwd, test.Pos, test.Total); got != test.Want {
				t.Errorf("approvalMessage = %q, want %q", got, test.Want)
			}
		})
	}
}

func TestApprovalSchemaRememberTitle(t *testing.T) {
	t.Parallel()

	schema := approvalSchema(approve.Suggestion{Kind: approve.KindPrefix, Tokens: []string{"go", "test"}}, false, "/tmp/.cg.yaml")
	if got := schema.Properties["remember"].Title; got != "Remember this rule" {
		t.Errorf("remember title = %q, want %q", got, "Remember this rule")
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
