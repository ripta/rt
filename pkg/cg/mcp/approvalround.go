package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
)

// divergenceID is the input request id of the trailing divergence form. It sorts after every
// approval id, which matters because a client shows the forms in the key order of the JSON
// object, and Go marshals map keys sorted.
const divergenceID = "divergence"

// approvalID names the input request for form i of n. The index is zero-padded to the width of n
// so that lexical order is form order. Without the padding, "approval-10" would show before
// "approval-2".
func approvalID(i, n int) string {
	return fmt.Sprintf("approval-%0*d", len(strconv.Itoa(n)), i+1)
}

// approvalForm is one approval prompt: the commands it lists, all of which share a suggested rule.
// Inner reports that the rule covers the inner command of a wrapper for at least one of them.
type approvalForm struct {
	Suggestion approve.Suggestion
	Commands   []int
	Inner      bool
}

// promptTarget is the command a prompt asks about. That is the typed command, or the inner
// command a wrapper rule peeled out of it when the wrapper rule is allowed and the inner one
// is not.
type promptTarget struct {
	Offset     int
	Argv       []string
	Resolution *model.Resolution
}

// promptTargetOf finds the command a prompting verdict asks about. The last layer of the chain
// is the one without a rule.
func promptTargetOf(call approvalCall, i int, v approve.Verdict) promptTarget {
	if n := len(v.Chain); n > 0 && v.Chain[n-1].Offset > 0 {
		last := v.Chain[n-1]

		return promptTarget{Offset: last.Offset, Argv: last.Argv, Resolution: last.Resolution}
	}

	return promptTarget{Argv: call.Argvs[i], Resolution: call.Resolutions[i]}
}

// planForms groups the commands that need a prompt by their suggested rule, kind and tokens
// together. Groups come in the order their first command appears, commands keep call order inside
// a group, and a group longer than maxFormCommands splits into consecutive forms under the same
// rule.
func planForms(rs *approve.Ruleset, needs []int, targets map[int]promptTarget) []approvalForm {
	var groups []approvalForm
	index := make(map[string]int)

	for _, i := range needs {
		target := targets[i]
		suggestion := rs.Suggest(target.Argv, target.Resolution)
		key := fmt.Sprintf("%d\x00%s", suggestion.Kind, strings.Join(suggestion.Tokens, "\x00"))

		at, ok := index[key]
		if !ok {
			at = len(groups)
			index[key] = at
			groups = append(groups, approvalForm{Suggestion: suggestion})
		}

		groups[at].Commands = append(groups[at].Commands, i)
		groups[at].Inner = groups[at].Inner || target.Offset > 0
	}

	var forms []approvalForm
	for _, group := range groups {
		for chunk := range slices.Chunk(group.Commands, maxFormCommands) {
			forms = append(forms, approvalForm{Suggestion: group.Suggestion, Commands: chunk, Inner: group.Inner})
		}
	}

	return forms
}

// request ends a first call that needs approval. It builds one approval form per suggested rule
// among the commands that need a prompt, plus one trailing form when the project rules file has
// changed on disk since it was loaded, and stores what the user was shown under a fresh nonce.
// Nothing runs and nothing is written until the client retries with the answers.
//
// A project file that cannot be read is treated as unchanged. The retry reads it again, and a
// remember that cannot read it either is skipped with a warning.
func (g *gate) request(call approvalCall, needs []int, verdicts []approve.Verdict) (gateOutcome, error) {
	changed, current, err := g.store.CheckProjectDivergence()
	unreadable := err != nil
	if unreadable {
		changed, current = false, nil
	}

	commands := make([]pendingCommand, len(call.Argvs))
	for i := range call.Argvs {
		commands[i].Resolution = call.Resolutions[i]

		for _, layer := range verdicts[i].Chain {
			if layer.Offset > 0 {
				commands[i].Chain = append(commands[i].Chain, pendingLayer{Offset: layer.Offset, Resolution: layer.Resolution})
			}
		}
	}

	targets := make(map[int]promptTarget, len(needs))
	for _, i := range needs {
		targets[i] = promptTargetOf(call, i, verdicts[i])
	}

	plan := planForms(g.store.Ruleset(), needs, targets)
	forms := make(mcpsdk.InputRequestMap, len(plan)+1)
	for pos, form := range plan {
		id := approvalID(pos, len(plan))

		argvs := make([][]string, len(form.Commands))
		for j, i := range form.Commands {
			argvs[j] = call.Argvs[i]
			commands[i].Prompted = true
			commands[i].Suggestion = form.Suggestion
			commands[i].PromptOffset = targets[i].Offset
			commands[i].Form = id
		}

		forms[id] = &mcpsdk.ElicitParams{
			Message:         approvalMessage(argvs, call.Cwd, pos+1, len(plan)),
			RequestedSchema: approvalSchema(form.Suggestion, form.Inner, g.store.Project.Path),
		}
	}

	if changed {
		forms[divergenceID] = &mcpsdk.ElicitParams{
			Message:         divergenceMessage(g.store.Project.Snapshot, current, g.store.Project.Path),
			RequestedSchema: divergenceSchema(),
		}
	}

	nonce, err := g.pending().add(pendingEntry{
		Tool:               call.Tool,
		Argvs:              call.Argvs,
		Cwd:                call.Cwd,
		Env:                call.Env,
		Args:               call.Args,
		Commands:           commands,
		DivergenceSnapshot: current,
		Diverged:           changed,
		Unreadable:         unreadable,
	})
	if err != nil {
		return gateOutcome{}, fmt.Errorf("%s refused: %w", call.Tool, err)
	}

	return gateOutcome{Pending: &mcpsdk.CallToolResult{InputRequests: forms, RequestState: nonce}}, nil
}

