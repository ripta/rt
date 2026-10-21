package approve

import (
	"fmt"
	"path/filepath"
	"strings"
)

// WarningKind classifies a rule warning. Warnings never stop a file from loading.
type WarningKind int

const (
	// WarnDanglingFlag marks a wrapper rule that ends on a bare flag, which may be
	// waiting for a value the wrapper then reads from the inner command.
	WarnDanglingFlag WarningKind = iota
	// WarnBreaksContract marks a wrapper rule on a program from the built-in list.
	WarnBreaksContract
	// WarnDashFirstToken marks a deny or restrict rule whose first token starts with '-'.
	WarnDashFirstToken
	// WarnCoveredByAllow marks a wrapper rule that a prefix allow makes pointless.
	WarnCoveredByAllow
	// WarnDeadByDeny marks a wrapper rule that a deny rule makes dead.
	WarnDeadByDeny
)

// Warning is one non-fatal finding about a rule or a pair of rules.
type Warning struct {
	Kind    WarningKind
	Message string
}

// Warnings reports every warning for the merged ruleset: the findings about single
// rules, then the findings that need the merged view. Built-in deny rules take
// part in both.
func (rs *Ruleset) Warnings() []Warning {
	out := ruleWarnings("deny", rs.Deny)
	out = append(out, ruleWarnings("allow", rs.Allow)...)
	out = append(out, ruleWarnings("restrict", rs.Restrict)...)

	return append(out, rs.coverageWarnings()...)
}

// Warnings reports the warnings of the live ruleset, which includes rules a
// remember added since load.
func (s *Store) Warnings() []Warning {
	return s.Ruleset().Warnings()
}

// documentWarnings reports the findings about single rules in one layer. They
// need no merged view, so a layer can be checked alone.
func documentWarnings(doc *Document) []Warning {
	if doc == nil {
		return nil
	}

	out := ruleWarnings("deny", doc.Deny)
	out = append(out, ruleWarnings("allow", doc.Allow)...)

	return append(out, ruleWarnings("restrict", doc.Restrict)...)
}

// ruleWarnings reports the findings about single rules in one section. Allow rules skip the
// dash check because the loader already rejects them.
func ruleWarnings(section string, rules []Rule) []Warning {
	var out []Warning

	for i := range rules {
		rule := &rules[i]
		label := section + " " + describeRule(rule)

		switch {
		case rule.kind == KindWrapper:
			out = append(out, wrapperWarnings(rule, label)...)
		case section != "allow" && (rule.kind == KindExact || rule.kind == KindPrefix) && dashFirstToken(rule):
			out = append(out, Warning{
				Kind: WarnDashFirstToken,
				Message: fmt.Sprintf("%s starts with '-', so it can never match a command's program. "+
					"A command whose first token starts with '-' is never judged on its own.", label),
			})
		}
	}

	return out
}

// wrapperWarnings reports the findings that depend only on a wrapper rule's own tokens.
func wrapperWarnings(rule *Rule, label string) []Warning {
	var out []Warning

	tokens := rule.Wrapper
	if len(tokens) == 0 {
		return nil
	}

	if danglingFlag(tokens[len(tokens)-1]) {
		out = append(out, Warning{
			Kind: WarnDanglingFlag,
			Message: fmt.Sprintf("%s ends on a bare flag. If the flag takes a value, the wrapper reads the "+
				"inner command's first token as that value, and cg judges a different command than the one that "+
				"runs. A rule ending in a boolean flag, such as [caffeinate, -i], also warns. Appending -- "+
				"changes what the rule matches, so it does not silence this warning.", label),
		})
	}

	if entry := listedEntry(newForm(tokens[0], tokens[1:])); entry != nil {
		out = append(out, Warning{
			Kind: WarnBreaksContract,
			Message: fmt.Sprintf("%s has no effect: %s does not re-execute its arguments as a command, so the "+
				"rule never peels. An exact rule on the same program still works.",
				label, strings.Join(entry, " ")),
		})
	}

	return out
}

