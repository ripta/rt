package mcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"

	"github.com/ripta/rt/pkg/cg"
	"github.com/ripta/rt/pkg/cg/approve"
)

// stderr is where best-effort persistence diagnostics go. It is a package
// variable so tests can capture the messages.
var stderr io.Writer = os.Stderr

const (
	actionAccept = "accept"

	divergeReloadMerge = "reload_merge"
	divergeOverwrite   = "overwrite"
	divergeSkip        = "skip"

	maxDiffBytes = 4000
)

// prompt asks the user to approve an unmatched command. Accept runs the command
// and, when remember is checked, persists the edited prefix rule to the project
// file and swaps it into the live matcher. Decline and cancel refuse this once.
func (g *gate) prompt(ctx context.Context, in runInput, el elicitor) error {
	suggestion := approve.SuggestPrefix(in.Command)
	res, err := el.Elicit(ctx, &mcpsdk.ElicitParams{
		Message:         approvalMessage(in),
		RequestedSchema: approvalSchema(suggestion),
	})
	if err != nil {
		return fmt.Errorf("cg_run refused: approval prompt failed: %w", err)
	}
	if res.Action != actionAccept {
		return fmt.Errorf("cg_run refused: command was declined at the approval prompt")
	}

	if remember(res.Content) {
		tokens, err := parseRuleField(res.Content, suggestion)
		if err != nil {
			return fmt.Errorf("cg_run refused: %w", err)
		}
		g.persistRemember(ctx, tokens, el)
	}

	return nil
}

// persistRemember writes the remembered rule, resolving on-disk divergence
// through a second prompt. Persistence is best-effort: the command was approved,
// so a write failure or a skipped divergence still lets the run proceed; the
// problem is reported to the server's stderr.
func (g *gate) persistRemember(ctx context.Context, tokens []string, el elicitor) {
	changed, current, err := g.store.CheckProjectDivergence()
	if err != nil {
		fmt.Fprintf(stderr, "cg_run: skipping remember: %v\n", err)
		return
	}

	strategy := approve.WriteDirect
	if changed {
		strategy, err = g.resolveDivergence(ctx, current, el)
		if err != nil {
			fmt.Fprintf(stderr, "cg_run: skipping remember: %v\n", err)
			return
		}
		if strategy < 0 {
			return
		}
	}

	if err := g.store.AppendProjectAllowPrefix(tokens, strategy); err != nil {
		fmt.Fprintf(stderr, "cg_run: remember write failed: %v\n", err)
	}
}

// resolveDivergence prompts the user to reconcile an on-disk change to the
// project file. It returns the chosen write strategy, or a negative value to
// skip persistence. Without an elicitor, or on skip, it returns the skip signal.
func (g *gate) resolveDivergence(ctx context.Context, current []byte, el elicitor) (approve.WriteStrategy, error) {
	if el == nil {
		return -1, nil
	}

	res, err := el.Elicit(ctx, &mcpsdk.ElicitParams{
		Message:         divergenceMessage(g.store.Project.Snapshot, current),
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

// approvalSchema builds the elicitation form: a remember checkbox and an
// editable rule field pre-filled with the suggested prefix as a YAML flow
// sequence.
func approvalSchema(suggestion []string) map[string]any {
	return obj(map[string]any{
		"remember": map[string]any{
			"type": "boolean", "title": "Remember this command",
			"description": "save an allow rule to .cg/approve.yaml so it is not asked again",
			"default":     false,
		},
		"rule": map[string]any{
			"type": "string", "title": "Allow rule (prefix)",
			"description": "edited only when remembering; a YAML list of argv tokens, e.g. [go, test] or [foo, \"bar baz\"]",
			"default":     renderFlowSeq(suggestion),
		},
	})
}

// divergenceMessage renders the second prompt's body with a unified diff of the
// project file as loaded versus its current on-disk content.
func divergenceMessage(snapshot, current []byte) string {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(snapshot)),
		B:        difflib.SplitLines(string(current)),
		FromFile: "loaded",
		ToFile:   "on disk",
		Context:  3,
	})
	if err != nil {
		diff = "(could not render diff)"
	}
	if len(diff) > maxDiffBytes {
		diff = diff[:maxDiffBytes] + "\n... (diff truncated)"
	}

	return fmt.Sprintf(".cg/approve.yaml changed on disk since it was loaded. How should the remembered rule be saved?\n\n%s", diff)
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
