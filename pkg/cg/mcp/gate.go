package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/approve"
)

// elicitor is the subset of *mcpsdk.ServerSession the gate needs to prompt the
// user. A nil elicitor means the client cannot prompt, so an unmatched command
// fails closed. Narrowing to an interface lets tests drive the prompt path with
// a canned response.
type elicitor interface {
	Elicit(ctx context.Context, params *mcpsdk.ElicitParams) (*mcpsdk.ElicitResult, error)
}

// gate is the per-server approval gate consulted before cg_run execs a command.
// It holds the whole config store, not just the ruleset, so the interactive
// persistence path can reuse it. A nil gate bypasses every check; the server
// always builds a real gate, so nil only occurs in tests that exercise the
// non-gated paths.
type gate struct {
	store        *approve.Store
	blindlyAllow bool
}

// check evaluates the command against the gate and returns nil to permit
// execution or a refusal error. blindlyAllow (and a nil gate) bypasses matching
// and lets the env override pass through untouched, matching allow-all. A real
// allow rule additionally gates dangerous env overrides; allow-all (whose rule
// is nil) does not. A command that matches nothing prompts the user when el is
// available, and otherwise fails closed.
func (g *gate) check(ctx context.Context, in runInput, el elicitor) error {
	if g == nil || g.blindlyAllow {
		return nil
	}

	res := g.store.Ruleset().Match(in.Command)
	switch res.Decision {
	case approve.DecisionRun:
		if res.Rule != nil {
			if bad := res.Rule.DisallowedEnvs(in.Env); len(bad) > 0 {
				return fmt.Errorf("cg_run refused: env override sets %s, which the matching allow rule does not permit; list them under permit_unsafe_envs to allow", strings.Join(bad, ", "))
			}
		}
		return nil
	case approve.DecisionRefuse:
		return refusalError(res.Rule)
	default:
		return g.promptOrFailClosed(ctx, in, el)
	}
}

// promptOrFailClosed handles a command that matched neither allow nor deny. A
// dangerous env override is refused before prompting, because a prompted command
// has no rule to carry a permit_unsafe_envs exemption. With no elicitor the gate
// fails closed; otherwise it prompts for approval.
func (g *gate) promptOrFailClosed(ctx context.Context, in runInput, el elicitor) error {
	if bad := (&approve.Rule{}).DisallowedEnvs(in.Env); len(bad) > 0 {
		return fmt.Errorf("cg_run refused: env override sets %s, which a prompted command cannot permit; add an allow rule with permit_unsafe_envs to %s", strings.Join(bad, ", "), g.store.Project.Path)
	}
	if el == nil {
		return g.failClosedError()
	}

	return g.prompt(ctx, in, el)
}

// refusalError builds the error for a deny match, appending the rule's message
// when set so the agent sees why the command was blocked.
func refusalError(rule *approve.Rule) error {
	if rule != nil && rule.Message != "" {
		return fmt.Errorf("cg_run refused: command matches a deny rule: %s", rule.Message)
	}
	return fmt.Errorf("cg_run refused: command matches a deny rule")
}

// failClosedError builds the error for a command that matched neither allow nor
// deny when no interactive prompt is available. The message points at the ways
// to permit the command.
func (g *gate) failClosedError() error {
	return fmt.Errorf("cg_run refused: no rule matched and the client cannot prompt for approval; add an allow rule to %s or start cg mcp with --blindly-allow", g.store.Project.Path)
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
