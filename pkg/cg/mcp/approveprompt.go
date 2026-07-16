package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	jsonschema "github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"

	"github.com/ripta/rt/pkg/cg"
	"github.com/ripta/rt/pkg/cg/approve"
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
)

// prompt asks the user to approve an unmatched command. Accept runs the command
// and, when remember is checked, persists the edited prefix rule to the project
// file and swaps it into the live matcher. Decline and cancel refuse this once.
// The suggestion pre-fills the canonical executable path resolved for the run, so
// a remembered rule is strict by default; the user can edit it down to a name.
//
// A non-empty first return is a best-effort persistence diagnostic the caller
// surfaces in the tool result; the command was still approved and runs.
func (g *gate) prompt(ctx context.Context, tool string, in runInput, resolved *cg.Resolution, el elicitor) (string, error) {
	suggestion := approve.SuggestPrefix(in.Command, resolved.ExecPath())
	res, err := el.Elicit(ctx, &mcpsdk.ElicitParams{
		Message:         approvalMessage(in),
		RequestedSchema: approvalSchema(suggestion, g.store.Project.Path),
	})
	if err != nil {
		return "", fmt.Errorf("%s refused: approval prompt failed: %w", tool, err)
	}
	// A declined or cancelled prompt refuses the command this once and persists
	// nothing. Elicitation only returns form content on accept, so the remember
	// checkbox cannot ride along with a decline to record a deny rule, and there
	// is no way to remember a denial here. We deliberately do not add an in-form
	// "always accept / always decline" enum to work around that: the prompt
	// already carries Accept and Decline buttons, and duplicating that choice
	// inside the form is clunky.
	if res.Action != actionAccept {
		return "", fmt.Errorf("%s refused: command was declined at the approval prompt", tool)
	}

	if remember(res.Content) {
		tokens, err := parseRuleField(res.Content, suggestion)
		if err != nil {
			return "", fmt.Errorf("%s refused: %w", tool, err)
		}
		return g.persistRemember(ctx, tokens, el), nil
	}

	return "", nil
}

// persistRemember writes the remembered rule, resolving on-disk divergence
// through a second prompt. Persistence is best-effort: the command was approved,
// so a write failure or a skipped divergence still lets the run proceed. It
// returns a diagnostic the caller surfaces in the tool result, or empty on
// success, rather than writing to the server's stderr, which an MCP host would
// bleed onto the screen.
func (g *gate) persistRemember(ctx context.Context, tokens []string, el elicitor) string {
	changed, current, err := g.store.CheckProjectDivergence()
	if err != nil {
		return fmt.Sprintf("skipping remember: %v", err)
	}

	strategy := approve.WriteDirect
	if changed {
		strategy, err = g.resolveDivergence(ctx, current, el)
		if err != nil {
			return fmt.Sprintf("skipping remember: %v", err)
		}
		if strategy < 0 {
			return ""
		}
	}

	if err := g.store.AppendProjectAllowPrefix(tokens, strategy); err != nil {
		return fmt.Sprintf("remember write failed: %v", err)
	}

	return ""
}

// resolveDivergence prompts the user to reconcile an on-disk change to the
// project file. It returns the chosen write strategy, or a negative value to
// skip persistence. Without an elicitor, or on skip, it returns the skip signal.
func (g *gate) resolveDivergence(ctx context.Context, current []byte, el elicitor) (approve.WriteStrategy, error) {
	if el == nil {
		return -1, nil
	}

	res, err := el.Elicit(ctx, &mcpsdk.ElicitParams{
		Message:         divergenceMessage(g.store.Project.Snapshot, current, g.store.Project.Path),
		RequestedSchema: divergenceSchema(),
	})
	if err != nil {
		return -1, err
	}
	if res.Action != actionAccept {
		return -1, nil
	}

	switch choice(res.Content, "choice") {
	case divergeReloadMerge:
		return approve.WriteReloadMerge, nil
	case divergeOverwrite:
		return approve.WriteOverwrite, nil
	default:
		return -1, nil
	}
}

// approvalMessage renders the human-facing prompt body: the command as it will
// run and the working directory it runs in.
func approvalMessage(in runInput) string {
	cwd := in.Cwd
	if cwd == "" {
		cwd = "(server cwd)"
	}

	return fmt.Sprintf("Allow this command?\n\n  %s\n\nworking directory: %s", cg.EscapeArgs(in.Command), cwd)
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
				Title:       "Remember this command",
				Description: fmt.Sprintf("save an allow rule to %s so it is not asked again", path),
				Default:     json.RawMessage("false"),
			},
		},
		PropertyOrder: []string{"rule", "remember"},
	}
}

// divergenceMessage renders the second prompt's body describing how the project
// file as loaded differs from its current on-disk content. path is the project
// rules file the change describes.
func divergenceMessage(snapshot, current []byte, path string) string {
	return fmt.Sprintf("%s changed on disk since it was loaded. How should the remembered rule be saved?\n\n%s", path, renderDivergence(snapshot, current))
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
			"oneOf": []any{
				map[string]any{"const": divergeReloadMerge, "title": "Reload and merge (keep disk changes, add the rule)"},
				map[string]any{"const": divergeOverwrite, "title": "Overwrite (drop disk changes)"},
				map[string]any{"const": divergeSkip, "title": "Skip (do not save the rule)"},
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
