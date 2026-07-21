// Package approvecmd implements the `cg check` and `cg lint` subcommands. It
// lives outside package cg because it depends on package approve, which
// itself depends on package cg (for EscapeArgs); package cg cannot import
// approve directly without an import cycle, the same constraint that already
// puts `cg mcp` in its own package.
package approvecmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/cg"
	"github.com/ripta/rt/pkg/cg/approve"
)

// checkOptions holds the flags for `cg check`.
type checkOptions struct {
	Cwd           string
	Env           map[string]string
	ProjectConfig []string
	Output        string
}

// checkResult is the `cg check --output json` payload: the evaluated command,
// the verdict, and the rule that produced it, when there is one.
type checkResult struct {
	Command    []string  `json:"command"`
	Decision   string    `json:"decision"`
	Section    string    `json:"section,omitempty"`
	Rule       *ruleInfo `json:"rule,omitempty"`
	Restricted bool      `json:"restricted,omitempty"`
	BadEnvs    []string  `json:"bad_envs,omitempty"`
}

// ruleInfo is the JSON-facing description of the rule that decided a verdict.
type ruleInfo struct {
	Kind    string `json:"kind"`
	Pattern string `json:"pattern"`
	Message string `json:"message,omitempty"`
}

// NewCheckCommand returns the `cg check` subcommand. It evaluates a command
// against the cg_run approval rules without executing it, so a script or an
// operator can ask what would happen before running something for real.
func NewCheckCommand() *cobra.Command {
	opts := &checkOptions{}
	c := &cobra.Command{
		Use:   "check [flags] -- COMMAND [ARGS...]",
		Short: "Check whether the approval rules would run, refuse, or prompt for a command",
		Long: "Evaluate COMMAND against the cg_run approval rules without running it.\n\n" +
			"Exit codes report the verdict: 0 the command would run, 1 it would be\n" +
			"refused, 2 the invocation itself was invalid (bad flags, missing command,\n" +
			"or the approval rules failed to load), 3 no rule matched and it would\n" +
			"prompt for interactive approval.",

		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: opts.run,
	}

	c.Flags().StringVar(&opts.Cwd, "cwd", "", "working directory to resolve COMMAND against; defaults to the current directory")
	c.Flags().StringToStringVar(&opts.Env, "env", nil, "environment override to check for dangerous variables (repeatable, KEY=VALUE)")
	c.Flags().StringSliceVar(&opts.ProjectConfig, "project-config", nil,
		"project-specific approval rules files, relative to the project root, tried in order (first existing wins; default "+strings.Join(approve.DefaultProjectFiles(), ", ")+")")
	c.Flags().StringVarP(&opts.Output, "output", "o", "text", "output format: text or json")

	// Stop flag parsing at the first positional so cg flags must precede the
	// program; everything after passes through to the checked command
	// untouched. The -- separator stays honored regardless.
	c.Flags().SetInterspersed(false)

	return c
}

func (opts *checkOptions) run(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		_ = cmd.Usage()
		return &cg.ExitError{Code: 2}
	}

	switch opts.Output {
	case "text", "json":
	default:
		fmt.Fprintf(cmd.ErrOrStderr(), "unsupported --output value: %q (supported: text, json)\n", opts.Output)
		return &cg.ExitError{Code: 2}
	}

	store, err := approve.Load(approve.LoadOptions{ProjectFiles: opts.ProjectConfig})
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "loading approval rules: %v\n", err)
		return &cg.ExitError{Code: 2}
	}

	// A command that fails to resolve is not an error here: it just means
	// canonical/resolved path rules cannot match, mirroring how the cg_run gate
	// treats an unresolvable executable. See cg.ResolveCommand and gate.check.
	resolved, _ := cg.ResolveCommand(args, opts.Cwd)
	subject := approve.Subject{Argv: args, Canonical: resolved.CanonicalArgv(), Resolved: resolved.ResolvedArgv()}
	v := store.Ruleset().Evaluate(subject, opts.Env)

	if opts.Output == "json" {
		if err := writeJSON(cmd.OutOrStdout(), checkResultFrom(args, v)); err != nil {
			return err
		}
	} else {
		writeCheckText(cmd.OutOrStdout(), args, v, store.Project.Path)
	}

	switch v.Decision {
	case approve.DecisionRun:
		return nil
	case approve.DecisionRefuse:
		return &cg.ExitError{Code: 1}
	default:
		return &cg.ExitError{Code: 3}
	}
}

