package approve

import (
	"github.com/ripta/rt/pkg/cg/model"
)

// Suggestion is the allow rule an approval prompt pre-fills for a command.
//
// A command whose program heads a wrapper rule gets an exact rule over its whole argv.
// Editing an exact rule narrows it. Deleting tokens from a prefix rule would widen it into
// the over-broad rule the wrapper rule exists to avoid.
type Suggestion struct {
	Kind   RuleKind
	Tokens []string

	// HeadsWrapper reports that the command's program heads a wrapper rule, which is why the
	// suggestion is exact.
	HeadsWrapper bool

	// NearMiss is the wrapper rule that matched the command but did not peel, because the
	// inner command starts with '-'. It is nil otherwise.
	NearMiss *Rule
}

// Suggest derives the rule pre-filled into the prompt for argv, resolved as res. A program
// that heads a wrapper rule gets an exact rule whose first token is the path the rule pins.
// Any other command gets SuggestPrefix.
//
// "Heads a wrapper rule" reads the first token the way matching does: a bare token compares
// with the invoked basename, and a path token compares with the canonical or resolved path.
func (rs *Ruleset) Suggest(argv []string, res *model.Resolution) Suggestion {
	if len(argv) == 0 {
		return Suggestion{Kind: KindPrefix}
	}

	execPath := res.RulePath()
	canonical, resolved, basename := resolutionForms(argv, res)

	var heads bool
	var near *Rule

	for i := range rs.Allow {
		rule := &rs.Allow[i]
		if rule.kind != KindWrapper || len(rule.cmpArgv) == 0 || !headMatches(rule, canonical, resolved, basename) {
			continue
		}

		heads = true

		if !tokenRuleMatches(rule, canonical, resolved, basename) || !innerStartsWithDash(argv, len(rule.cmpArgv)) {
			continue
		}

		if near == nil || len(rule.cmpArgv) > len(near.cmpArgv) {
			near = rule
		}
	}

	if !heads {
		return Suggestion{Kind: KindPrefix, Tokens: SuggestPrefix(argv, execPath)}
	}

	head := execPath
	if head == "" {
		head = argv[0]
	}

	tokens := append([]string{head}, argv[1:]...)

	return Suggestion{Kind: KindExact, Tokens: tokens, HeadsWrapper: true, NearMiss: near}
}

// headMatches reports whether a wrapper rule's first token names the program in the forms.
func headMatches(rule *Rule, canonical, resolved, basename matchForm) bool {
	first := rule.cmpArgv[0]

	if rule.wantBasename {
		return basename.ok && basename.head == first
	}

	return (canonical.ok && canonical.head == first) || (resolved.ok && resolved.head == first)
}
