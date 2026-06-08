package mcp

import (
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/approve"
)

// gate is the per-server approval gate consulted before cg_run execs a command.
// It holds the whole config store, not just the ruleset, so the interactive
// persistence work can reuse it without rewiring. A nil gate bypasses every
// check; the server always builds a real gate, so nil only occurs in tests that
// exercise the non-gated paths.
type gate struct {
	store        *approve.Store
	blindlyAllow bool
}

// check evaluates the command against the gate and returns nil to permit
// execution or a refusal error. blindlyAllow (and a nil gate) bypasses matching
// and lets the env override pass through untouched, matching allow-all. A real
// allow rule additionally gates dangerous env overrides; allow-all (whose rule
// is nil) does not. A command that matches nothing fails closed: the interactive
// prompt is not yet wired, so there is no path to approval at runtime.
func (g *gate) check(in runInput, canElicit bool) error {
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
		return failClosedError(canElicit)
	}
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
// deny. With no interactive prompt available, the gate refuses; the message
// points at the ways to permit the command.
func failClosedError(canElicit bool) error {
	if canElicit {
		return fmt.Errorf("cg_run refused: no rule matched and interactive approval is not yet available; add an allow rule to .cg/approve.yaml or start cg mcp with --blindly-allow")
	}
	return fmt.Errorf("cg_run refused: no rule matched and the client cannot prompt for approval; add an allow rule to .cg/approve.yaml or start cg mcp with --blindly-allow")
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
