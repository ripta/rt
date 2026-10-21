package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
)

// gate is the per-server approval gate consulted before cg_run and cg_run_many exec a command. It
// holds the whole config store, not just the ruleset, so the interactive persistence path can
// reuse it. A nil gate bypasses every check; the server always builds a real gate, so nil only
// occurs in tests that exercise the non-gated paths.
//
// The gate never prompts the user itself. A command that needs approval ends the call with input
// requests, and the client's retry carries the answers. See approvalCall.
type gate struct {
	store        *approve.Store
	blindlyAllow bool

	pendingOnce sync.Once
	pendingApps *pendingStore
}

// ruleWarnings returns the warnings for the rules the server holds now, for cg_info. They go to
// the tool result and never to stderr, which an MCP host shows on screen.
func (g *gate) ruleWarnings() []string {
	if g == nil || g.store == nil {
		return nil
	}

	var out []string
	for _, w := range g.store.Warnings() {
		out = append(out, w.Message)
	}

	return out
}

// pending returns the store of calls waiting on the user, creating it on first use so a gate
// built as a literal still works.
func (g *gate) pending() *pendingStore {
	g.pendingOnce.Do(func() { g.pendingApps = newPendingStore() })

	return g.pendingApps
}

// approvalCall is one gated tool call, seen from the gate. Argvs holds each distinct command in
// the call, with Resolutions parallel to it. cg_run is the one-command case.
//
// State and Responses are empty on the first call. The retry carries the request state the first
// call returned and the client's answers, keyed by the ids approvalID and divergenceID produce.
type approvalCall struct {
	Tool        string
	Argvs       [][]string
	Resolutions []*model.Resolution
	Cwd         string
	Env         map[string]string

	// Args is the canonical JSON of the whole tool input. The retry must present the same bytes,
	// so an approval covers exactly the call the user was asked about.
	Args []byte

	// CanPrompt reports whether the client advertised elicitation. A command that needs approval
	// fails closed without it.
	CanPrompt bool

	State     string
	Responses mcpsdk.InputResponseMap
}

// newApprovalCall builds the gate's view of a tool request. A nil request, as in tests that skip
// the protocol, has no session and so cannot prompt. in is the handler's decoded tool input. It is
// re-encoded rather than taken from the raw arguments, so formatting differences between the two
// calls do not matter.
func newApprovalCall(req *mcpsdk.CallToolRequest, tool string, in any, argvs [][]string, resolutions []*model.Resolution, cwd string, env map[string]string) (approvalCall, error) {
	args, err := json.Marshal(in)
	if err != nil {
		return approvalCall{}, fmt.Errorf("%s refused: encoding the call for approval: %w", tool, err)
	}

	call := approvalCall{
		Tool:        tool,
		Argvs:       argvs,
		Resolutions: resolutions,
		Cwd:         cwd,
		Env:         env,
		Args:        args,
		CanPrompt:   elicitationAvailable(req),
	}
	if req != nil && req.Params != nil {
		call.State = req.Params.RequestState
		call.Responses = req.Params.InputResponses
	}

	return call, nil
}

// gateOutcome is what a permitted call carries back to its handler.
type gateOutcome struct {
	// Pending is the input-required result to return instead of running anything. It is nil when
	// the call may run.
	Pending *mcpsdk.CallToolResult

	// Warning is a best-effort persistence diagnostic to surface alongside a permitted run, empty
	// when there is nothing to report.
	Warning string

	// Rewrites runs parallel to the call's argvs. Each entry maps an offset into the typed argv to
	// the path the wrapper receives there, so an inner command runs the file the gate judged. An
	// entry is nil when nothing is pinned. A gate that checks nothing returns nil.
	Rewrites []map[int]string
}

