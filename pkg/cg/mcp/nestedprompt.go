package mcp

import (
	"errors"
	"fmt"
	"slices"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/approve"
)

// nestedForm is one prompt asking whether to run commands without their sandbox's profile, because
// cg is inside an outer sandbox and the sandbox says already_sandboxed: ask. All its commands share
// the sandbox. The prompt has no rule and no remember, since the answer says nothing about a rule.
type nestedForm struct {
	Sandbox  string
	Commands []int
}

// planNestedForms groups the commands that wait on a sandbox prompt by sandbox. Groups come in the
// order their first command appears, and a group longer than maxFormCommands splits into consecutive
// forms, as rule forms do.
func planNestedForms(asks []int, plans []sandboxPlan) []nestedForm {
	var groups []nestedForm

	for _, i := range asks {
		name := plans[i].Sandbox.Name

		at := slices.IndexFunc(groups, func(g nestedForm) bool { return g.Sandbox == name })
		if at < 0 {
			at = len(groups)
			groups = append(groups, nestedForm{Sandbox: name})
		}

		groups[at].Commands = append(groups[at].Commands, i)
	}

	var forms []nestedForm
	for _, group := range groups {
		for chunk := range slices.Chunk(group.Commands, maxFormCommands) {
			forms = append(forms, nestedForm{Sandbox: group.Sandbox, Commands: chunk})
		}
	}

	return forms
}

// nestedMessage renders a sandbox prompt: a header, then each command on its own line, so the
// client's visible lines show them. A call with several forms leads the header with its position.
func nestedMessage(sandbox string, argvs [][]string, cwd string, pos, total int) string {
	if cwd == "" {
		cwd = "(server cwd)"
	}

	what := "this command"
	if len(argvs) != 1 {
		what = fmt.Sprintf("%d commands", len(argvs))
	}

	lead := "Run"
	if total > 1 {
		lead = fmt.Sprintf("Approval %d of %d: run", pos, total)
	}

	msg := fmt.Sprintf("%s %s without sandbox %q in %s? cg is already inside another sandbox, so it cannot apply the profile.", lead, what, sandbox, visibleText(cwd))
	for _, argv := range argvs {
		msg += "\n" + visibleArgs(argv)
	}

	return msg
}

// nestedSchema is the form's schema. It has no fields, so the user can only accept or decline.
func nestedSchema() map[string]any {
	return obj(map[string]any{})
}

// entryNestedForms lists the sandbox form ids of a pending entry in form order, with the commands
// each one covered.
func entryNestedForms(entry *pendingEntry) ([]string, map[string][]int) {
	members := make(map[string][]int)
	for i, cmd := range entry.Commands {
		if cmd.NestedForm != "" {
			members[cmd.NestedForm] = append(members[cmd.NestedForm], i)
		}
	}

	ids := make([]string, 0, len(members))
	for id := range members {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	return ids, members
}

// declinedAsk is a command whose sandbox prompt was declined.
type declinedAsk struct {
	Argv    []string
	Sandbox string
}

// nestedDeclinedError refuses the call for every command whose sandbox prompt was declined. A pool
// names what was declined, so the user can tell which form refused.
func nestedDeclinedError(tool string, declined []declinedAsk, pool bool) error {
	msg := fmt.Sprintf("%s refused: running without the sandbox was declined at the approval prompt", tool)
	if len(declined) == 1 && !pool {
		msg = fmt.Sprintf("%s refused: running without sandbox %q was declined at the approval prompt", tool, declined[0].Sandbox)
	} else {
		for _, d := range declined {
			msg += fmt.Sprintf("\n  %s (sandbox %q)", visibleArgs(d.Argv), d.Sandbox)
		}
	}

	return errors.New(msg)
}

// readNested reads the answers to the sandbox forms of a retry. It returns the commands the user
// agreed to run without their profile and those whose prompt was declined or cancelled. A missing
// or mistyped answer is an error, which the caller reports once every form has been read.
//
// A command the live rules no longer ask about gates nothing, and its answer is not read.
func readNested(call approvalCall, entry *pendingEntry, asks []int, plans []sandboxPlan) (approved []int, declined []declinedAsk, err error) {
	asking := func(ids []int) []int {
		return slices.DeleteFunc(slices.Clone(ids), func(i int) bool { return !slices.Contains(asks, i) })
	}

	if entry.Carry != nil {
		return slices.DeleteFunc(slices.Clone(entry.Carry.Approved), func(i int) bool { return !slices.Contains(asks, i) }), nil, nil
	}

	ids, members := entryNestedForms(entry)
	for _, id := range ids {
		cmds := asking(members[id])
		if len(cmds) == 0 {
			continue
		}

		res, answerErr := elicitAnswer(call.Responses, id)
		if answerErr != nil {
			if err == nil {
				err = answerErr
			}

			continue
		}

		if res.Action != actionAccept {
			for _, i := range cmds {
				declined = append(declined, declinedAsk{Argv: call.Argvs[i], Sandbox: plans[i].Sandbox.Name})
			}

			continue
		}

		approved = append(approved, cmds...)
	}

	return approved, declined, err
}

// staleNested reports why a sandbox prompt no longer describes a command, or nil when it does. The
// retry must not run a command the user was not asked about, or that now resolves elsewhere.
func staleNested(call approvalCall, entry *pendingEntry, i int, plan sandboxPlan, resolve approve.Resolver) *pendingRefusal {
	cmd := entry.Commands[i]

	if cmd.NestedForm == "" || cmd.NestedSandbox != plan.Sandbox.Name {
		return &pendingRefusal{msg: fmt.Sprintf("%s refused: the rules changed and this command was not part of the approval that was requested; %s", call.Tool, pendingAdvice)}
	}
	if resolutionChanged(cmd.Resolution, call.Resolutions[i]) {
		return &pendingRefusal{msg: fmt.Sprintf("%s refused: the command now resolves to a different executable than the one that was shown; %s", call.Tool, pendingAdvice)}
	}

	for _, layer := range cmd.Chain {
		inner := call.Argvs[i][layer.Offset:]
		if resolutionChanged(layer.Resolution, resolve(inner)) {
			return &pendingRefusal{msg: fmt.Sprintf("%s refused: the inner command `%s` now resolves to a different executable than the one that was shown; %s", call.Tool, visibleArgs(inner), pendingAdvice)}
		}
	}

	return nil
}

// nestedRequests builds the sandbox prompts for a first round. They follow the rule forms, so form
// positions run across both kinds, and total counts both. Each command a form covers is recorded in
// commands.
func nestedRequests(call approvalCall, plan []nestedForm, commands []pendingCommand, ruleForms, total int) mcpsdk.InputRequestMap {
	forms := make(mcpsdk.InputRequestMap, len(plan))

	for k, form := range plan {
		id := approvalID(ruleForms+k, total)

		argvs := make([][]string, len(form.Commands))
		for j, i := range form.Commands {
			argvs[j] = call.Argvs[i]
			commands[i].NestedForm = id
			commands[i].NestedSandbox = form.Sandbox
		}

		forms[id] = &mcpsdk.ElicitParams{
			Message:         nestedMessage(form.Sandbox, argvs, call.Cwd, ruleForms+k+1, total),
			RequestedSchema: nestedSchema(),
		}
	}

	return forms
}
