package approve

import (
	"path/filepath"
	"strings"

	"github.com/ripta/rt/pkg/cg"
)

// Match evaluates argv against the frozen ruleset and returns the verdict. mode
// allow-all and deny-all short-circuit; otherwise the first matching deny rule
// refuses, the first matching allow rule runs, and no match prompts. Deny is
// evaluated in full before allow, so a deny match always wins, including across
// layers.
func (rs *Ruleset) Match(argv []string) MatchResult {
	if len(argv) == 0 {
		return MatchResult{Decision: DecisionRefuse}
	}

	switch rs.Mode {
	case ModeAllowAll:
		return MatchResult{Decision: DecisionRun}
	case ModeDenyAll:
		return MatchResult{Decision: DecisionRefuse}
	}

	quoted := cg.EscapeArgs(argv)

	for i := range rs.Deny {
		if ruleMatches(&rs.Deny[i], argv, quoted, true) {
			return MatchResult{Decision: DecisionRefuse, Rule: &rs.Deny[i]}
		}
	}
	for i := range rs.Allow {
		if ruleMatches(&rs.Allow[i], argv, quoted, false) {
			return MatchResult{Decision: DecisionRun, Rule: &rs.Allow[i]}
		}
	}

	return MatchResult{Decision: DecisionPrompt}
}

// ruleMatches reports whether a single rule matches the command. exact and
// prefix compare argv tokens; glob and regex match the precomputed quoted join.
func ruleMatches(rule *Rule, argv []string, quoted string, isDeny bool) bool {
	switch rule.kind {
	case KindExact:
		return matchTokens(rule.Exact, argv, true, isDeny)
	case KindPrefix:
		return matchTokens(rule.Prefix, argv, false, isDeny)
	case KindGlob, KindRegex:
		return rule.compiled != nil && rule.compiled.MatchString(quoted)
	}

	return false
}

// matchTokens compares rule tokens against argv element-wise. exact requires
// equal length; prefix requires argv to be at least as long as the rule. argv[0]
// uses the asymmetric program-token normalization; argv[1:] compare byte-exact.
func matchTokens(tokens, argv []string, exact, isDeny bool) bool {
	if len(tokens) == 0 {
		return false
	}
	if exact && len(argv) != len(tokens) {
		return false
	}
	if !exact && len(argv) < len(tokens) {
		return false
	}

	if !programTokenMatches(tokens[0], argv[0], isDeny) {
		return false
	}
	for i := 1; i < len(tokens); i++ {
		if tokens[i] != argv[i] {
			return false
		}
	}

	return true
}

// programTokenMatches applies the asymmetric program-token normalization to
// argv[0]. A slash-bearing rule token matches argv[0] literally. A no-slash rule
// token broadens a deny to basename(argv[0]) so [sh] catches sh, /bin/sh, and
// ./sh; on an allow it matches only when argv[0] itself has no slash, so a
// path-qualified program falls through to a prompt rather than being
// rubber-stamped.
func programTokenMatches(token, argv0 string, isDeny bool) bool {
	if strings.ContainsRune(token, '/') {
		return token == argv0
	}
	if isDeny {
		return filepath.Base(argv0) == token
	}
	if strings.ContainsRune(argv0, '/') {
		return false
	}

	return token == argv0
}
