package approve

import (
	"path/filepath"
	"slices"

	"github.com/ripta/rt/pkg/cg/model"
)

// EditTarget is one command a remembered rule must cover: the argv a prompt asked about, and
// the resolution of its executable. For a wrapped command that is the inner layer and its own
// resolution.
type EditTarget struct {
	Argv       []string
	Resolution *model.Resolution
}

// EditReason says why CheckEdit refused a submitted rule.
type EditReason int

const (
	// EditMismatch means the rule does not match a target, so saving it would write a rule that
	// misses the command the user approved.
	EditMismatch EditReason = iota

	// EditWrapperTrim means a form for a program that re-executes its arguments was trimmed. A
	// shorter rule would allow every inner command.
	EditWrapperTrim

	// EditWrapperBox means the allow-extra-arguments box was ticked for such a program.
	EditWrapperBox
)

// EditRejection is a refused edit. Target indexes the target the rule misses, and is 0 for the
// wrapper reasons, which concern the whole form.
type EditRejection struct {
	Reason EditReason
	Target int
}

// CheckEdit judges a rule the user submitted against the commands one form covers, and infers
// the kind the rule is saved as.
//
// The tokens compile the way the loader compiles a rule from the project file, and the compiled
// rule is tested against each target's match forms directly. It does not go through Match, whose
// mode and tier logic would answer a different question. A rule with as many tokens as the
// command is exact, or prefix when extraArgs is set. A shorter rule is prefix. A longer one
// matches nothing.
//
// A form whose program heads a wrapper rule, or whose submitted first token names such a
// program, accepts only the exact, full-length rule without the box.
func (s *Store) CheckEdit(tokens []string, extraArgs bool, targets []EditTarget) (RuleKind, *EditRejection) {
	if len(tokens) == 0 || len(targets) == 0 {
		return 0, &EditRejection{Reason: EditMismatch}
	}

	full := len(tokens) == len(targets[0].Argv)
	trimmed := len(tokens) < len(targets[0].Argv)

	kind := KindPrefix
	if full && !extraArgs {
		kind = KindExact
	}

	rule := Rule{kind: kind}
	if kind == KindExact {
		rule.Exact = slices.Clone(tokens)
	} else {
		rule.Prefix = slices.Clone(tokens)
	}
	compileMatch(&rule, s.projectRoot)

	for i, target := range targets {
		canonical, resolved, basename := resolutionForms(target.Argv, target.Resolution)
		if !tokenRuleMatches(&rule, canonical, resolved, basename) {
			return 0, &EditRejection{Reason: EditMismatch, Target: i}
		}
	}

	if !s.formHeadsWrapper(tokens[0], targets) {
		return kind, nil
	}

	switch {
	case trimmed:
		return 0, &EditRejection{Reason: EditWrapperTrim}
	case extraArgs:
		return 0, &EditRejection{Reason: EditWrapperBox}
	}

	return KindExact, nil
}

// FormHeadsWrapper reports whether a remembered rule for these targets is exact-only: a target's
// program heads a wrapper rule. A form that does is shown without the allow-extra-arguments box.
func (s *Store) FormHeadsWrapper(targets []EditTarget) bool {
	return s.formHeadsWrapper("", targets)
}

func (s *Store) formHeadsWrapper(first string, targets []EditTarget) bool {
	rs := s.Ruleset()
	for _, target := range targets {
		if rs.Suggest(target.Argv, target.Resolution).HeadsWrapper {
			return true
		}
	}

	return first != "" && s.CollidesWithWrapper(first)
}

// CollidesWithWrapper reports whether a submitted first token, read with its own token shape,
// names a program that heads a wrapper allow rule. A bare token collides with a head that equals
// it or whose basename equals it. A path token collides with a head that equals it or that equals
// its basename. A project-relative token or head is compared after joining the project root.
func (s *Store) CollidesWithWrapper(first string) bool {
	if first == "" {
		return false
	}

	isPath := hasPathSeparator(first)
	if isPath && !filepath.IsAbs(first) {
		first = filepath.Clean(filepath.Join(s.projectRoot, first))
	}

	for i := range s.Ruleset().Allow {
		rule := &s.Ruleset().Allow[i]
		if rule.kind != KindWrapper || len(rule.cmpArgv) == 0 {
			continue
		}

		head := rule.cmpArgv[0]
		if first == head {
			return true
		}

		if isPath && filepath.Base(first) == head {
			return true
		}
		if !isPath && first == filepath.Base(head) {
			return true
		}
	}

	return false
}