// check evaluates every command in the call. A nil error with a nil Pending permits execution of
// all of them. A refusal error blocks the whole call.
//
// A retry first redeems its pending entry, which deletes it, so a nonce works once even when the
// rules changed in between. The live ruleset is then evaluated again. Deny and restrict win over
// an accepted form, and an allow runs without its answer gating it.
//
// A retry reads its forms even when no command needs a prompt any more. A form whose commands are
// all allowed now can still carry a remember to save.
func (g *gate) check(call approvalCall) (gateOutcome, error) {
	if g == nil || g.blindlyAllow {
		return gateOutcome{}, nil
	}

	var entry *pendingEntry
	if call.State != "" {
		var err error
		entry, err = g.pending().take(call.State, call.Tool, call.Argvs, call.Cwd, call.Env, call.Args)
		if err != nil {
			return gateOutcome{}, fmt.Errorf("%s refused: %w", call.Tool, err)
		}
	}

	var needs []int
	verdicts := make([]approve.Verdict, len(call.Argvs))
	for i := range call.Argvs {
		v, err := g.evaluate(call, i)
		if err != nil {
			return gateOutcome{}, err
		}

		verdicts[i] = v
		if v.Decision == approve.DecisionPrompt {
			needs = append(needs, i)
		}
	}

	if len(needs) == 0 && entry == nil {
		return gateOutcome{Rewrites: pinnedPaths(verdicts)}, nil
	}
	if len(needs) > 0 && !call.CanPrompt {
		return gateOutcome{}, g.failClosedError(call.Tool)
	}
	if entry == nil {
		return g.request(call, needs, verdicts)
	}

	outcome, err := g.answer(call, entry, needs, verdicts)
	if err != nil {
		return gateOutcome{}, err
	}

	// A remembered rule may now decide a layer, and the pin follows the rule that decides it. So
	// the commands are judged again against the rules as they stand after the answers.
	for i := range call.Argvs {
		if verdicts[i], err = g.evaluate(call, i); err != nil {
			return gateOutcome{}, err
		}
	}
	outcome.Rewrites = pinnedPaths(verdicts)

	return outcome, nil
}

// pinnedPaths works out, for each judged command, the inner executables a wrapper must be
// handed. The top layer is never pinned, since the supervisor already runs the path the gate
// resolved.
//
// An inner layer is pinned to the path it resolved to when every rule that permits it is
// path-form. A bare-name rule accepts any binary of that name, so a layer it permits keeps
// the wrapper's own lookup. A layer still prompting was approved at the form, which showed its
// resolved path, so that layer is pinned to the path shown.
func pinnedPaths(verdicts []approve.Verdict) []map[int]string {
	out := make([]map[int]string, len(verdicts))

	for i, v := range verdicts {
		for j, layer := range v.Chain {
			if layer.Offset == 0 || layer.Resolution == nil || layer.Resolution.Resolved == "" {
				continue
			}

			shown := v.Decision == approve.DecisionPrompt && j == len(v.Chain)-1
			if !layer.PathForm && !shown {
				continue
			}

			if out[i] == nil {
				out[i] = make(map[int]string)
			}
			out[i][layer.Offset] = layer.Resolution.RulePath()
		}
	}

	return out
}

// evaluate checks one command against the live ruleset. A deny, restrict, or dangerous-env
// refusal comes back as an error. Evaluate refuses a dangerous env override before it returns a
// prompt verdict, so a prompt verdict always has a clean env.
func (g *gate) evaluate(call approvalCall, i int) (approve.Verdict, error) {
	resolved := call.Resolutions[i]
	subject := approve.Subject{Argv: call.Argvs[i], Canonical: resolved.CanonicalArgv(), Resolved: resolved.ResolvedArgv()}

	v := g.store.Ruleset().Evaluate(subject, call.Env, approve.CommandResolver(call.Cwd))
	if v.Decision != approve.DecisionRefuse {
		return v, nil
	}
	if len(v.BadEnvs) > 0 {
		return v, envRefusalError(call.Tool, v, g.store.Project.Path)
	}

	return v, refusalError(call.Tool, v)
}

