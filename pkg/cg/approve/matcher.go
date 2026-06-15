package approve

import (
	"path/filepath"

	"github.com/ripta/rt/pkg/cg"
)

// matchForm is one argv representation a rule can match against: the token slice
// and its precomputed quoted join. ok is false for the canonical form when the
// subject could not be canonicalized, which makes non-basename rules skip it.
type matchForm struct {
	argv   []string
	quoted string
	ok     bool
}

// Match evaluates a subject against the frozen ruleset and returns the verdict.
// mode allow-all and deny-all short-circuit; otherwise the first matching deny
// rule refuses, the first matching allow rule runs, and no match prompts. Deny is
// evaluated in full before allow, so a deny match always wins, including across
// layers.
//
// Rules match the canonical form by default; a rule with AsBasename set matches
// the basename form instead. When the subject has no canonical form, non-basename
// rules cannot match, so a command with an unknown executable identity is never
// allowed by canonical policy and falls through to prompt or fail-closed.
func (rs *Ruleset) Match(subj Subject) MatchResult {
	if len(subj.Argv) == 0 {
		return MatchResult{Decision: DecisionRefuse}
	}

	switch rs.Mode {
	case ModeAllowAll:
		return MatchResult{Decision: DecisionRun}
	case ModeDenyAll:
		return MatchResult{Decision: DecisionRefuse}
	}

	canonical, basename := subj.forms()

	for i := range rs.Deny {
		if ruleMatches(&rs.Deny[i], canonical, basename) {
			return MatchResult{Decision: DecisionRefuse, Rule: &rs.Deny[i]}
		}
	}
	for i := range rs.Allow {
		if ruleMatches(&rs.Allow[i], canonical, basename) {
			return MatchResult{Decision: DecisionRun, Rule: &rs.Allow[i]}
		}
	}

	return MatchResult{Decision: DecisionPrompt}
}

// forms builds the canonical and basename match forms once per Match call. The
// canonical form is unavailable when Canonical is nil. The basename form replaces
// only Argv[0] with its basename, the invoked token, and leaves the tail intact.
func (s Subject) forms() (canonical, basename matchForm) {
	if s.Canonical != nil {
		canonical = matchForm{argv: s.Canonical, quoted: cg.EscapeArgs(s.Canonical), ok: true}
	}

	base := make([]string, len(s.Argv))
	copy(base, s.Argv)
	base[0] = filepath.Base(s.Argv[0])
	basename = matchForm{argv: base, quoted: cg.EscapeArgs(base), ok: true}

	return canonical, basename
}

// ruleMatches reports whether a single rule matches the subject. AsBasename
// selects the basename form; otherwise the rule matches the canonical form, which
// it cannot do when that form is unavailable. exact and prefix compare argv
// tokens; glob and regex match the precomputed quoted join.
func ruleMatches(rule *Rule, canonical, basename matchForm) bool {
	form := canonical
	if rule.AsBasename {
		form = basename
	}
	if !form.ok {
		return false
	}

	switch rule.kind {
	case KindExact:
		return matchTokens(rule.Exact, form.argv, true)
	case KindPrefix:
		return matchTokens(rule.Prefix, form.argv, false)
	case KindGlob, KindRegex:
		return rule.compiled != nil && rule.compiled.MatchString(form.quoted)
	}

	return false
}

// matchTokens compares rule tokens against argv element-wise. exact requires
// equal length; prefix requires argv to be at least as long as the rule. Every
// token, including argv[0], compares byte-exact.
func matchTokens(tokens, argv []string, exact bool) bool {
	if len(tokens) == 0 {
		return false
	}
	if exact && len(argv) != len(tokens) {
		return false
	}
	if !exact && len(argv) < len(tokens) {
		return false
	}

	for i := range tokens {
		if tokens[i] != argv[i] {
			return false
		}
	}

	return true
}
