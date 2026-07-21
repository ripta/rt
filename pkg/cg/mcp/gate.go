package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
)

// elicitor is the subset of *mcpsdk.ServerSession the gate needs to prompt the
// user. A nil elicitor means the client cannot prompt, so an unmatched command
// fails closed. Narrowing to an interface lets tests drive the prompt path
// with a canned response.
type elicitor interface {
	Elicit(ctx context.Context, params *mcpsdk.ElicitParams) (*mcpsdk.ElicitResult, error)
}

// gate is the per-server approval gate consulted before cg_run execs a
// command. It holds the whole config store, not just the ruleset, so the
// interactive persistence path can reuse it. A nil gate bypasses every check;
// the server always builds a real gate, so nil only occurs in tests that
// exercise the non-gated paths.
type gate struct {
	store        *approve.Store
	blindlyAllow bool
}

// check evaluates the command against the gate. It returns a nil error to
// permit execution or a refusal error to block it. The srting return is a
// best-effort persistence diagnostic to surface alongside a permitted run,
// empty when there is nothing to report. tool names the calling MCP tool in
// refusal messages.
func (g *gate) check(ctx context.Context, tool string, in runInput, resolved *model.Resolution, el elicitor) (string, error) {
	if g == nil || g.blindlyAllow {
		return "", nil
	}

	subject := approve.Subject{Argv: in.Command, Canonical: resolved.CanonicalArgv(), Resolved: resolved.ResolvedArgv()}
	v := g.store.Ruleset().Evaluate(subject, in.Env)
	switch v.Decision {
	case approve.DecisionRun:
		return "", nil
	case approve.DecisionRefuse:
		if len(v.BadEnvs) > 0 {
			return "", envRefusalError(tool, v, g.store.Project.Path)
		}
		return "", refusalError(tool, v)
	default:
		return g.promptOrFailClosed(ctx, tool, in, resolved, el)
	}
}

// promptOrFailClosed handles a command that matched neither allow nor deny.
// Evaluate already refuses a dangerous env override before returning
// DecisionPrompt, so reaching here means the env is clean. With no elicitor
// the gate fails closed; otherwise it prompts for approval. resolved carries
// the canonical executable path the prompt pre-fills as a strict rule.
func (g *gate) promptOrFailClosed(ctx context.Context, tool string, in runInput, resolved *model.Resolution, el elicitor) (string, error) {
	if el == nil {
		return "", g.failClosedError(tool)
	}

	return g.prompt(ctx, tool, in, resolved, el)
}

// refusalError builds the error for a deny or restrict match, naming the rule
// kind and appending the rule's message when set so the agent sees why the
// command was blocked.
func refusalError(tool string, v approve.Verdict) error {
	kind := "deny"
	if v.Restricted {
		kind = "restrict"
	}
	if v.Rule != nil && v.Rule.Message != "" {
		return fmt.Errorf("%s refused: command matches a %s rule: %s", tool, kind, v.Rule.Message)
	}
	return fmt.Errorf("%s refused: command matches a %s rule", tool, kind)
}

// envRefusalError builds the error for a DecisionRefuse produced by the
// dangerous-env gate rather than a deny or restrict rule. A matched allow rule
// (Rule non-nil) points at permit_unsafe_envs; an unmatched command (Rule nil)
// has no rule to carry an exemption, so the message points at adding one to
// the project file instead.
func envRefusalError(tool string, v approve.Verdict, projectPath string) error {
	bad := strings.Join(v.BadEnvs, ", ")
	if v.Rule != nil {
		return fmt.Errorf("%s refused: env override sets %s, which the matching allow rule does not permit; list them under permit_unsafe_envs to allow", tool, bad)
	}
	return fmt.Errorf("%s refused: env override sets %s, which a prompted command cannot permit; add an allow rule with permit_unsafe_envs to %s", tool, bad, projectPath)
}

// failClosedError builds the error for a command that matched neither allow
// nor deny when no interactive prompt is available. The message points at the
// ways to permit the command.
func (g *gate) failClosedError(tool string) error {
	return fmt.Errorf("%s refused: no rule matched and the client cannot prompt for approval; add an allow rule to %s or start cg mcp with --blindly-allow", tool, g.store.Project.Path)
}

// elicitationAvailable reports whether the connected client advertised the
// elicitation capability. It is nil-safe so handlers can call it on a request
// that has no session.
func elicitationAvailable(req *mcpsdk.CallToolRequest) bool {
	if req == nil || req.Session == nil {
		return false
	}
	params := req.Session.InitializeParams()
	return params != nil && params.Capabilities != nil && params.Capabilities.Elicitation != nil
}