// answer applies the client's answers to a retry. needs holds the commands the live ruleset
// still wants approved. Every other command was allowed by a rule and needs no answer.
//
// A stale round is refused whole before any answer is read, and nothing is persisted. A command
// that was not prompted, or that now resolves to a different executable, means the user answered
// against a state that no longer holds. They are prompted again on the next call.
//
// Past that check, each form is read once through the ids the pending entry recorded. A decline,
// cancel, missing answer, or malformed rule on a form with commands that still need a prompt
// refuses the call, but only after every accepted form's remember has been persisted, so a user
// who approved one form with remember checked does not lose that rule to a problem with another.
// The first answer error is the one reported. Refusal always comes before any spawn.
//
// A form whose commands are all allowed now gates nothing. Its answer is read only for the
// remember, and a problem with it becomes a warning instead of a refusal.
func (g *gate) answer(call approvalCall, entry *pendingEntry, needs []int, verdicts []approve.Verdict) (gateOutcome, error) {
	resolve := approve.CommandResolver(call.Cwd)

	needed := make([]bool, len(call.Argvs))
	for _, i := range needs {
		needed[i] = true

		cmd := entry.Commands[i]
		if !cmd.Prompted {
			return gateOutcome{}, &pendingRefusal{msg: fmt.Sprintf("%s refused: the rules changed and this command was not part of the approval that was requested; %s", call.Tool, pendingAdvice)}
		}
		if resolutionChanged(cmd.Resolution, call.Resolutions[i]) {
			return gateOutcome{}, &pendingRefusal{msg: fmt.Sprintf("%s refused: the command now resolves to a different executable than the one that was shown; %s", call.Tool, pendingAdvice)}
		}
		if promptTargetOf(call, i, verdicts[i]).Offset != cmd.PromptOffset {
			return gateOutcome{}, &pendingRefusal{msg: fmt.Sprintf("%s refused: the rules changed and the prompt is now about a different part of this command than the one that was shown; %s", call.Tool, pendingAdvice)}
		}

		for _, layer := range cmd.Chain {
			inner := call.Argvs[i][layer.Offset:]
			if resolutionChanged(layer.Resolution, resolve(inner)) {
				return gateOutcome{}, &pendingRefusal{msg: fmt.Sprintf("%s refused: the inner command `%s` now resolves to a different executable than the one that was shown; %s", call.Tool, visibleArgs(inner), pendingAdvice)}
			}
		}
	}

	var declined []int
	var rules []rememberRule
	var notes []string
	var answerErr error

	formIDs, members := entryForms(entry)
	for _, id := range formIDs {
		asking := slices.DeleteFunc(slices.Clone(members[id]), func(i int) bool { return !needed[i] })
		suggestion := entry.Commands[members[id][0]].Suggestion

		res, err := elicitAnswer(call.Responses, id)
		if len(asking) == 0 {
			if err != nil || res.Action != actionAccept || !remember(res.Content) {
				continue
			}

			tokens, err := parseRuleField(res.Content, suggestion.Tokens)
			if err != nil {
				notes = append(notes, fmt.Sprintf("skipping remember: %v", err))
				continue
			}

			rules = addRememberRule(rules, suggestion.Kind, tokens, false)
			continue
		}

		if err != nil {
			if answerErr == nil {
				answerErr = err
			}
			continue
		}

		// A declined or cancelled prompt refuses its commands this once and persists nothing.
		// Elicitation only returns form content on accept, so the remember checkbox cannot ride
		// along with a decline to record a deny rule. We deliberately do not add an in-form
		// "always accept / always decline" enum to work around that: the prompt already carries
		// Accept and Decline buttons, and duplicating that choice inside the form is clunky.
		if res.Action != actionAccept {
			declined = append(declined, asking...)
			continue
		}

		if !remember(res.Content) {
			continue
		}

		tokens, err := parseRuleField(res.Content, suggestion.Tokens)
		if err != nil {
			if answerErr == nil {
				answerErr = err
			}
			continue
		}

		rules = addRememberRule(rules, suggestion.Kind, tokens, true)
	}

	// The refusals below still carry the remember warning. The rules from the accepted forms were
	// already written or skipped, and the user would otherwise never learn which.
	if persisted := g.persistRemembered(call, entry, rules); persisted != "" {
		notes = append(notes, persisted)
	}

	warning := strings.Join(notes, "; ")
	if answerErr != nil {
		if warning != "" {
			return gateOutcome{}, fmt.Errorf("%s refused: %w\n%s", call.Tool, answerErr, warning)
		}

		return gateOutcome{}, fmt.Errorf("%s refused: %w", call.Tool, answerErr)
	}
	if len(declined) > 0 {
		slices.Sort(declined)

		argvs := make([][]string, len(declined))
		for j, i := range declined {
			argvs[j] = call.Argvs[i]
		}

		return gateOutcome{}, declinedError(call.Tool, argvs, len(call.Argvs) > 1, warning)
	}

	return gateOutcome{Warning: warning}, nil
}