// checkResultFrom builds the JSON payload for a verdict.
func checkResultFrom(argv []string, v approve.Verdict) checkResult {
	res := checkResult{
		Command:    argv,
		Decision:   decisionString(v.Decision),
		Section:    ruleSectionFor(v),
		Restricted: v.Restricted,
		BadEnvs:    v.BadEnvs,
	}
	if v.Rule != nil {
		res.Rule = &ruleInfo{Kind: ruleKindString(v.Rule.Kind()), Pattern: describeRulePattern(v.Rule), Message: v.Rule.Message}
	}

	return res
}

// writeCheckText renders a verdict as a short human-readable report.
func writeCheckText(w io.Writer, argv []string, v approve.Verdict, projectPath string) {
	fmt.Fprintf(w, "%s: %s\n", decisionString(v.Decision), cg.EscapeArgs(argv))

	if v.Rule != nil {
		fmt.Fprintf(w, "  rule: %s %s\n", ruleSectionFor(v), describeRulePattern(v.Rule))
		if v.Rule.Message != "" {
			fmt.Fprintf(w, "  message: %s\n", v.Rule.Message)
		}
	}

	if len(v.BadEnvs) > 0 {
		fmt.Fprintf(w, "  blocked env: %s\n", strings.Join(v.BadEnvs, ", "))
		if v.Rule != nil {
			fmt.Fprintln(w, "  hint: list them under permit_unsafe_envs to allow")
		} else {
			fmt.Fprintf(w, "  hint: add an allow rule with permit_unsafe_envs to %s\n", projectPath)
		}
	}

	if v.Decision == approve.DecisionPrompt {
		fmt.Fprintln(w, "  hint: no rule matched; cg_run would prompt for interactive approval, or fail closed without one")
	}
}

// decisionString renders a Decision for both the text and JSON outputs.
func decisionString(d approve.Decision) string {
	switch d {
	case approve.DecisionRun:
		return "run"
	case approve.DecisionRefuse:
		return "refuse"
	default:
		return "prompt"
	}
}

// ruleSectionFor names which rule list decided v: allow, deny, or restrict.
// Rule is nil for allow-all, deny-all, prompt, and a refusal from the
// dangerous-env gate on an unmatched command, so it returns empty then.
func ruleSectionFor(v approve.Verdict) string {
	switch {
	case v.Rule == nil:
		return ""
	case v.Restricted:
		return "restrict"
	case v.Decision == approve.DecisionRun || len(v.BadEnvs) > 0:
		return "allow"
	default:
		return "deny"
	}
}

// ruleKindString renders a RuleKind for display.
func ruleKindString(k approve.RuleKind) string {
	switch k {
	case approve.KindExact:
		return "exact"
	case approve.KindPrefix:
		return "prefix"
	case approve.KindGlob:
		return "glob"
	case approve.KindRegex:
		return "regex"
	default:
		return "unknown"
	}
}

// describeRulePattern renders the pattern side of a rule for display: the argv
// tokens for exact/prefix, the raw pattern string for glob/regex.
func describeRulePattern(r *approve.Rule) string {
	switch r.Kind() {
	case approve.KindExact:
		return "[" + strings.Join(r.Exact, ", ") + "]"
	case approve.KindPrefix:
		return "[" + strings.Join(r.Prefix, ", ") + "]"
	case approve.KindGlob:
		return fmt.Sprintf("%q", r.Glob)
	case approve.KindRegex:
		return fmt.Sprintf("%q", r.Regex)
	default:
		return ""
	}
}
