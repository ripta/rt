package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	jsonschema "github.com/google/jsonschema-go/jsonschema"
	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"

	"github.com/ripta/rt/pkg/cg/model"
)

const (
	actionAccept = "accept"

	divergeReloadMerge = "reload_merge"
	divergeOverwrite   = "overwrite"
	divergeSkip        = "skip"

	// maxDiffLines bounds how tall the divergence body may grow. The elicitation
	// dialog does not scroll, so a taller body pushes the approval buttons off
	// screen; past this many lines the body collapses to changed lines, then to a
	// count summary.
	maxDiffLines = 10
	maxDiffBytes = 4000

	// visibleMessageLines is how many lines of a form's message the client shows. Claude Code's
	// input dialog prints the first three, then "(+N more lines)", and offers no way to expand
	// it. The cap does not change with terminal size.
	visibleMessageLines = 3

	// maxFormCommands is the most commands one approval form lists. The message is one header line
	// plus one line per command, and a command past the visible lines is hidden from the person
	// approving it. Rerun the cg_elicit_test approval-form probe if the client's cap changes.
	maxFormCommands = visibleMessageLines - 1
)

// approvalMessage renders the human-facing prompt body: a header with the count and the working
// directory, then every command the form covers as it will run, one per line.
//
// The client shows only the first visibleMessageLines lines, so the header takes one and the
// commands take the rest, with no blank lines. A call with several approval forms leads the header
// with its position, so every form stands alone. A lone form carries none.
func approvalMessage(argvs [][]string, cwd string, pos, total int) string {
	if cwd == "" {
		cwd = "(server cwd)"
	}
	cwd = visibleText(cwd)

	what := "this command"
	if len(argvs) != 1 {
		what = fmt.Sprintf("%d commands", len(argvs))
	}

	var b strings.Builder
	if total > 1 {
		fmt.Fprintf(&b, "Approval %d of %d: allow %s in %s?", pos, total, what, cwd)
	} else {
		fmt.Fprintf(&b, "Allow %s in %s?", what, cwd)
	}

	for _, argv := range argvs {
		fmt.Fprintf(&b, "\n%s", visibleArgs(argv))
	}

	return b.String()
}

// visibleArgs renders an argv as a shell command on exactly one line. An argument with a control
// character is written in ANSI-C quoting, so a newline cannot push the rest of the form out of the
// client's visible lines and an escape sequence cannot reach the terminal.
func visibleArgs(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		if hasControl(arg) {
			quoted[i] = ansiCQuote(arg)
		} else {
			quoted[i] = model.EscapeArgs([]string{arg})
		}
	}

	return strings.Join(quoted, " ")
}

// visibleText renders free text, such as a working directory, on one line. Text with a control
// character is written in ANSI-C quoting and anything else is left as it is.
func visibleText(s string) string {
	if hasControl(s) {
		return ansiCQuote(s)
	}

	return s
}

// hasControl reports whether s holds a character that could break a line or drive the terminal.
// That is a C0 or C1 control, DEL, or a Unicode line or paragraph separator.
func hasControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || r == ' ' || r == ' '
	})
}

// ansiCQuote quotes s as $'...', which bash and zsh read back to the same bytes.
func ansiCQuote(s string) string {
	var b strings.Builder
	b.WriteString("$'")

	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\'':
			b.WriteString(`\'`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case unicode.IsControl(r) || r == ' ' || r == ' ':
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}

	b.WriteString("'")

	return b.String()
}

// approvalSchema builds the elicitation form: an editable rule field pre-filled
// with the suggested prefix as a YAML flow sequence, followed by a remember
// checkbox. path is the project rules file the remembered rule is saved to.
//
// The form is a typed *jsonschema.Schema rather than a property map so
// PropertyOrder controls the field order on the wire: a property map marshals
// its keys alphabetically, which would float "remember" above "rule". The client
// renders fields in wire order, so listing rule first keeps the command the
// prompt is about at the top, above the remember toggle.
func approvalSchema(suggestion []string, path string) *jsonschema.Schema {
	ruleDefault, _ := json.Marshal(renderFlowSeq(suggestion))
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"rule": {
				Type:        "string",
				Title:       "Allow rule (prefix)",
				Description: "edited only when remembering; a YAML list of argv tokens, e.g. [go, test] or [foo, \"bar baz\"]",
				Default:     ruleDefault,
			},
			"remember": {
				Type:        "boolean",
				Title:       "Remember this rule",
				Description: fmt.Sprintf("save an allow rule to %s so it is not asked again", path),
				Default:     json.RawMessage("false"),
			},
		},
		PropertyOrder: []string{"rule", "remember"},
	}
}