// entryForms lists the approval form ids of a pending entry in form order, with the commands each
// one covered. Ids are zero-padded, so sorting them lexically sorts them numerically.
func entryForms(entry *pendingEntry) ([]string, map[string][]int) {
	members := make(map[string][]int)
	for i, cmd := range entry.Commands {
		if cmd.Form != "" {
			members[cmd.Form] = append(members[cmd.Form], i)
		}
	}

	ids := slices.Sorted(maps.Keys(members))

	return ids, members
}

// rememberRule is a rule a user asked to save, of the kind the form suggested.
//
// Retryable reports whether any form that asked for it still held a command needing a prompt.
// Calling the tool again offers such a rule afresh. For any other rule the commands no longer
// prompt, so a new call would never offer it.
type rememberRule struct {
	Kind      approve.RuleKind
	Tokens    []string
	Retryable bool
}

// addRememberRule adds a rule once. Several forms of one group carry the same rule, and saving it
// twice would only duplicate the entry. An exact and a prefix rule over the same tokens are two
// rules, since they allow different things.
func addRememberRule(rules []rememberRule, kind approve.RuleKind, tokens []string, retryable bool) []rememberRule {
	for i := range rules {
		if rules[i].Kind == kind && slices.Equal(rules[i].Tokens, tokens) {
			rules[i].Retryable = rules[i].Retryable || retryable

			return rules
		}
	}

	return append(rules, rememberRule{Kind: kind, Tokens: tokens, Retryable: retryable})
}

// persistRemembered writes the rules remembered in one round with a single divergence answer.
// Persistence is best-effort: the commands were approved, so a write failure or a skipped
// divergence still lets the run proceed. It returns a diagnostic the caller surfaces in the tool
// result, or empty on success, rather than writing to the server's stderr, which an MCP host
// would bleed onto the screen.
//
// The file on disk must still be what the first round saw. Otherwise the user's choice describes
// a file that no longer exists, and an unreviewed change is never adopted.
//
// When the first round showed no divergence form, the file need only match the store's live
// snapshot. A rule that cg itself wrote between the two calls moves that snapshot, and skipping
// for it would drop a remember the user asked for. A round that showed a diff keeps the stricter
// check, since the user's choice describes that diff.
func (g *gate) persistRemembered(call approvalCall, entry *pendingEntry, rules []rememberRule) string {
	if len(rules) == 0 {
		return ""
	}

	path := g.store.Project.Path
	if entry.Unreadable {
		return skippedRemember(path, rules, "could not be read when the approval was requested")
	}

	changed, current, err := g.store.CheckProjectDivergence()
	if err != nil {
		return fmt.Sprintf("skipping remember: %v", err)
	}

	stale := changed
	if entry.Diverged {
		stale = !changed || !bytes.Equal(current, entry.DivergenceSnapshot)
	}
	if stale {
		return skippedRemember(path, rules, "changed on disk since the approval was requested")
	}

	strategy := approve.WriteDirect
	if entry.Diverged {
		var warning string
		strategy, warning = divergenceStrategy(call.Responses)
		if strategy < 0 {
			return warning
		}
	}

	var failures []string
	for _, rule := range rules {
		if err := g.writeRemembered(rule, strategy); err != nil {
			failures = append(failures, rememberFailure(path, rule, err))
		}
	}

	return strings.Join(failures, "; ")
}

