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

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
)

// checkOptions holds the flags for `cg check`.
type checkOptions struct {
	Cwd           string
	Env           map[string]string
	ProjectConfig []string
	Output        string
	Shell         bool
}

// checkResult is the `cg check --output json` payload: the evaluated command,
// the verdict, and the rule that produced it, when there is one.
type checkResult struct {
	Command  []string  `json:"command"`
	Decision string    `json:"decision"`
	Section  string    `json:"section,omitempty"`
	Rule     *ruleInfo `json:"rule,omitempty"`

	// Deciding lists every rule of a tie: the matching rules no other outranks. It is empty when
	// one rule decided, which Rule then names.
	Deciding   []ruleInfo  `json:"deciding,omitempty"`
	Restricted bool        `json:"restricted,omitempty"`
	BadEnvs    []string    `json:"bad_envs,omitempty"`
	Chain      []chainInfo `json:"chain,omitempty"`
	Skipped    []skipInfo  `json:"skipped,omitempty"`

	// Matches lists every allow that matched the typed command's layer, ranked. Each chain entry
	// carries the same list for its own layer.
	Matches []matchInfo `json:"matches,omitempty"`

	// Warnings describe the loaded rules, not this command, and do not affect the
	// verdict. A --shell run carries them once, on the aggregate payload.
	Warnings []string `json:"warnings,omitempty"`
}

// chainInfo is the JSON-facing description of one layer of a wrapper chain, from
// the typed command inward. Resolved is the path an inner layer's executable
// resolved to, and is empty when it did not resolve. Candidates and PathForm
// describe the deciding set of the step that settled the layer.
type chainInfo struct {
	Offset     int         `json:"offset"`
	Command    []string    `json:"command"`
	Resolved   string      `json:"resolved,omitempty"`
	Wrappers   []ruleInfo  `json:"wrappers,omitempty"`
	Step       string      `json:"step"`
	Rule       *ruleInfo   `json:"rule,omitempty"`
	Candidates []ruleInfo  `json:"candidates,omitempty"`
	PathForm   bool        `json:"path_form,omitempty"`
	Matches    []matchInfo `json:"matches,omitempty"`
}

// matchInfo is the JSON-facing description of one allow that matched a layer. Rule is set for an
// ordinary rule. A wrapper chain that competed with the ordinary rules sets Wrappers, the wrapper rules
// the peel chose, and Inner, the rules the inner layer settled on.
//
// Count, Kind, Form, and Layer are the rank the match competed at. Deciding marks the deciding set.
type matchInfo struct {
	Rule     *ruleInfo  `json:"rule,omitempty"`
	Wrappers []ruleInfo `json:"wrappers,omitempty"`
	Inner    []ruleInfo `json:"inner,omitempty"`
	Count    int        `json:"count"`
	Kind     string     `json:"kind"`
	Form     string     `json:"form"`
	Layer    string     `json:"layer"`
	Deciding bool       `json:"deciding"`
}

// skipInfo is the JSON-facing description of a wrapper rule that matched a layer
// and did not peel.
type skipInfo struct {
	Offset int      `json:"offset"`
	Rule   ruleInfo `json:"rule"`
	Reason string   `json:"reason"`
}