// divergenceMessage renders the trailing form's body describing how the project
// file as loaded differs from its current on-disk content. path is the project
// rules file the change describes. The choice covers every rule remembered in
// the round and does nothing when no approval form has remember checked.
func divergenceMessage(snapshot, current []byte, path string) string {
	return fmt.Sprintf("%s changed on disk since it was loaded. How should remembered rules be saved? This applies to every approval above that has remember checked.\n\n%s", path, renderDivergence(snapshot, current))
}

// renderDivergence renders a compact view of the change between the loaded
// snapshot and the current on-disk content. A small change shows as a unified
// diff with one line of context; a taller change collapses to just the added and
// removed lines; a change whose changed lines still overflow collapses to a count
// summary. This keeps the body short enough that the approval buttons stay on
// screen, since the elicitation dialog does not scroll.
func renderDivergence(snapshot, current []byte) string {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(snapshot)),
		B:        difflib.SplitLines(string(current)),
		FromFile: "loaded",
		ToFile:   "on disk",
		Context:  1,
	})
	if err != nil {
		return "(could not render diff)"
	}

	diff = strings.TrimRight(diff, "\n")
	if countLines(diff) <= maxDiffLines {
		return clampBytes(diff)
	}

	changed := changedLines(diff)
	if len(changed) <= maxDiffLines {
		return clampBytes(strings.Join(changed, "\n"))
	}

	added, removed := 0, 0
	for _, line := range changed {
		if strings.HasPrefix(line, "+") {
			added++
		} else {
			removed++
		}
	}

	return fmt.Sprintf("%d line(s) added, %d line(s) removed (change too large to show)", added, removed)
}

// changedLines extracts only the added and removed lines from a unified diff,
// dropping the file headers, hunk headers, and context lines. The returned lines
// keep their leading + or - marker.
func changedLines(diff string) []string {
	var out []string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			out = append(out, line)
		}
	}

	return out
}

// countLines reports the number of newline-separated lines in s.
func countLines(s string) int {
	if s == "" {
		return 0
	}

	return strings.Count(s, "\n") + 1
}

// clampBytes guards against pathologically long lines by capping the body at
// maxDiffBytes, since the line-count bounds alone do not limit line width.
func clampBytes(s string) string {
	if len(s) > maxDiffBytes {
		return s[:maxDiffBytes] + "\n... (truncated)"
	}

	return s
}

// divergenceSchema builds the titled-enum form for reconciling an on-disk
// change to the project file.
func divergenceSchema() map[string]any {
	return obj(map[string]any{
		"choice": map[string]any{
			"type": "string", "title": "Resolve change",
			"description": "how to reconcile the on-disk change",
			"default":     divergeSkip,
			"oneOf": []any{
				map[string]any{"const": divergeReloadMerge, "title": "Reload and merge (keep disk changes, add the rules)"},
				map[string]any{"const": divergeOverwrite, "title": "Overwrite (drop disk changes)"},
				map[string]any{"const": divergeSkip, "title": "Skip (do not save the rules)"},
			},
		},
	}, "choice")
}

// remember reports whether the remember checkbox was checked. An unedited or
// absent field is treated as unchecked.
func remember(content map[string]any) bool {
	v, ok := content["remember"].(bool)
	return ok && v
}

// choice returns the string value of a form field, or empty when absent.
func choice(content map[string]any, key string) string {
	v, _ := content[key].(string)
	return v
}

// parseRuleField reads the edited rule field as a YAML sequence of argv tokens,
// falling back to the suggestion when the field is absent or blank. The flow- or
// block-sequence YAML keeps tokens with spaces expressible via quoting without a
// bespoke parser.
func parseRuleField(content map[string]any, suggestion []string) ([]string, error) {
	raw, ok := content["rule"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		if len(suggestion) == 0 {
			return nil, fmt.Errorf("no rule to remember")
		}
		return suggestion, nil
	}

	var tokens []string
	if err := yaml.Unmarshal([]byte(raw), &tokens); err != nil {
		return nil, fmt.Errorf("rule must be a YAML list of tokens like [go, test]: %w", err)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("rule must have at least one token")
	}
	for _, t := range tokens {
		if t == "" {
			return nil, fmt.Errorf("rule tokens must be non-empty")
		}
	}

	return tokens, nil
}

// renderFlowSeq renders tokens as a YAML flow sequence ([a, b]) for the prompt's
// pre-filled rule field, quoting tokens when needed.
func renderFlowSeq(tokens []string) string {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, t := range tokens {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t})
	}
	out, err := yaml.Marshal(seq)
	if err != nil {
		return strings.Join(tokens, " ")
	}

	return strings.TrimRight(string(out), "\n")
}
