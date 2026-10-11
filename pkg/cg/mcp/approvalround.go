package mcp

import (
	"bytes"
	"errors"
	"fmt"
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

// approvalID names the input request for command i of n. The index is zero-padded to the width of
// n so that lexical order is command order. Without the padding, "approval-10" would show before
// "approval-2".
func approvalID(i, n int) string {
	return fmt.Sprintf("approval-%0*d", len(strconv.Itoa(n)), i+1)
}

// request ends a first call that needs approval. It builds one approval form per command that
// needs a prompt, plus one trailing form when the project rules file has changed on disk since it
// was loaded, and stores what the user was shown under a fresh nonce. Nothing runs and nothing is
// written until the client retries with the answers.
//
// A project file that cannot be read is treated as unchanged. The retry reads it again, and a
// remember that cannot read it either is skipped with a warning.
func (g *gate) request(call approvalCall, needs []int) (gateOutcome, error) {
	changed, current, err := g.store.CheckProjectDivergence()
	unreadable := err != nil
	if unreadable {
		changed, current = false, nil
	}

	commands := make([]pendingCommand, len(call.Argvs))
	for i := range call.Argvs {
		commands[i].Resolution = call.Resolutions[i]
	}

	forms := make(mcpsdk.InputRequestMap, len(needs)+1)
	for pos, i := range needs {
		suggestion := approve.SuggestPrefix(call.Argvs[i], call.Resolutions[i].RulePath())
		commands[i].Prompted = true
		commands[i].Suggestion = suggestion

		forms[approvalID(i, len(call.Argvs))] = &mcpsdk.ElicitParams{
			Message:         approvalMessage(call.Argvs[i], call.Cwd, pos+1, len(needs)),
			RequestedSchema: approvalSchema(suggestion, g.store.Project.Path),
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
// Past that check, a decline, cancel, missing answer, or malformed rule refuses the call, but only
// after every accepted form's remember has been persisted, so a user who approved one command with
// remember checked does not lose that rule to a problem with another. The first answer error is
// the one reported. Refusal always comes before any spawn.
func (g *gate) answer(call approvalCall, entry *pendingEntry, needs []int) (gateOutcome, error) {
	var declined [][]string
	var rules [][]string
	var answerErr error

	for _, i := range needs {
		cmd := entry.Commands[i]
		if !cmd.Prompted {
			return gateOutcome{}, &pendingRefusal{msg: fmt.Sprintf("%s refused: the rules changed and this command was not part of the approval that was requested; %s", call.Tool, pendingAdvice)}
		}
		if resolutionChanged(cmd.Resolution, call.Resolutions[i]) {
			return gateOutcome{}, &pendingRefusal{msg: fmt.Sprintf("%s refused: the command now resolves to a different executable than the one that was shown; %s", call.Tool, pendingAdvice)}
		}
	}

	for _, i := range needs {
		cmd := entry.Commands[i]

		res, err := elicitAnswer(call.Responses, approvalID(i, len(call.Argvs)))
		if err != nil {
			if answerErr == nil {
				answerErr = err
			}
			continue
		}

		// A declined or cancelled prompt refuses the command this once and persists nothing.
		// Elicitation only returns form content on accept, so the remember checkbox cannot ride
		// along with a decline to record a deny rule. We deliberately do not add an in-form
		// "always accept / always decline" enum to work around that: the prompt already carries
		// Accept and Decline buttons, and duplicating that choice inside the form is clunky.
		if res.Action != actionAccept {
			declined = append(declined, call.Argvs[i])
			continue
		}

		if !remember(res.Content) {
			continue
		}

		tokens, err := parseRuleField(res.Content, cmd.Suggestion)
		if err != nil {
			if answerErr == nil {
				answerErr = err
			}
			continue
		}
		if !slices.ContainsFunc(rules, func(r []string) bool { return slices.Equal(r, tokens) }) {
			rules = append(rules, tokens)
		}
	}

	warning := g.persistRemembered(call, entry, rules)
	if answerErr != nil {
		return gateOutcome{}, fmt.Errorf("%s refused: %w", call.Tool, answerErr)
	}
	if len(declined) > 0 {
		return gateOutcome{}, declinedError(call.Tool, declined, len(call.Argvs) > 1, warning)
	}

	return gateOutcome{Warning: warning}, nil
}

// persistRemembered writes the rules remembered in one round with a single divergence answer.
// Persistence is best-effort: the commands were approved, so a write failure or a skipped
// divergence still lets the run proceed. It returns a diagnostic the caller surfaces in the tool
// result, or empty on success, rather than writing to the server's stderr, which an MCP host
// would bleed onto the screen.
//
// The file on disk must still be what the first round saw. Otherwise the user's choice describes
// a file that no longer exists, and an unreviewed change is never adopted.
func (g *gate) persistRemembered(call approvalCall, entry *pendingEntry, rules [][]string) string {
	if len(rules) == 0 {
		return ""
	}

	if entry.Unreadable {
		return fmt.Sprintf("skipping remember: %s could not be read when the approval was requested; %s", g.store.Project.Path, pendingAdvice)
	}

	changed, current, err := g.store.CheckProjectDivergence()
	if err != nil {
		return fmt.Sprintf("skipping remember: %v", err)
	}
	if changed != entry.Diverged || !bytes.Equal(current, entry.DivergenceSnapshot) {
		return fmt.Sprintf("skipping remember: %s changed on disk since the approval was requested; %s", g.store.Project.Path, pendingAdvice)
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
	for _, tokens := range rules {
		if err := g.store.AppendProjectAllowPrefix(tokens, strategy); err != nil {
			failures = append(failures, rememberFailure(g.store.Project.Path, err))
		}
	}

	return strings.Join(failures, "; ")
}

// rememberFailure words a failed rule write. A file that changed between the divergence check and
// the write reads like the other skipped remembers.
func rememberFailure(path string, err error) string {
	if errors.Is(err, approve.ErrProjectChanged) {
		return fmt.Sprintf("skipping remember: %s changed on disk since the approval was requested; %s", path, pendingAdvice)
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
