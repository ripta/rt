package approve

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/ripta/rt/pkg/cg/model"
)

// matchForm is one argv representation a rule can match against. It holds the
// first token and a tail that shares the typed argv's backing array, so building a
// form costs nothing in proportion to the argv's length. A wrapper chain builds
// one set of forms per layer, and a copy per layer would make the chain quadratic
// in memory. ok is false for the canonical form when the subject could not be
// canonicalized, which makes non-basename rules skip it.
type matchForm struct {
	head string
	tail []string
	ok   bool
}

func newForm(head string, tail []string) matchForm {
	return matchForm{head: head, tail: tail, ok: true}
}

func (f matchForm) length() int { return 1 + len(f.tail) }

func (f matchForm) at(i int) string {
	if i == 0 {
		return f.head
	}

	return f.tail[i-1]
}

// quoted renders the form as the quoted join that glob and regex rules match. It
// is built on demand and never kept, since only pattern rules need it.
func (f matchForm) quoted() string {
	argv := make([]string, 0, f.length())
	argv = append(argv, f.head)
	argv = append(argv, f.tail...)

	return model.EscapeArgs(argv)
}

// Match evaluates a subject against the frozen ruleset with no environment
// override and no resolver, and returns the verdict as a MatchResult. It serves
// callers that need only the tier outcome. Evaluate is the full entry point.
//
// Mode allow-all and deny-all short-circuit; otherwise the tiers are consulted in
// order deny > allow > restrict > prompt: the first matching deny rule refuses,
// then a matching allow rule runs, then the first matching restrict rule refuses
// an in-scope command with no allow carve-out, and an unmatched command prompts.
// Deny is evaluated in full before allow, so a deny match always wins, and allow
// is evaluated before restrict, so an allow carves out of a restrict scope. Both
// relationships hold across layers.
//
// Each rule matches either the path forms or the basename form, decided at
// load by compileMatch from the rule's shape. A path rule matches the canonical
// form or the resolved form, so a rule can pin either side of a symlink: the
// file that ultimately runs, or the shim the user invoked, which for a
// multiplexer like rustup is the only path that names the tool. When the
// subject has no canonical form, path rules cannot match, so a command with an
// unknown executable identity is never allowed by path policy and falls through
// to prompt or fail-closed.
func (rs *Ruleset) Match(subj Subject) MatchResult {
	v := rs.Evaluate(subj, nil, nil)

	return MatchResult{Decision: v.Decision, Rule: v.Rule, Restricted: v.Restricted}
}

// forms builds the canonical, resolved, and basename match forms once per layer.
// The canonical form is unavailable when Canonical is nil. The resolved form is
// offered only when the canonical form is too, preserving the invariant that a
// command whose executable identity is unknown cannot match path rules; it is
// skipped as redundant when symlink evaluation changed nothing. The basename form
// replaces only Argv[0] with its basename, the invoked token, and leaves the tail
// intact.
func (s Subject) forms() (canonical, resolved, basename matchForm) {
	if s.Canonical != nil {
		canonical = newForm(s.Canonical[0], s.Canonical[1:])
		if s.Resolved != nil && s.Resolved[0] != s.Canonical[0] {
			resolved = newForm(s.Resolved[0], s.Resolved[1:])
		}
	}

	return canonical, resolved, newForm(filepath.Base(s.Argv[0]), s.Argv[1:])
}

// resolutionForms builds the same three forms for an inner layer from its
// resolution, sharing argv's tail instead of copying it into a Subject.
func resolutionForms(argv []string, res *model.Resolution) (canonical, resolved, basename matchForm) {
	tail := argv[1:]
	if res != nil && res.Canonical != "" {
		canonical = newForm(res.Canonical, tail)
		if res.Resolved != "" && res.Resolved != res.Canonical {
			resolved = newForm(res.Resolved, tail)
		}
	}

	return canonical, resolved, newForm(filepath.Base(argv[0]), tail)
}

// ruleMatches reports whether a single rule matches the subject as an ordinary
// rule. A wrapper rule never does: it acts only through a peel, so a scan that
// ran the full argv on a wrapper match would skip judging the inner command.
func ruleMatches(rule *Rule, canonical, resolved, basename matchForm) bool {
	if rule.kind == KindWrapper {
		return false
	}

	return tokenRuleMatches(rule, canonical, resolved, basename)
}

// tokenRuleMatches reports whether a rule's own comparison matches the subject.
// wantBasename, set at load by compileMatch, selects the basename form;
// otherwise the rule matches the canonical form or the resolved form, neither of
// which is available when canonicalization failed.
func tokenRuleMatches(rule *Rule, canonical, resolved, basename matchForm) bool {
	if rule.wantBasename {
		return formMatches(rule, basename)
	}

	return formMatches(rule, canonical) || formMatches(rule, resolved)
}

// formMatches evaluates one rule against one form. exact and prefix compare
// cmpArgv, the load-resolved comparison tokens; wrapper compares the same tokens
// as a prefix but needs at least one token after them; glob and regex match the
// precomputed quoted join.
func formMatches(rule *Rule, form matchForm) bool {
	if !form.ok {
		return false
	}

	switch rule.kind {
	case KindExact:
		return matchTokens(rule.cmpArgv, form, true)
	case KindPrefix:
		return matchTokens(rule.cmpArgv, form, false)
	case KindWrapper:
		return form.length() > len(rule.cmpArgv) && matchTokens(rule.cmpArgv, form, false)
	case KindGlob, KindRegex:
		return rule.compiled != nil && rule.compiled.MatchString(form.quoted())
	}

	return false
}

// compileMatch derives the match form and comparison tokens for a rule once at
// load, so the matcher itself reads only precomputed fields. For prefix, exact,
// and wrapper rules the form follows the first token's shape: a bare program name
// matches the invoked basename; an absolute path matches the canonical or resolved
// path as written; a relative path matches those paths after being resolved
// against projectRoot. For glob and regex rules there is no token to read, so
// as_basename selects the form. The relative token is joined and cleaned but not
// symlink evaluated, matching how an absolute token compares literally against the
// subject's path forms, and so the rule does not require the file to exist at
// load.
func compileMatch(rule *Rule, projectRoot string) {
	switch rule.kind {
	case KindGlob, KindRegex:
		rule.wantBasename = rule.AsBasename
	case KindExact, KindPrefix, KindWrapper:
		tokens := tokensOf(rule)
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

		rule.baseArgv = slices.Clone(cmp)
		rule.baseArgv[0] = filepath.Base(cmp[0])
	}
}

// hasPathSeparator reports whether s carries a path separator, which marks it as
// a path token rather than a bare program name.
func hasPathSeparator(s string) bool {
	return strings.ContainsRune(s, '/') || strings.ContainsRune(s, filepath.Separator)
}

// matchTokens compares rule tokens against a form element-wise. exact requires
// equal length; prefix requires the form to be at least as long as the rule. Every
// token, including the first, compares byte-exact.
func matchTokens(tokens []string, form matchForm, exact bool) bool {
	if len(tokens) == 0 {
		return false
	}
	if exact && form.length() != len(tokens) {
		return false
	}
	if !exact && form.length() < len(tokens) {
		return false
	}

	for i := range tokens {
		if tokens[i] != form.at(i) {
			return false
		}
	}

	return true
}
