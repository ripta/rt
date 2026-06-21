package approve

import (
	"path/filepath"
	"slices"
	"strings"

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
// mode allow-all and deny-all short-circuit; otherwise the tiers are consulted in
// order deny > allow > restrict > prompt: the first matching deny rule refuses,
// then the first matching allow rule runs, then the first matching restrict rule
// refuses an in-scope command with no allow carve-out, and an unmatched command
// prompts. Deny is evaluated in full before allow, so a deny match always wins,
// and allow is evaluated before restrict, so an allow carves out of a restrict
// scope. Both relationships hold across layers.
//
// Each rule matches either the canonical form or the basename form, decided at
// load by compileMatch from the rule's shape. When the subject has no canonical
// form, canonical-form rules cannot match, so a command with an unknown
// executable identity is never allowed by canonical policy and falls through to
// prompt or fail-closed.
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
	for i := range rs.Restrict {
		if ruleMatches(&rs.Restrict[i], canonical, basename) {
			return MatchResult{Decision: DecisionRefuse, Rule: &rs.Restrict[i]}
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

// ruleMatches reports whether a single rule matches the subject. wantBasename,
// set at load by compileMatch, selects the basename form; otherwise the rule
// matches the canonical form, which it cannot do when that form is unavailable.
// exact and prefix compare cmpArgv, the load-resolved comparison tokens; glob and
// regex match the precomputed quoted join.
func ruleMatches(rule *Rule, canonical, basename matchForm) bool {
	form := canonical
	if rule.wantBasename {
		form = basename
	}
	if !form.ok {
		return false
	}

	switch rule.kind {
	case KindExact:
		return matchTokens(rule.cmpArgv, form.argv, true)
	case KindPrefix:
		return matchTokens(rule.cmpArgv, form.argv, false)
	case KindGlob, KindRegex:
		return rule.compiled != nil && rule.compiled.MatchString(form.quoted)
	}

	return false
}

// compileMatch derives the match form and comparison tokens for a rule once at
// load, so the matcher itself reads only precomputed fields. For prefix and exact
// rules the form follows the first token's shape: a bare program name matches the
// invoked basename; an absolute path matches the canonical path as written; a
// relative path matches the canonical path after being resolved against
// projectRoot. For glob and regex rules there is no token to read, so as_basename
// selects the form. The relative token is joined and cleaned but not symlink
// evaluated, matching how an absolute token compares literally against the
// subject's symlink-resolved canonical path, and so the rule does not require the
// file to exist at load.
func compileMatch(rule *Rule, projectRoot string) {
	switch rule.kind {
	case KindGlob, KindRegex:
		rule.wantBasename = rule.AsBasename
	case KindExact, KindPrefix:
		tokens := rule.Prefix
		if rule.kind == KindExact {
			tokens = rule.Exact
		}
		if len(tokens) == 0 {
			return
		}
		if !hasPathSeparator(tokens[0]) {
			rule.wantBasename = true
			rule.cmpArgv = tokens
			return
		}
		cmp := slices.Clone(tokens)
		if !filepath.IsAbs(cmp[0]) {
			cmp[0] = filepath.Clean(filepath.Join(projectRoot, cmp[0]))
		}
		rule.cmpArgv = cmp
	}
}

// hasPathSeparator reports whether s carries a path separator, which marks it as
// a path token rather than a bare program name.
func hasPathSeparator(s string) bool {
	return strings.ContainsRune(s, '/') || strings.ContainsRune(s, filepath.Separator)
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