// writeRemembered saves one remembered rule with the writer for its kind. Remember never creates
// a wrapper rule. A wrapper rule says that a program re-executes its arguments, which only the
// user can know.
func (g *gate) writeRemembered(rule rememberRule, strategy approve.WriteStrategy) error {
	switch rule.Kind {
	case approve.KindExact:
		return g.store.AppendProjectAllowExact(rule.Tokens, strategy)
	case approve.KindPrefix:
		return g.store.AppendProjectAllowPrefix(rule.Tokens, strategy)
	}

	return fmt.Errorf("cannot remember a %s rule", rule.Kind)
}

// skippedRemember words a skip that applies to every rule of the round. reason finishes the
// sentence that begins with the project file's path.
func skippedRemember(path string, rules []rememberRule, reason string) string {
	var out []string
	if slices.ContainsFunc(rules, func(r rememberRule) bool { return r.Retryable }) {
		out = append(out, fmt.Sprintf("skipping remember: %s %s; %s", path, reason, pendingAdvice))
	}

	for _, rule := range rules {
		if !rule.Retryable {
			out = append(out, unsavedRule(path, rule, reason))
		}
	}

	return strings.Join(out, "; ")
}

// unsavedRule words a skipped rule whose commands no longer prompt. It names the rule and the
// file and leaves out the advice to call again, because a new call would not offer the rule.
func unsavedRule(path string, rule rememberRule, reason string) string {
	return fmt.Sprintf("skipping remember: rule %s was not saved because %s %s", renderFlowSeq(rule.Tokens), path, reason)
}

// rememberFailure words a failed rule write. A file that changed between the divergence check and
// the write reads like the other skipped remembers.
func rememberFailure(path string, rule rememberRule, err error) string {
	if errors.Is(err, approve.ErrProjectChanged) {
		return skippedRemember(path, []rememberRule{rule}, "changed on disk since the approval was requested")
	}

	return fmt.Sprintf("remember write failed: %v", err)
}

// divergenceStrategy reads the trailing form's answer. It returns a negative strategy to skip
// persistence. A skip the user chose carries no warning. A missing answer does, since it means
// the client dropped the form.
func divergenceStrategy(responses mcpsdk.InputResponseMap) (approve.WriteStrategy, string) {
	res, err := elicitAnswer(responses, divergenceID)
	if err != nil {
		return -1, "skipping remember: the client did not answer the divergence prompt"
	}
	if res.Action != actionAccept {
		return -1, ""
	}

	switch choice(res.Content, "choice") {
	case divergeReloadMerge:
		return approve.WriteReloadMerge, ""
	case divergeOverwrite:
		return approve.WriteOverwrite, ""
	default:
		return -1, ""
	}
}

// pendingAdvice is the next step for any refusal that spent its pending entry.
const pendingAdvice = "call the tool again to be prompted afresh"

// pendingRefusal is a refusal that leads with its reason and still matches ErrPendingApproval.
type pendingRefusal struct{ msg string }

func (e *pendingRefusal) Error() string { return e.msg }

func (e *pendingRefusal) Is(target error) bool { return target == ErrPendingApproval }

// elicitAnswer reads the elicitation result for one input request id. A missing or mistyped
// answer means the client did not answer what was asked, and the entry is already spent, so the
// error says to call again.
func elicitAnswer(responses mcpsdk.InputResponseMap, id string) (*mcpsdk.ElicitResult, error) {
	resp, ok := responses[id]
	if !ok {
		return nil, fmt.Errorf("%w: no answer for input request %q", ErrPendingApproval, id)
	}

	res, ok := resp.(*mcpsdk.ElicitResult)
	if !ok || res == nil {
		return nil, fmt.Errorf("%w: input request %q was answered with %T, want an elicitation result", ErrPendingApproval, id, resp)
	}

	return res, nil
}

// resolutionChanged reports whether two resolutions name different executables. The user approved
// the executable that was resolved when the form was built, so a changed one needs a fresh
// prompt.
func resolutionChanged(a, b *model.Resolution) bool {
	if a == nil || b == nil {
		return a != b
	}

	return a.Resolved != b.Resolved || a.Canonical != b.Canonical
}

// declinedError refuses the call for every declined command, listed in command order. The warning
// is the remember diagnostic from the accepted forms. A refusal has no result to carry it, so it
// rides in the error text.
//
// A pool is a call with more than one distinct command. It always names what was declined, so the
// user can tell which form refused. A lone command is the one that was asked about.
func declinedError(tool string, declined [][]string, pool bool, warning string) error {
	var b strings.Builder
	if len(declined) == 1 {
		fmt.Fprintf(&b, "%s refused: command was declined at the approval prompt", tool)
	} else {
		fmt.Fprintf(&b, "%s refused: %d commands were declined at the approval prompt", tool, len(declined))
	}

	if pool {
		for _, argv := range declined {
			fmt.Fprintf(&b, "\n  %s", model.EscapeArgs(argv))
		}
	}

	if warning != "" {
		fmt.Fprintf(&b, "\n%s", warning)
	}

	return errors.New(b.String())
}
