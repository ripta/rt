package mcp

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ripta/rt/pkg/cg/approve"
)

// editReason says why a remember edit cannot be saved as submitted. Every reason takes the same
// path: a second round on a client that takes one, and a warning on one that does not. A new
// refusal adds a reason here and a clause to editConstraints.
type editReason int

const (
	editUnparseable editReason = iota
	editMismatch
	editWrapperTrim
	editWrapperBox
	editCredential
)

// editFailure is a remember edit the gate refused to save.
type editFailure struct {
	Reason editReason

	// Parse is the error from reading the field, set for editUnparseable.
	Parse error

	// Tokens are the tokens the field parsed to, or nil when it did not parse.
	Tokens []string

	// ExtraArgs is the allow-extra-arguments box as submitted.
	ExtraArgs bool

	// Missed is the command the rule does not match, set for editMismatch.
	Missed approve.EditTarget

	// Credentials are the zero-based positions of the credential-shaped tokens, set for
	// editCredential.
	Credentials []int
}

// editConstraints is the set of refusals a form is under, which decides the edits its note offers.
// The note offers only the edits that pass every constraint.
type editConstraints struct {
	// ExactOnly means a program in the form re-executes its arguments, so only an exact,
	// full-length rule is safe.
	ExactOnly bool

	// Credentials are the positions of the credential-shaped tokens in the submitted rule. When
	// there are any, only unchecking remember, a hand-written rule, or a trim that drops them is
	// accepted. A box ticked on an unchanged field and a respelled first token do not help.
	Credentials []int
}

// extraArgs reports whether the allow-extra-arguments box was ticked. An absent field is unticked.
func extraArgs(content map[string]any) bool {
	v, ok := content[extraArgsField].(bool)

	return ok && v
}

// readRemember reads one form's remember answer. It returns the kind and tokens to save, or the
// failure that stops the save. The kind comes from comparing the edit with the commands. It is
// never taken from what the form suggested.
//
// A field that does not parse is a failed edit like one that does not match, so both ask again.
func (g *gate) readRemember(content map[string]any, suggestion []string, targets []approve.EditTarget) (approve.RuleKind, []string, *editFailure) {
	extra := extraArgs(content)

	tokens, err := parseRuleField(content, suggestion)
	if err != nil {
		return 0, nil, &editFailure{Reason: editUnparseable, Parse: scrubParse(content, err), ExtraArgs: extra}
	}

	if positions := approve.CredentialPositions(tokens); len(positions) > 0 {
		return 0, nil, &editFailure{Reason: editCredential, Tokens: tokens, ExtraArgs: extra, Credentials: positions}
	}

	kind, rejection := g.store.CheckEdit(tokens, extra, targets)
	if rejection == nil {
		return kind, tokens, nil
	}

	failure := &editFailure{Tokens: tokens, ExtraArgs: extra}
	switch rejection.Reason {
	case approve.EditMismatch:
		failure.Reason = editMismatch
		failure.Missed = targets[rejection.Target]
	case approve.EditWrapperTrim:
		failure.Reason = editWrapperTrim
	case approve.EditWrapperBox:
		failure.Reason = editWrapperBox
	}

	return 0, nil, failure
}

// scrubParse drops the parser's detail when the field holds a credential-shaped piece. The YAML
// errors quote the offending text, and the note must not echo a credential.
func scrubParse(content map[string]any, err error) error {
	raw, _ := content["rule"].(string)
	if !approve.CredentialText(raw) {
		return err
	}

	return errors.New("rule must be a YAML list of tokens like [go, test]")
}

// constraintsOf works out the refusals a form is under. A rejection for a wrapper reason proves
// the form is exact-only even when the edit changed the first token to a wrapper head.
func (g *gate) constraintsOf(failure *editFailure, targets []approve.EditTarget) editConstraints {
	wrapper := failure.Reason == editWrapperTrim || failure.Reason == editWrapperBox

	exact := wrapper || g.store.FormHeadsWrapper(targets) || (len(failure.Tokens) > 0 && g.store.CollidesWithWrapper(failure.Tokens[0]))

	return editConstraints{ExactOnly: exact, Credentials: failure.Credentials}
}