// danglingFlag reports whether a wrapper rule's last token is a bare flag: exactly
// -X for one character other than '-', or --word with no '='. A flag that carries
// its value, such as -n19 or --user=bob, and the -- separator are not bare.
func danglingFlag(token string) bool {
	switch {
	case len(token) == 2:
		return token[0] == '-' && token[1] != '-'
	case strings.HasPrefix(token, "--"):
		return len(token) > 2 && !strings.Contains(token, "=")
	}

	return false
}

// coverageWarnings reports the wrapper rules that a prefix allow or a prefix deny
// covers. Only these findings need the merged view.
func (rs *Ruleset) coverageWarnings() []Warning {
	var out []Warning

	for i := range rs.Allow {
		wrapper := &rs.Allow[i]
		if wrapper.kind != KindWrapper {
			continue
		}

		for j := range rs.Allow {
			if rs.Allow[j].kind == KindPrefix && tokensCover(&rs.Allow[j], wrapper) {
				out = append(out, Warning{
					Kind: WarnCoveredByAllow,
					Message: fmt.Sprintf("allow %s already allows every command %s matches, "+
						"without judging the inner command, so the wrapper rule has no effect. Narrow the "+
						"prefix rule or remove it.", describeRule(&rs.Allow[j]), "allow "+describeRule(wrapper)),
				})
			}
		}

		for j := range rs.Deny {
			if rs.Deny[j].kind == KindPrefix && tokensCover(&rs.Deny[j], wrapper) {
				out = append(out, Warning{
					Kind: WarnDeadByDeny,
					Message: fmt.Sprintf("deny %s refuses every command %s matches, so the "+
						"wrapper rule is dead.", describeRule(&rs.Deny[j]), "allow "+describeRule(wrapper)),
				})
			}
		}
	}

	return out
}

// tokensCover reports whether the cover rule's tokens are a leading prefix of the
// wrapper rule's tokens, equal length included. The first token compares by basename
// when the two differ in shape, because remember writes path-form rules and people
// write bare-name wrapper rules. The rest compare byte-exact.
func tokensCover(cover, wrapper *Rule) bool {
	c, w := compareTokens(cover), compareTokens(wrapper)
	if len(c) == 0 || len(c) > len(w) {
		return false
	}

	if hasPathSeparator(c[0]) == hasPathSeparator(w[0]) {
		if c[0] != w[0] {
			return false
		}
	} else if filepath.Base(c[0]) != filepath.Base(w[0]) {
		return false
	}

	for i := 1; i < len(c); i++ {
		if c[i] != w[i] {
			return false
		}
	}

	return true
}

// compareTokens returns the tokens a rule matches by: the load-resolved ones when the
// rule was compiled, otherwise the tokens as written.
func compareTokens(rule *Rule) []string {
	if rule.cmpArgv != nil {
		return rule.cmpArgv
	}

	return tokensOf(rule)
}

// describeRule renders a rule as its kind and pattern, such as `wrapper [nice, -n, 19]`.
func describeRule(rule *Rule) string {
	pattern := rule.Pattern()
	if pattern == "" {
		return "rule"
	}

	return rule.kind.String() + " " + pattern
}

// Pattern renders the pattern side of a rule: the argv tokens for exact, prefix, and wrapper
// rules, and the quoted pattern string for glob and regex rules.
func (r *Rule) Pattern() string {
	switch r.kind {
	case KindExact:
		return "[" + strings.Join(r.Exact, ", ") + "]"
	case KindPrefix:
		return "[" + strings.Join(r.Prefix, ", ") + "]"
	case KindWrapper:
		return "[" + strings.Join(r.Wrapper, ", ") + "]"
	case KindGlob:
		return fmt.Sprintf("%q", r.Glob)
	case KindRegex:
		return fmt.Sprintf("%q", r.Regex)
	}

	return ""
}

// WarningMessages reduces warnings to their text, for the output formats that show only the text.
// It returns nil for no warnings.
func WarningMessages(warnings []Warning) []string {
	var out []string
	for _, w := range warnings {
		out = append(out, w.Message)
	}

	return out
}