// refusalError builds the error for a deny or restrict match, naming the rule
// kind and appending the rule's message when set so the agent sees why the
// command was blocked. A refusal behind a wrapper rule names the layer that refused and
// the rule that refused it.
func refusalError(tool string, v approve.Verdict) error {
	kind := "deny"
	if v.Restricted {
		kind = "restrict"
	}

	what := fmt.Sprintf("command matches a %s rule", kind)
	if layer, ok := refusingLayer(v); ok {
		what = fmt.Sprintf("%s matches %s", layerName(layer), ruleLabel(kind, v.Rule))
	}

	if v.Rule != nil && v.Rule.Message != "" {
		return fmt.Errorf("%s refused: %s: %s", tool, what, v.Rule.Message)
	}
	return fmt.Errorf("%s refused: %s", tool, what)
}

// envRefusalError builds the error for a DecisionRefuse produced by the
// dangerous-env gate rather than a deny or restrict rule. A matched allow rule
// (Rule non-nil) points at permit_unsafe_envs; an unmatched command (Rule nil)
// has no rule to carry an exemption, so the message points at the project file instead.
// Behind a wrapper rule, the message names the layer and the rule that lacked the permit.
func envRefusalError(tool string, v approve.Verdict, projectPath string) error {
	bad := strings.Join(v.BadEnvs, ", ")
	if layer, ok := refusingLayer(v); ok && v.Rule != nil {
		return fmt.Errorf("%s refused: env override sets %s, which %s on %s does not permit; list them under permit_unsafe_envs to allow", tool, bad, ruleLabel("allow", v.Rule), layerName(layer))
	}
	if v.Rule != nil {
		return fmt.Errorf("%s refused: env override sets %s, which the matching allow rule does not permit; list them under permit_unsafe_envs to allow", tool, bad)
	}
	return fmt.Errorf("%s refused: env override sets %s, which a prompted command cannot permit; add an allow rule with permit_unsafe_envs to %s", tool, bad, projectPath)
}

// refusingLayer returns the layer a wrapper chain refused at. It reports false when no wrapper
// rule was involved, so the refusal is about the typed command alone.
func refusingLayer(v approve.Verdict) (approve.ChainLayer, bool) {
	if len(v.Chain) == 0 {
		return approve.ChainLayer{}, false
	}

	return v.Chain[len(v.Chain)-1], true
}

// layerName names a chain layer for a message: the typed command, or the inner command a
// wrapper rule peeled out of it.
func layerName(layer approve.ChainLayer) string {
	if layer.Offset == 0 {
		return "the command"
	}

	return fmt.Sprintf("the inner command `%s`", visibleArgs(layer.Argv))
}

// ruleLabel names a rule with the list it came from.
func ruleLabel(list string, rule *approve.Rule) string {
	if rule == nil {
		return "a " + list + " rule"
	}

	return fmt.Sprintf("the %s rule %s", list, visibleText(rule.String()))
}

// failClosedError builds the error for a command that matched neither allow
// nor deny when no interactive prompt is available. The message points at the
// ways to permit the command.
func (g *gate) failClosedError(tool string) error {
	return fmt.Errorf("%s refused: no rule matched and the client cannot prompt for approval; add an allow rule to %s or start cg mcp with --blindly-allow", tool, g.store.Project.Path)
}

// elicitationAvailable reports whether the calling client advertised the
// elicitation capability. It is nil-safe so handlers can call it on a request
// that has no session.
//
// SEP-2575 moves client capabilities into each request's _meta from protocol
// 2026-07-28 on, and _meta is authoritative for that request. The SDK reads
// _meta first and falls back to the initialize handshake for older protocols.
func elicitationAvailable(req *mcpsdk.CallToolRequest) bool {
	if req == nil {
		return false
	}
	caps := req.ClientCapabilities()
	return caps != nil && caps.Elicitation != nil
}