// checkShellResult is the `cg check --shell --output json` payload: the
// original shell string, the aggregate decision across every command it
// contains, and one checkResult per extracted command.
type checkShellResult struct {
	Shell    string        `json:"shell"`
	Decision string        `json:"decision"`
	Commands []checkResult `json:"commands"`
	Warnings []string      `json:"warnings,omitempty"`
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
			"prompt for interactive approval.\n\n" +
			"With --shell, COMMAND is instead a single shell command string, e.g.\n" +
			"cg check --shell -- 'git status && git branch'. It is parsed as Bash and\n" +
			"every command it chains or nests, through &&, ||, |, ;, &, subshells, or\n" +
			"command substitution, is checked; the exit code and verdict reflect the\n" +
			"worst outcome across all of them: any refusal refuses, else any prompt\n" +
			"prompts, else it runs.",

		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: opts.run,
	}

	c.Flags().StringVar(&opts.Cwd, "cwd", "", "working directory to resolve COMMAND against; defaults to the current directory")
	c.Flags().StringToStringVar(&opts.Env, "env", nil, "environment override to check for dangerous variables (repeatable, KEY=VALUE)")
	c.Flags().StringSliceVar(&opts.ProjectConfig, "project-config", nil,
		"project-specific approval rules files, tried in order (first existing wins; default "+strings.Join(approve.DefaultProjectFiles(), ", ")+"). A relative value joins the project root. An absolute value, or one starting with ~, names that exact file")
	c.Flags().StringVarP(&opts.Output, "output", "o", "text", "output format: text or json")
	c.Flags().BoolVar(&opts.Shell, "shell", false, "treat COMMAND as a single shell command string; check every command it chains or nests instead of a pre-split argv")

	// Stop flag parsing at the first positional so cg flags must precede the
	// program; everything after passes through to the checked command
	// untouched. The -- separator stays honored regardless.
	c.Flags().SetInterspersed(false)

	return c
}

func (opts *checkOptions) run(cmd *cobra.Command, args []string) error {
	switch opts.Output {
	case "text", "json":
	default:
		fmt.Fprintf(cmd.ErrOrStderr(), "unsupported --output value: %q (supported: text, json)\n", opts.Output)
		return &model.ExitError{Code: 2}
	}

	commands, shellSrc, err := opts.resolveCommands(cmd, args)
	if err != nil {
		return err
	}

	store, err := approve.Load(approve.LoadOptions{ProjectFiles: opts.ProjectConfig})
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "loading approval rules: %v\n", err)
		return &model.ExitError{Code: 2}
	}

	resolver := approve.CommandResolver(opts.Cwd)
	verdicts := make([]approve.Verdict, len(commands))
	results := make([]checkResult, len(commands))
	explained := make([][]approve.LayerMatches, len(commands))
	for i, argv := range commands {
		// A command that fails to resolve is not an error here: it just means
		// canonical/resolved path rules cannot match, mirroring how the cg_run gate
		// treats an unresolvable executable. See model.ResolveCommand and gate.check.
		resolved, _ := model.ResolveCommand(argv, opts.Cwd)
		subject := approve.Subject{Argv: argv, Canonical: resolved.CanonicalArgv(), Resolved: resolved.ResolvedArgv()}
		verdicts[i] = store.Ruleset().Evaluate(subject, opts.Env, resolver)
		explained[i] = store.Ruleset().Explain(subject, opts.Env, resolver)
		results[i] = checkResultFrom(argv, verdicts[i], explained[i])
	}

	aggregate := aggregateDecision(verdicts)

	warnings := approve.WarningMessages(store.Warnings())

	if opts.Output == "json" {
		var payload any
		if opts.Shell {
			payload = checkShellResult{Shell: shellSrc, Decision: decisionString(aggregate), Commands: results, Warnings: warnings}
		} else {
			results[0].Warnings = warnings
			payload = results[0]
		}
		if err := writeJSON(cmd.OutOrStdout(), payload); err != nil {
			return err
		}
	} else {
		if opts.Shell {
			writeCheckShellText(cmd.OutOrStdout(), shellSrc, commands, verdicts, explained, store.Project.Path)
		} else {
			writeCheckText(cmd.OutOrStdout(), commands[0], verdicts[0], explained[0], store.Project.Path)
		}

		for _, warning := range warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
		}
	}

	switch aggregate {
	case approve.DecisionRun:
		return nil
	case approve.DecisionRefuse:
		return &model.ExitError{Code: 1}
	default:
		return &model.ExitError{Code: 3}
	}
}