// editNote says why an edit was not saved and which edits would be. The sentences are the same in
// the second-round form and in the legacy warning.
func (g *gate) editNote(failure *editFailure, targets []approve.EditTarget) string {
	var why string

	// readRemember reports editCredential before any other reason, so the rule tokens below hold
	// no credential today. They are redacted anyway, so a reorder cannot make a note echo one.
	switch failure.Reason {
	case editUnparseable:
		why = fmt.Sprintf("The rule field was not read: %v.", failure.Parse)
	case editMismatch:
		why = fmt.Sprintf("The rule %s does not match the command %s.", renderFlowSeq(approve.RedactCredentials(failure.Tokens)), describeCommand(failure.Missed.Argv))
	case editWrapperTrim:
		why = fmt.Sprintf("The rule %s is shorter than the command. This program re-executes its arguments, so a shorter rule would allow any inner command.", renderFlowSeq(approve.RedactCredentials(failure.Tokens)))
	case editWrapperBox:
		why = "Allow extra arguments would save a prefix rule. This program re-executes its arguments, so a prefix rule would allow any inner command. Untick the box."
	case editCredential:
		why = fmt.Sprintf("The rule holds what looks like a credential at %s, so nothing was saved. The project file may be committed, and a pushed secret cannot be recalled.", approve.DescribePositions(failure.Credentials))
	}

	return why + " " + g.acceptedEdits(g.constraintsOf(failure, targets))
}

// acceptedEdits lists the edits that pass every constraint. It names the credential's position and
// never the token.
//
// With a credential, the list runs from the safest way out to the one that widens the rule. A
// respelled first token and the box cannot clear a credential, so they are not offered. A form that
// is exact-only refuses a trim as well, which leaves unchecking remember and a hand-written rule.
func (g *gate) acceptedEdits(c editConstraints) string {
	path := g.store.Project.Path

	if len(c.Credentials) > 0 {
		if c.ExactOnly {
			return fmt.Sprintf("Uncheck remember, or write the rule by hand in %s. This program re-executes its arguments, so a trimmed rule is refused too. To allow the whole family of inner commands, write a wrapper rule by hand.", path)
		}

		if c.Credentials[0] == 0 {
			return fmt.Sprintf("Uncheck remember, or write the rule by hand in %s. The first token is the credential, so no trim removes it.", path)
		}

		return fmt.Sprintf("Uncheck remember, or write the rule by hand in %s. Or trim the rule to the tokens before %s. A trim allows any command that starts with those tokens, so it widens the rule.", path, approve.DescribePositions(c.Credentials[:1]))
	}

	if c.ExactOnly {
		return fmt.Sprintf("The accepted edits are respelling the first token and unchecking remember. To allow the whole family of inner commands, write a wrapper rule in %s by hand.", g.store.Project.Path)
	}

	return "The accepted edits are leaving the rule unchanged, ticking allow extra arguments, trimming it to a leading part, respelling the first token as a bare name or a path to the same program, and unchecking remember."
}

// describeCommand renders a command for a note. It is the one place a note prints a command. A
// credential-shaped argument is replaced, since the note reaches the agent and the tool result.
func describeCommand(argv []string) string {
	return "`" + visibleArgs(approve.RedactCredentials(argv)) + "`"
}

// prefillNote says so when the pre-filled rule cannot pass the check as it stands. That happens
// when the executable has no canonical path, since the path forms then offer nothing to match. The
// note names the bare-name edit that does match.
func (g *gate) prefillNote(suggestion approve.Suggestion, targets []approve.EditTarget) string {
	if len(suggestion.Tokens) == 0 || len(targets) == 0 {
		return ""
	}

	if _, rejection := g.store.CheckEdit(suggestion.Tokens, false, targets); rejection == nil {
		return ""
	}

	bare := append([]string{filepath.Base(targets[0].Argv[0])}, suggestion.Tokens[1:]...)
	if _, rejection := g.store.CheckEdit(bare, false, targets); rejection != nil {
		return "The pre-filled rule matches nothing here, and no first token matches every command in this form. Remember will ask again."
	}

	return fmt.Sprintf("The pre-filled rule names %s, which cannot match because this executable has no canonical path. Edit the first token to %s to remember it.", visibleText(suggestion.Tokens[0]), visibleText(bare[0]))
}

// formTargets lists the commands a form covers, each as the prompt asked about it. For a wrapped
// command that is the inner layer the first round peeled, with the resolution recorded for it.
func (e *pendingEntry) formTargets(call approvalCall, members []int) []approve.EditTarget {
	targets := make([]approve.EditTarget, len(members))

	for j, i := range members {
		cmd := e.Commands[i]
		targets[j] = approve.EditTarget{Argv: call.Argvs[i], Resolution: cmd.Resolution}

		if cmd.PromptOffset == 0 {
			continue
		}

		for _, layer := range cmd.Chain {
			if layer.Offset == cmd.PromptOffset {
				targets[j] = approve.EditTarget{Argv: call.Argvs[i][layer.Offset:], Resolution: layer.Resolution}
			}
		}
	}

	return targets
}
