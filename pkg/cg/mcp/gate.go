package mcp

import (
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
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

	return approve.WarningMessages(g.store.Warnings())
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

	// MultiRound reports whether the client fulfills input requests and retries on its own, which
	// a second round needs. Older clients get one round only.
	MultiRound bool

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
		MultiRound:  multiRoundAvailable(req),
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

	// Sandboxes runs parallel to the call's argvs. Each entry is the sandbox the command runs under,
	// from the deciding rule, or nil for an unsandboxed command. A gate that checks nothing returns
	// nil.
	Sandboxes []*approve.Sandbox

	// Skips runs parallel to Sandboxes. A non-empty entry says why the command runs without the
	// profile of the sandbox its rule names, because cg is inside an outer sandbox.
	Skips []string

	// NestedApproved lists the commands whose nested sandbox the user agreed, at this round's prompt,
	// to run without. It carries the answer from the round to the final outcome.
	NestedApproved []int
}

// skipAt returns why the i-th command runs without its sandbox's profile, or empty when it applies
// the profile or has no sandbox.
func (o gateOutcome) skipAt(i int) string {
	if i < len(o.Skips) {
		return o.Skips[i]
	}

	return ""
}

// sandboxAt returns the sandbox the i-th command runs under.
func (o gateOutcome) sandboxAt(i int) *approve.Sandbox {
	if i < len(o.Sandboxes) {
		return o.Sandboxes[i]
	}

	return nil
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
	if g == nil || (g.blindlyAllow && g.store == nil) {
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

	needs, asks, verdicts, plans, err := g.evaluateAll(call)
	if err != nil {
		return gateOutcome{}, err
	}

	if len(needs) == 0 && len(asks) == 0 && entry == nil {
		return g.permitted(call, verdicts, plans, nil)
	}
	if len(asks) > 0 && !call.CanPrompt {
		return gateOutcome{}, nestedFailClosedError(call.Tool, asks, plans)
	}
	if len(needs) > 0 && !call.CanPrompt {
		return gateOutcome{}, g.failClosedError(call.Tool)
	}
	if entry == nil {
		return g.request(call, needs, asks, verdicts, plans)
	}

	outcome, err := g.answer(call, entry, needs, asks, verdicts, plans)
	if err != nil {
		return gateOutcome{}, err
	}
	if outcome.Pending != nil {
		return outcome, nil
	}

	// A remembered rule may now decide a layer, and the pin follows the rule that decides it. So
	// the commands are judged again against the rules as they stand after the answers.
	_, _, verdicts, plans, err = g.evaluateAll(call)
	if err != nil {
		return gateOutcome{}, err
	}

	permitted, err := g.permitted(call, verdicts, plans, outcome.NestedApproved)
	if err != nil {
		return gateOutcome{}, err
	}
	outcome.Rewrites, outcome.Sandboxes, outcome.Skips = permitted.Rewrites, permitted.Sandboxes, permitted.Skips

	return outcome, nil
}

// evaluateAll judges every command in the call. needs are the commands that want a rule prompt, and
// asks the commands whose sandbox wants a prompt before it is skipped. A command is never in both,
// since a prompt-approved command has no rule and so no sandbox.
func (g *gate) evaluateAll(call approvalCall) (needs, asks []int, verdicts []approve.Verdict, plans []sandboxPlan, err error) {
	verdicts = make([]approve.Verdict, len(call.Argvs))
	plans = make([]sandboxPlan, len(call.Argvs))

	for i := range call.Argvs {
		v, plan, err := g.evaluate(call, i)
		if err != nil {
			return nil, nil, nil, nil, err
		}

		verdicts[i], plans[i] = v, plan

		switch {
		case v.Decision == approve.DecisionPrompt:
			needs = append(needs, i)
		case plan.Ask:
			asks = append(asks, i)
		}
	}

	return needs, asks, verdicts, plans, nil
}

// sandboxPlan is what the deciding rule's sandbox asks of a run.
//
// Sandbox is the sandbox the deciding rule names, or nil when it names none. Skip is set when cg is
// inside an outer sandbox and already_sandboxed lets the command run without the profile. It holds
// the reason for the run record. Ask is set when already_sandboxed is ask, so the command waits for
// an approval before it runs without the profile.
type sandboxPlan struct {
	Sandbox *approve.Sandbox
	Skip    string
	Ask     bool
}

const (
	skipByRule     = "cg is inside an outer sandbox and already_sandboxed is run, so the profile was not applied"
	skipByApproval = "cg is inside an outer sandbox and running without the profile was approved at the prompt, so the profile was not applied"
)

// permitted builds the outcome of a call whose commands all run: the inner pins, the sandboxes, and
// the reasons any profile is skipped. approved lists the commands whose nested sandbox the user
// agreed to run without. A command that waits on that agreement and has none is an error, never an
// unsandboxed run.
//
// Under allow-all the pins apply only to a sandboxed command, since the sandbox profile approves the
// command and its inner layers as one. Other commands under this mode run as they always have.
func (g *gate) permitted(call approvalCall, verdicts []approve.Verdict, plans []sandboxPlan, approved []int) (gateOutcome, error) {
	out := gateOutcome{Rewrites: pinnedPaths(verdicts), Sandboxes: make([]*approve.Sandbox, len(verdicts)), Skips: make([]string, len(verdicts))}

	allowAll := g.blindlyAllow || g.store.Ruleset().Mode == approve.ModeAllowAll

	for i, plan := range plans {
		out.Sandboxes[i] = plan.Sandbox

		switch {
		case plan.Ask && slices.Contains(approved, i):
			out.Skips[i] = skipByApproval
		case plan.Ask:
			return gateOutcome{}, &pendingRefusal{msg: fmt.Sprintf("%s refused: running `%s` without sandbox %q was not approved and the rules changed since; %s",
				call.Tool, visibleArgs(call.Argvs[i]), plan.Sandbox.Name, pendingAdvice)}
		default:
			out.Skips[i] = plan.Skip
		}

		if allowAll && plan.Sandbox == nil {
			out.Rewrites[i] = nil
		}
	}

	return out, nil
}

// sandboxFor works out what a verdict's sandbox asks of the run, and refuses a command whose
// sandbox cannot be applied. A rule that names a sandbox never runs without it, except through
// already_sandboxed: run, or an ask the user approves.
//
// The refusals are a sandbox with no block for this platform, a command whose executable did not
// resolve, since sandbox-exec would search PATH for it, and a profile the load probe could not
// confirm applies. Under an outer sandbox the stored class is nested, and already_sandboxed decides.
// Any other class refuses under every value, with the probe's stderr.
func (g *gate) sandboxFor(tool string, v approve.Verdict, resolved *model.Resolution) (sandboxPlan, error) {
	name := v.Sandbox()
	if name == "" {
		return sandboxPlan{}, nil
	}

	sb, ok := g.store.Ruleset().Sandbox(name)
	if !ok {
		return sandboxPlan{}, fmt.Errorf("%s refused: the rule that allows the command names sandbox %q, which is not defined", tool, name)
	}

	if sb.Seatbelt == nil {
		return sandboxPlan{}, fmt.Errorf("%s refused: the rule that allows the command names sandbox %q, which has no block for this platform (%s). It defines: %s",
			tool, name, runtime.GOOS, strings.Join(sb.Declared, ", "))
	}

	switch sb.Seatbelt.Class {
	case model.SandboxApplies:
		if resolved == nil || resolved.Resolved == "" {
			return sandboxPlan{}, fmt.Errorf("%s refused: sandbox %q needs the command's executable path, and it did not resolve", tool, name)
		}

		return sandboxPlan{Sandbox: sb}, nil

	case model.SandboxNested:
		switch sb.Seatbelt.AlreadySandboxed {
		case approve.AlreadyRun:
			return sandboxPlan{Sandbox: sb, Skip: skipByRule}, nil
		case approve.AlreadyAsk:
			return sandboxPlan{Sandbox: sb, Ask: true}, nil
		}

		return sandboxPlan{}, fmt.Errorf("%s refused: sandbox %q cannot be applied because cg is already inside another sandbox, which Seatbelt cannot nest (the profile check at load reported: %s). "+
			"already_sandboxed is %s for it; set already_sandboxed to run or ask in %s to run the command without the profile",
			tool, name, sb.Seatbelt.ProbeStderr, sb.Seatbelt.AlreadySandboxed, sb.File)
	}

	return sandboxPlan{}, fmt.Errorf("%s refused: sandbox %q cannot be applied: the profile check at load classified it as %s: %s",
		tool, name, sb.Seatbelt.Class, sb.Seatbelt.ProbeStderr)
}

// nestedFailClosedError refuses commands that wait on a prompt the client cannot show. It points at
// the sandbox's own setting. It never recommends --blindly-allow, which still prompts for these.
func nestedFailClosedError(tool string, asks []int, plans []sandboxPlan) error {
	var names []string
	for _, i := range asks {
		if name := plans[i].Sandbox.Name; !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	return fmt.Errorf("%s refused: sandbox %s cannot be applied because cg is already inside another sandbox, already_sandboxed is ask for it, and the client cannot prompt for approval; "+
		"set already_sandboxed to run or refuse for it in %s",
		tool, quotedList(names), plans[asks[0]].Sandbox.File)
}

func quotedList(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = fmt.Sprintf("%q", name)
	}

	return strings.Join(quoted, ", ")
}

// pinnedPaths works out, for each judged command, the inner executables a wrapper must be
// handed. The top layer is never pinned, since the supervisor already runs the path the gate
// resolved.
//
// An inner layer is pinned to the path it resolved to when every rule of the set that settled it is
// path-form. On a peeled layer that set is the chosen wrapper rules. A bare-name rule accepts any
// binary of that name, so a layer it settles keeps the wrapper's own lookup.
//
// A mixed-form tie is unpinned. So is a tie of a chain and an ordinary allow, unless the ordinary
// rules and the wrapper rules are all path-form.
//
// A layer still prompting was approved at the form, which showed its resolved path, so that layer
// is pinned to the path shown.
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
//
// Under --blindly-allow only the allow tier ranks, since the mode skips every other check. The plan
// is what the deciding rule's sandbox asks of the run.
func (g *gate) evaluate(call approvalCall, i int) (approve.Verdict, sandboxPlan, error) {
	resolved := call.Resolutions[i]
	subject := approve.Subject{Argv: call.Argvs[i], Canonical: resolved.CanonicalArgv(), Resolved: resolved.ResolvedArgv()}

	if g.blindlyAllow {
		v := g.store.Ruleset().EvaluateAllowAll(subject, approve.CommandResolver(call.Cwd))
		if v.Decision == approve.DecisionRefuse {
			return v, sandboxPlan{}, refusalError(call.Tool, v)
		}

		plan, err := g.sandboxFor(call.Tool, v, resolved)

		return v, plan, err
	}

	v := g.store.Ruleset().Evaluate(subject, call.Env, approve.CommandResolver(call.Cwd))
	if v.Decision != approve.DecisionRefuse {
		plan, err := g.sandboxFor(call.Tool, v, resolved)

		return v, plan, err
	}
	if len(v.BadEnvs) > 0 {
		return v, sandboxPlan{}, envRefusalError(call.Tool, v, g.store.Project.Path)
	}

	return v, sandboxPlan{}, refusalError(call.Tool, v)
}

// refusalError builds the error for a deny or restrict match, naming the rule
// kind and appending the rule's message when set so the agent sees why the
// command was blocked. A refusal behind a wrapper rule names the layer that refused and
// the rule that refused it. Equally specific rules all decide, so a refusal names every one
// of them with its message. A refusal from equally specific allow rules that disagree names
// those rules.
func refusalError(tool string, v approve.Verdict) error {
	if refusalStep(v) == approve.StepTieRefusal {
		return fmt.Errorf("%s refused: %s matches equally specific allow rules that disagree: %s",
			tool, refusedName(v), ruleLabels("allow", v.Deciding))
	}

	kind := "deny"
	if v.Restricted {
		kind = "restrict"
	}

	what := fmt.Sprintf("command matches a %s rule", kind)
	if layer, ok := refusingLayer(v); ok {
		what = fmt.Sprintf("%s matches %s", layerName(layer), ruleLabel(kind, v.Rule))
	}

	if len(v.Deciding) > 1 {
		parts := make([]string, len(v.Deciding))
		for i, rule := range v.Deciding {
			parts[i] = ruleLabel(kind, rule)
			if rule.Message != "" {
				parts[i] += ": " + rule.Message
			}
		}

		return fmt.Errorf("%s refused: %s matches %d equally specific %s rules; %s",
			tool, refusedName(v), len(v.Deciding), kind, strings.Join(parts, "; "))
	}

	if v.Rule != nil && v.Rule.Message != "" {
		return fmt.Errorf("%s refused: %s: %s", tool, what, v.Rule.Message)
	}
	return fmt.Errorf("%s refused: %s", tool, what)
}

// refusalStep is the step that settled the layer the refusal came from.
func refusalStep(v approve.Verdict) approve.Step {
	if layer, ok := refusingLayer(v); ok {
		return layer.Step
	}

	return v.Step
}

// refusedName names what a refusal is about: the typed command, or the inner command of the layer
// the chain refused at.
func refusedName(v approve.Verdict) string {
	if layer, ok := refusingLayer(v); ok {
		return layerName(layer)
	}

	return "the command"
}

// ruleLabels names several rules with the list they came from.
func ruleLabels(list string, rules []*approve.Rule) string {
	parts := make([]string, len(rules))
	for i, rule := range rules {
		parts[i] = ruleLabel(list, rule)
	}

	return strings.Join(parts, ", ")
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

// multiRoundAvailable reports whether this request's client takes more than one input-required
// round. The SDK reads the version from the request's _meta from protocol 2026-07-28 on, and from
// the initialize handshake before that.
//
// A client that sent neither is treated as the newest, as the SDK's own middleware does. Sending
// it down the one-round branch would only strip it of the second round.
func multiRoundAvailable(req *mcpsdk.CallToolRequest) bool {
	if req == nil {
		return false
	}

	version := req.ProtocolVersion()

	return version == "" || version >= multiRoundTripVersion
}