// resolveCommands turns the command-line invocation into one or more argv
// slices to evaluate. Without --shell, args is used as a single argv
// unchanged. With --shell, args must hold exactly one shell command string,
// which extractShellCommands parses and walks for every command it contains.
func (opts *checkOptions) resolveCommands(cmd *cobra.Command, args []string) (commands [][]string, shellSrc string, err error) {
	if !opts.Shell {
		if len(args) == 0 {
			_ = cmd.Usage()
			return nil, "", &model.ExitError{Code: 2}
		}
		return [][]string{args}, "", nil
	}

	if len(args) != 1 {
		fmt.Fprintln(cmd.ErrOrStderr(), "--shell takes exactly one argument: the shell command string, e.g. cg check --shell -- 'git status && git branch'")
		return nil, "", &model.ExitError{Code: 2}
	}
	shellSrc = args[0]

	commands, parseErr := extractShellCommands(shellSrc)
	if parseErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "parsing shell command: %v\n", parseErr)
		return nil, "", &model.ExitError{Code: 2}
	}
	if len(commands) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "no command found in shell string")
		return nil, "", &model.ExitError{Code: 2}
	}

	return commands, shellSrc, nil
}

// aggregateDecision reduces per-command verdicts to one overall decision for
// --shell: any refusal refuses, else any prompt prompts, else it runs. This
// mirrors the deny > allow > restrict > prompt precedence Match already
// applies within a single command, extended across the commands a shell
// string contains.
func aggregateDecision(verdicts []approve.Verdict) approve.Decision {
	agg := approve.DecisionRun
	for _, v := range verdicts {
		switch v.Decision {
		case approve.DecisionRefuse:
			return approve.DecisionRefuse
		case approve.DecisionPrompt:
			agg = approve.DecisionPrompt
		}
	}
	return agg
}

// checkResultFrom builds the JSON payload for a verdict.
func checkResultFrom(argv []string, v approve.Verdict, layers []approve.LayerMatches) checkResult {
	res := checkResult{
		Command:    argv,
		Decision:   decisionString(v.Decision),
		Section:    ruleSectionFor(v),
		Restricted: v.Restricted,
		BadEnvs:    v.BadEnvs,
	}
	if v.Rule != nil {
		info := ruleInfoFor(v.Rule)
		res.Rule = &info
	}
	if len(v.Deciding) > 1 {
		for _, r := range v.Deciding {
			res.Deciding = append(res.Deciding, ruleInfoFor(r))
		}
	}

	for _, cl := range v.Chain {
		info := chainInfoFrom(cl)
		if lm, ok := layerAt(layers, cl.Offset); ok {
			info.Matches = matchInfosFrom(lm)
		}

		res.Chain = append(res.Chain, info)
	}
	if lm, ok := layerAt(layers, 0); ok {
		res.Matches = matchInfosFrom(lm)
	}
	for _, sk := range v.Skipped {
		res.Skipped = append(res.Skipped, skipInfo{Offset: sk.Offset, Rule: ruleInfoFor(sk.Rule), Reason: sk.Reason.String()})
	}

	return res
}

// layerAt finds the explained layer at an offset.
func layerAt(layers []approve.LayerMatches, offset int) (approve.LayerMatches, bool) {
	for _, lm := range layers {
		if lm.Offset == offset {
			return lm, true
		}
	}

	return approve.LayerMatches{}, false
}

// matchInfosFrom describes the matches of one layer for the JSON output, in ranked order.
func matchInfosFrom(lm approve.LayerMatches) []matchInfo {
	var out []matchInfo

	for _, m := range lm.Matches {
		info := matchInfo{Count: m.Count, Kind: m.Kind, Form: formName(m), Layer: m.Layer, Deciding: m.Deciding()}

		if m.Rule != nil {
			rule := ruleInfoFor(m.Rule)
			info.Rule = &rule
		}
		for _, r := range m.Wrappers {
			info.Wrappers = append(info.Wrappers, ruleInfoFor(r))
		}
		for _, r := range m.Inner {
			info.Inner = append(info.Inner, ruleInfoFor(r))
		}

		out = append(out, info)
	}

	return out
}

func formName(m approve.Match) string {
	if m.PathForm {
		return "path"
	}

	return "bare"
}

