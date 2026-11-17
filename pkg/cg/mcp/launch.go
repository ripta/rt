package mcp

import (
	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
)

// launch is how one approved command is handed to a supervisor: the argv to exec, the resolution it
// execs, the inner pins, the sandbox record, and the environment the command's own sandbox adds.
type launch struct {
	Argv      []string
	Resolved  *model.Resolution
	InnerExec map[int]string
	Sandbox   *model.SandboxSpec

	// Env is the run's environment overrides. For a sandboxed command it holds the sandbox's proxy
	// variables below the caller's own, so a caller's env wins.
	Env map[string]string
}

// newLaunch encodes a command for the supervisor. An unsandboxed command passes through as typed.
//
// A command whose profile is skipped, because cg is inside an outer sandbox, also passes through as
// typed. It sets none of the sandbox's proxy variables, and its spec only records the sandbox's name
// and why the profile was not applied. A supervisor that does not know the field runs it as typed.
//
// A sandboxed command is encoded into the spec fields every supervisor honors: sandbox-exec with the
// frozen profile text and the command's own resolved path. The inner pins are inlined, so the run
// never relies on the inner-exec pin. Only a supervisor that knows the sandbox field also records the
// typed command and runs the preflight.
func newLaunch(command []string, resolved *model.Resolution, inner map[int]string, env map[string]string, sb *approve.Sandbox, skipped string) launch {
	l := launch{Argv: command, Resolved: resolved, InnerExec: inner, Env: env}

	if skipped != "" && sb != nil {
		l.Sandbox = &model.SandboxSpec{Name: sb.Name, Command: command, Class: sb.Seatbelt.Class, Skipped: skipped}

		return l
	}

	spec, ok := sb.Spec(command, resolved)
	if !ok {
		return l
	}

	l.Argv, l.Resolved = model.SandboxLaunch(spec, inner)
	l.InnerExec = nil
	l.Sandbox = spec

	if proxy := sb.Seatbelt.Proxy; proxy != "" {
		l.Env = model.LayerEnv(model.ProxyEnv(proxy), env)
	}

	return l
}
