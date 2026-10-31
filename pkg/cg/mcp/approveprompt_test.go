package mcp

import (
	"encoding/json"
	"path/filepath"
	"slices"
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

type flowSeqTest struct {
	Name   string
	Tokens []string
}

var flowSeqTests = []flowSeqTest{
	{Name: "plain words", Tokens: []string{"go", "test", "./..."}},
	{Name: "a trailing empty token", Tokens: []string{"echo", ""}},
	{Name: "an empty token inside a list", Tokens: []string{"git", "commit", "-m", ""}},
	{Name: "a sed expression", Tokens: []string{"sed", "-E", "s/a/b/", "f.txt"}},
	{Name: "a json object", Tokens: []string{"curl", "-d", `{"a":1}`}},
	{Name: "a glob star", Tokens: []string{"ls", "*"}},
	{Name: "words yaml reads as other types", Tokens: []string{"yes", "no", "null", "true", "~", "1e3", "0o17", "0x1f", "12", "1.5", ".inf"}},
	{Name: "a comment lookalike", Tokens: []string{"echo", "#x", "a #b"}},
	{Name: "yaml indicators", Tokens: []string{"- a", "? b", "[c]", "{d}", "e, f", "g: h", "&i", "*j", "!k", "|l", ">m", "%n", "@o", "`p"}},
	{Name: "quotes and backslashes", Tokens: []string{`a"b`, `c'd`, `e\f`, `\`, `"`, `'`}},
	{Name: "spaces at the edges", Tokens: []string{" a", "b ", " ", "c  d"}},
	{Name: "newline and tab", Tokens: []string{"a\nb", "c\td", "\n", "e\r\nf"}},
	{Name: "control characters", Tokens: []string{"\x00", "\x01a", "\x1b[31m", "\x7f", "\u0085"}},
	{Name: "bidi override", Tokens: []string{"echo", "a\u202eb", "\u2066x\u2069"}},
	{Name: "format characters", Tokens: []string{"a\u200bb", "\u200d", "\ufeffx", "\u00adx"}},
	{Name: "line and paragraph separators", Tokens: []string{"a\u2028b", "c\u2029d"}},
	{Name: "characters beyond the basic plane", Tokens: []string{"\U000e0041", "\U0001f600", "\U0001d173"}},
	{Name: "non-ascii text", Tokens: []string{"héllo", "日本語", "👍"}},
	{Name: "a long token", Tokens: []string{"echo", strings.Repeat("word ", 60), strings.Repeat("x", 300)}},
}

func TestRenderFlowSeqRoundTrips(t *testing.T) {
	t.Parallel()

	for _, tt := range flowSeqTests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			rendered := renderFlowSeq(tt.Tokens)

			got, err := parseRuleField(map[string]any{"rule": rendered}, nil)
			if err != nil {
				t.Fatalf("parseRuleField(%q): %v", rendered, err)
			}
			if !slices.Equal(got, tt.Tokens) {
				t.Errorf("round trip of %q = %q, want %q", rendered, got, tt.Tokens)
			}
		})
	}
}

type flowSeqEscapeTest struct {
	Name   string
	Tokens []string
	Want   string
}

var flowSeqEscapeTests = []flowSeqEscapeTest{
	{Name: "bidi override", Tokens: []string{"a\u202eb"}, Want: `["a\u202eb"]`},
	{Name: "zero-width space", Tokens: []string{"a\u200bb"}, Want: `["a\u200bb"]`},
	{Name: "line separator", Tokens: []string{"a\u2028b"}, Want: `["a\u2028b"]`},
	{Name: "newline", Tokens: []string{"a\nb"}, Want: `["a\nb"]`},
	{Name: "escape character", Tokens: []string{"\x1b[0m"}, Want: `["\x1b[0m"]`},
	{Name: "format character beyond the basic plane", Tokens: []string{"\U000e0041"}, Want: `["\U000e0041"]`},
	{Name: "only the offending token is escaped", Tokens: []string{"echo", "a\u202eb"}, Want: `[echo, "a\u202eb"]`},
	{Name: "an empty token", Tokens: []string{"git", ""}, Want: `[git, ""]`},
}

func TestRuleDefaultShowsControlCharactersEscaped(t *testing.T) {
	t.Parallel()

	for _, tt := range flowSeqEscapeTests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			if got := renderFlowSeq(tt.Tokens); got != tt.Want {
				t.Errorf("renderFlowSeq(%q) = %s, want %s", tt.Tokens, got, tt.Want)
			}

			raw, err := json.Marshal(approvalSchema(approve.Suggestion{Kind: approve.KindExact, Tokens: tt.Tokens}, false, "/tmp/.cg.yaml"))
			if err != nil {
				t.Fatalf("marshalling schema: %v", err)
			}

			var schema struct {
				Properties struct {
					Rule struct {
						Default string `json:"default"`
					} `json:"rule"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatalf("decoding schema: %v", err)
			}

			def := schema.Properties.Rule.Default
			if def != tt.Want {
				t.Errorf("rule default = %s, want %s", def, tt.Want)
			}
			if strings.ContainsFunc(def, needsEscape) {
				t.Errorf("rule default %q holds a raw control or format character", def)
			}
		})
	}
}

func TestParseRuleFieldKeepsEmptyTokens(t *testing.T) {
	t.Parallel()

	got, err := parseRuleField(map[string]any{"rule": `[git, commit, -m, ""]`}, nil)
	if err != nil {
		t.Fatalf("parseRuleField: %v", err)
	}
	if want := []string{"git", "commit", "-m", ""}; !slices.Equal(got, want) {
		t.Errorf("tokens = %q, want %q", got, want)
	}

	if _, err := parseRuleField(map[string]any{"rule": "[]"}, nil); err == nil {
		t.Errorf("an empty list should fail")
	}
}

func TestParseRuleFieldRejectsAnEmptyProgram(t *testing.T) {
	t.Parallel()

	for _, rule := range []string{`[""]`, `["", a]`} {
		if got, err := parseRuleField(map[string]any{"rule": rule}, nil); err == nil {
			t.Errorf("parseRuleField(%s) = %q, want an error since no program is named", rule, got)
		}
	}
}

func TestRuleDescriptionShowsWrapperAndNearMissNotes(t *testing.T) {
	t.Parallel()

	suggestion := approve.Suggestion{Kind: approve.KindExact, Tokens: []string{"/usr/bin/nice", "-n", "19", "--", "make"}, HeadsWrapper: true, NearMiss: &approve.Rule{}}

	got := ruleDescription(suggestion, true)
	for _, want := range []string{"inner command", "heads a wrapper rule", "The wrapper rule", "stopped at the inner command's leading"} {
		if !strings.Contains(got, want) {
			t.Errorf("description lacks %q:\n%s", want, got)
		}
	}
}