// ruleInfoFor describes a rule for the JSON output.
func ruleInfoFor(r *approve.Rule) ruleInfo {
	return ruleInfo{Kind: r.Kind().String(), Pattern: r.Pattern(), Message: r.Message}
}

// chainInfoFrom describes one chain layer for the JSON output.
func chainInfoFrom(cl approve.ChainLayer) chainInfo {
	info := chainInfo{
		Offset:   cl.Offset,
		Command:  cl.Argv,
		Resolved: resolvedPath(cl.Resolution),
		Step:     cl.Step.String(),
		PathForm: cl.PathForm,
	}
	for _, r := range cl.Wrappers {
		info.Wrappers = append(info.Wrappers, ruleInfoFor(r))
	}
	if cl.Rule != nil {
		rule := ruleInfoFor(cl.Rule)
		info.Rule = &rule
	}
	for _, r := range cl.Candidates {
		info.Candidates = append(info.Candidates, ruleInfoFor(r))
	}

	return info
}

// resolvedPath is the executable path an inner layer resolved to, or empty when
// it did not resolve.
func resolvedPath(res *model.Resolution) string {
	if res == nil || (res.Resolved == "" && res.Canonical == "") {
		return ""
	}

	return res.RulePath()
}

// writeCheckText renders a verdict as a short human-readable report.
func writeCheckText(w io.Writer, argv []string, v approve.Verdict, layers []approve.LayerMatches, projectPath string) {
	fmt.Fprintf(w, "%s: %s\n", decisionString(v.Decision), model.EscapeArgs(argv))

	if v.Rule != nil {
		kind := ""
		if v.Rule.Kind() == approve.KindWrapper {
			kind = "wrapper "
		}
		fmt.Fprintf(w, "  rule: %s %s%s\n", ruleSectionFor(v), kind, v.Rule.Pattern())
		if v.Rule.Message != "" {
			fmt.Fprintf(w, "  message: %s\n", v.Rule.Message)
		}
	}

	writeTiedText(w, v)

	if len(v.Chain) == 0 {
		if lm, ok := layerAt(layers, 0); ok {
			writeMatchesText(w, lm, "  ")
		}
	}

	writeChainText(w, v, layers)

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

// writeTiedText names every rule of a tie. The rules are equally specific, so they decide together.
func writeTiedText(w io.Writer, v approve.Verdict) {
	if len(v.Deciding) < 2 {
		return
	}

	fmt.Fprintf(w, "  decided together by %d equally specific rules:\n", len(v.Deciding))

	for _, r := range v.Deciding {
		fmt.Fprintf(w, "    %s", r.String())
		if r.Message != "" {
			fmt.Fprintf(w, ": %s", r.Message)
		}
		fmt.Fprintln(w)
	}
}

// writeChainText renders the wrapper chain behind a verdict, then the wrapper
// rules that matched and did not peel. Every layer of a chain must permit a
// dangerous override, so a refusal names the layer that refused it.
func writeChainText(w io.Writer, v approve.Verdict, layers []approve.LayerMatches) {
	if len(v.Chain) > 0 {
		fmt.Fprintln(w, "  chain:")
	}

	for i, cl := range v.Chain {
		fmt.Fprintf(w, "    [%d] %s", i, model.EscapeArgs(cl.Argv))
		if path := resolvedPath(cl.Resolution); path != "" {
			fmt.Fprintf(w, " (%s)", path)
		}
		fmt.Fprintln(w)

		if len(cl.Wrappers) > 0 {
			patterns := make([]string, len(cl.Wrappers))
			for j, r := range cl.Wrappers {
				patterns[j] = r.Pattern()
			}
			fmt.Fprintf(w, "        peeled by wrapper %s\n", strings.Join(patterns, " or "))
		}

		switch {
		case cl.Rule == nil:
			fmt.Fprintf(w, "        %s\n", cl.Step)
		case cl.Step == approve.StepPeel:
			fmt.Fprintf(w, "        peel runs, permitted by %s\n", chainRuleLabel(cl))
		case cl.Step == approve.StepInner:
			fmt.Fprintf(w, "        inner verdict stands, peel permitted by %s\n", chainRuleLabel(cl))
		case cl.Step == approve.StepEnvRefusal:
			fmt.Fprintf(w, "        %s: %s\n", cl.Step, chainRuleLabel(cl))
		default:
			fmt.Fprintf(w, "        %s\n", chainRuleLabel(cl))
		}

		if lm, ok := layerAt(layers, cl.Offset); ok {
			writeMatchesText(w, lm, "        ")
		}
	}

	if len(v.Chain) > 0 && len(v.BadEnvs) > 0 {
		fmt.Fprintf(w, "  env refused at layer [%d]\n", len(v.Chain)-1)
	}

	for _, sk := range v.Skipped {
		fmt.Fprintf(w, "  wrapper %s did not peel at offset %d: %s\n", sk.Rule.Pattern(), sk.Offset, sk.Reason)
	}
}

// writeMatchesText lists every allow that matched a layer, in ranked order. The deciding set is
// marked with a word, so the mark survives without color. A rule outranked by another still shows,
// which tells a rule that did not match from one that matched and ranked lower.
func writeMatchesText(w io.Writer, lm approve.LayerMatches, indent string) {
	if len(lm.Matches) == 0 {
		return
	}

	fmt.Fprintf(w, "%smatching allows, most specific first:\n", indent)

	for _, m := range lm.Matches {
		mark := "outranked"
		if m.Deciding() {
			mark = "deciding"
		}

		fmt.Fprintf(w, "%s  %-9s %s (count %d, %s, %s, %s)\n", indent, mark, matchLabel(m), m.Count, m.Kind, formName(m), m.Layer)
	}
}

// matchLabel names a match: the rule, or the wrapper chain with the rules its inner layer settled on.
func matchLabel(m approve.Match) string {
	if m.Rule != nil {
		return m.Rule.String()
	}

	wrappers := make([]string, len(m.Wrappers))
	for i, r := range m.Wrappers {
		wrappers[i] = r.Pattern()
	}

	inner := make([]string, len(m.Inner))
	for i, r := range m.Inner {
		inner[i] = r.String()
	}

	return fmt.Sprintf("wrapper %s over %s", strings.Join(wrappers, " or "), strings.Join(inner, " and "))
}

// chainRuleLabel names a chain layer's rule with the list it came from: a wrapper
// rule, or an allow, deny, or restrict rule by the step that settled the layer.
func chainRuleLabel(cl approve.ChainLayer) string {
	pattern := cl.Rule.Pattern()

	switch {
	case cl.Rule.Kind() == approve.KindWrapper:
		return "wrapper " + pattern
	case cl.Step == approve.StepDeny:
		return "deny " + pattern
	case cl.Step == approve.StepRestrict:
		return "restrict " + pattern
	default:
		return "allow " + pattern
	}
}

// writeCheckShellText renders the aggregate decision for the original shell
// string, followed by each extracted command's own verdict via writeCheckText.
func writeCheckShellText(w io.Writer, shellSrc string, commands [][]string, verdicts []approve.Verdict, explained [][]approve.LayerMatches, projectPath string) {
	fmt.Fprintf(w, "%s: %s\n", decisionString(aggregateDecision(verdicts)), shellSrc)

	for i, argv := range commands {
		fmt.Fprintf(w, "\n[%d] ", i+1)
		writeCheckText(w, argv, verdicts[i], explained[i], projectPath)
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
	case v.Decision == approve.DecisionRun || len(v.BadEnvs) > 0 || lastStep(v) == approve.StepTieRefusal:
		return "allow"
	default:
		return "deny"
	}
}

// lastStep is the step that settled the layer behind the verdict: the last chain layer's, or the
// typed command's when no wrapper was involved.
func lastStep(v approve.Verdict) approve.Step {
	if n := len(v.Chain); n > 0 {
		return v.Chain[n-1].Step
	}

	return v.Step
}
