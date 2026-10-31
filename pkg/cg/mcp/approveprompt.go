package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	jsonschema "github.com/google/jsonschema-go/jsonschema"
	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"

	"github.com/ripta/rt/pkg/cg/approve"
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
	return buildApprovalMessage(argvs, cwd, pos, total, "")
}

// askAgainMessage is approvalMessage for a form the second round asks again. The position stays the
// one the form had in the first round, so the user can tell which form this is. The suffix says
// the command is not what changed.
func askAgainMessage(argvs [][]string, cwd string, pos, total int) string {
	return buildApprovalMessage(argvs, cwd, pos, total, " The remembered rule needs another look.")
}

func buildApprovalMessage(argvs [][]string, cwd string, pos, total int, suffix string) string {
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
	b.WriteString(suffix)

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

// hasControl reports whether s holds a character that could break a line, drive the terminal, or
// display differently from what runs. That is a C0 or C1 control, DEL, a Unicode line or paragraph
// separator, or a format character such as a bidi override or a zero-width joiner.
func hasControl(s string) bool {
	return strings.ContainsFunc(s, needsEscape)
}

func needsEscape(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029'
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
		case needsEscape(r) && r > 0xffff:
			fmt.Fprintf(&b, `\U%08x`, r)
		case needsEscape(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}

	b.WriteString("'")

	return b.String()
}

// formSpec is what an approval form shows for one rule field.
//
// Suggestion is the original pre-fill. Prefill replaces it as the field's default when it is set,
// which a second round does with the edit that failed. ExtraArgs is the allow-extra-arguments
// box's default. Note leads the field's description, and carries why a second round was asked or
// why the pre-fill cannot be remembered as it stands.
type formSpec struct {
	Suggestion approve.Suggestion
	Prefill    []string
	ExtraArgs  bool
	Inner      bool
	Note       string
	Path       string

	// NoteNamesCredential is set when Note already says where the credential is, so the
	// description does not repeat it.
	NoteNamesCredential bool
}

// approvalSchema builds the elicitation form: an editable rule field pre-filled
// with the suggested rule as a YAML flow sequence, an allow-extra-arguments box, and a remember
// checkbox. spec.Path is the project rules file the remembered rule is saved to.
//
// The field's title names no rule kind, because the kind follows from the edit. The message has
// room for the command and nothing else, because the client shows only its first
// visibleMessageLines lines. So what the rule means goes in the field's description: how the edit
// decides the kind, that it covers an inner command, that it is exact because the program heads a
// wrapper rule, and which wrapper rule nearly matched.
//
// A program that heads a wrapper rule gets no box, since its remembered rule is always exact.
//
// The form is a typed *jsonschema.Schema rather than a property map so
// PropertyOrder controls the field order on the wire: a property map marshals
// its keys alphabetically, which would float "remember" above "rule". The client
// renders fields in wire order, so listing rule first keeps the command the
// prompt is about at the top, above the remember toggle.
func approvalSchema(spec formSpec) *jsonschema.Schema {
	prefill := spec.Prefill
	if prefill == nil {
		prefill = spec.Suggestion.Tokens
	}

	ruleDefault, _ := json.Marshal(renderFlowSeq(prefill))
	schema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"rule": {
				Type:        "string",
				Title:       "Allow rule",
				Description: ruleDescription(spec),
				Default:     ruleDefault,
			},
			"remember": {
				Type:        "boolean",
				Title:       "Remember this rule",
				Description: fmt.Sprintf("save an allow rule to %s so it is not asked again", spec.Path),
				Default:     json.RawMessage("false"),
			},
		},
		PropertyOrder: []string{"rule", "remember"},
	}

	if spec.Suggestion.HeadsWrapper {
		return schema
	}

	schema.Properties[extraArgsField] = &jsonschema.Schema{
		Type:        "boolean",
		Title:       "Allow extra arguments",
		Description: "Allow extra arguments after the unchanged command. A trimmed rule always allows them.",
		Default:     json.RawMessage(strconv.FormatBool(spec.ExtraArgs)),
	}
	schema.PropertyOrder = []string{"rule", extraArgsField, "remember"}

	return schema
}

// extraArgsField is the name of the allow-extra-arguments box.
const extraArgsField = "allow_extra_args"

// ruleDescription explains the rule field. The notes come first, because they say what the
// pre-filled rule is for, and the editing hint closes the text.
func ruleDescription(spec formSpec) string {
	var b strings.Builder

	if spec.Note != "" {
		b.WriteString(spec.Note)
		b.WriteString(" ")
	}

	if spec.Inner {
		b.WriteString("The rule covers the inner command after the wrapper. ")
	}

	shown := spec.Prefill
	if shown == nil {
		shown = spec.Suggestion.Tokens
	}
	if positions := approve.CredentialPositions(shown); len(positions) > 0 && !spec.NoteNamesCredential {
		fmt.Fprintf(&b, "The rule holds what looks like a credential at %s, and remember will not save it. ", approve.DescribePositions(positions))
	}

	suggestion := spec.Suggestion
	if suggestion.HeadsWrapper {
		b.WriteString("This program heads a wrapper rule, so a remembered rule is always exact and allows only this whole command. Only the first token may change. ")
	} else {
		b.WriteString("An exact rule fixes every argument, and its first token decides which programs it covers. Leave the rule as it is for an exact rule, tick the box for a prefix rule of the whole command, or trim it to a leading part for a prefix rule that allows any command starting with those tokens. ")
	}

	if suggestion.NearMiss != nil {
		fmt.Fprintf(&b, "The wrapper rule %s stopped at the inner command's leading '-'. To allow this form, add a second wrapper rule ending in --. Appending -- to the existing rule would stop it matching the form without --. ", visibleText(suggestion.NearMiss.String()))
	}

	b.WriteString("Edited only when remembering; a YAML list of argv tokens, e.g. [go, test] or [foo, \"bar baz\"]")

	return b.String()
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
// bespoke parser. An empty token after the first is valid, since a command can carry one.
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
	if tokens[0] == "" {
		return nil, fmt.Errorf("the rule's first token must name a program")
	}

	return tokens, nil
}

// renderFlowSeq renders tokens as a YAML flow sequence ([a, b]) for the prompt's pre-filled rule
// field, quoting tokens when needed. An empty token renders as "" and survives parseRuleField.
func renderFlowSeq(tokens []string) string {
	parts := make([]string, len(tokens))
	for i, t := range tokens {
		parts[i] = renderFlowToken(t)
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

// renderFlowToken renders one token as a flow-sequence element. A token with a character that
// needsEscape is written as a YAML double-quoted scalar with escapes, because the YAML emitter
// passes bidi and other format characters through raw. Any other token goes through the emitter
// inside a one-element flow sequence, so it is quoted for flow context.
func renderFlowToken(t string) string {
	if hasControl(t) {
		return yamlDoubleQuote(t)
	}

	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t})

	out, err := yaml.Marshal(seq)
	if err != nil {
		return yamlDoubleQuote(t)
	}

	one := strings.TrimRight(string(out), "\n")

	return strings.TrimSuffix(strings.TrimPrefix(one, "["), "]")
}

// yamlDoubleQuote writes s as a YAML double-quoted scalar. Characters that needsEscape use the
// \xXX, \uXXXX, and \UXXXXXXXX forms, so the text shows what the argument holds.
func yamlDoubleQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')

	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			fmt.Fprintf(&b, `\x%02x`, r)
		case needsEscape(r) && r > 0xffff:
			fmt.Fprintf(&b, `\U%08x`, r)
		case needsEscape(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}

	b.WriteByte('"')

	return b.String()
}
