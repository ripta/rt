package approve

import (
	"cmp"
	"fmt"
	"regexp/syntax"
	"slices"
	"strings"

	"github.com/ripta/rt/pkg/cg/model"
)

// ruleLayer is the file a rule came from.
//
// A lower value is a more specific layer.
//
// The zero value is the project layer. A rule built without a layer ranks as a project rule, as a
// remembered rule does.
type ruleLayer int

const (
	layerProject ruleLayer = iota
	layerGlobal
	layerBuiltin
)

// rankShape orders the match kinds at equal count. A higher value is more specific.
type rankShape int

const (
	shapePrefix rankShape = iota
	shapePattern
	shapeExact
)

// rank is what the specificity ranking reads from a rule: the leading argv tokens it provably pins,
// its kind, its form, and its layer. A chain of wrapper rules and the rule it settles on has a rank
// of the same shape.
type rank struct {
	count int
	shape rankShape
	path  bool
	layer ruleLayer
}

// outranks reports whether a is more specific than b.
//
// A higher count outranks. At equal count, kind and form compare together. A rule that wins on one
// and ties on the other outranks. A rule that wins on one and loses on the other does not outrank,
// in either direction.
//
// When kind and form are both equal, the more specific layer outranks.
//
// The relation is a strict partial order. The rules no other rule outranks do not depend on the
// order they are compared in.
func (a rank) outranks(b rank) bool {
	if a.count != b.count {
		return a.count > b.count
	}

	k := cmp.Compare(a.shape, b.shape)
	f := 0
	switch {
	case a.path && !b.path:
		f = 1
	case !a.path && b.path:
		f = -1
	}

	switch {
	case k == 0 && f == 0:
		return a.layer < b.layer
	case k >= 0 && f >= 0:
		return true
	}

	return false
}

// ranking is the rank of a rule that matched. The form is the predicate pinning reads: the shape of
// a token rule's first token, or a pattern rule's as_basename.
func (r *Rule) ranking() rank {
	return rank{count: r.rankCount, shape: r.rankShape, path: !r.wantBasename, layer: r.layer}
}

// compileRank computes the count and shape of a rule once, at compile time.
func compileRank(rule *Rule) {
	switch rule.kind {
	case KindExact:
		rule.rankCount, rule.rankShape = len(rule.Exact), shapeExact
	case KindPrefix:
		rule.rankCount, rule.rankShape = len(rule.Prefix), shapePrefix
	case KindWrapper:
		rule.rankCount, rule.rankShape = len(rule.Wrapper), shapePrefix
	case KindGlob:
		rule.rankCount, rule.rankShape = patternRank(globToRegex(rule.Glob))
	case KindRegex:
		rule.rankCount, rule.rankShape = patternRank(rule.Regex)
	}
}

// patternRank reads the leading argv tokens a pattern pins from its parse. The count never
// overcounts. A token counts only when it ends at a literal space or at the end of the text on every
// path through the pattern. The count stops at a pattern not anchored at the start, a case-folded
// literal, a literal that needs shell quoting under model.EscapeArgs, and anything that is not a
// literal, an end anchor, or a boundary group.
//
// The shape is exact when the end of the text follows the counted tokens directly. It is prefix when
// a boundary group that accepts a space or the end of the text does. Those patterns match the same
// commands as the token rule. Any other constraint after the counted tokens makes it a pattern.
func patternRank(expr string) (int, rankShape) {
	re, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return 0, shapePattern
	}

	elems := flattenPattern(re, nil)
	if len(elems) == 0 || elems[0].Op != syntax.OpBeginText {
		return 0, shapePattern
	}

	count := 0
	inToken := false

	for i := 1; i < len(elems); i++ {
		e := elems[i]
		last := i == len(elems)-1

		switch e.Op {
		case syntax.OpLiteral:
			if e.Flags&syntax.FoldCase != 0 {
				return count, shapePattern
			}

			for _, r := range e.Rune {
				switch {
				case r == ' ' && inToken:
					count++
					inToken = false
				case r != ' ' && quotesFree(r):
					inToken = true
				default:
					return count, shapePattern
				}
			}
		case syntax.OpEndText:
			if !inToken {
				return count, shapePattern
			}
			if last {
				return count + 1, shapeExact
			}

			return count + 1, shapePattern
		case syntax.OpAlternate:
			if !inToken || !startsAtBoundary(e) {
				return count, shapePattern
			}
			if last && onlyBoundary(e) {
				return count + 1, shapePrefix
			}

			return count + 1, shapePattern
		default:
			return count, shapePattern
		}
	}

	return count, shapePattern
}

// flattenPattern lists the elements of a pattern in order, reading through concatenations and
// capture groups. A group that wraps an alternation stays one element.
func flattenPattern(re *syntax.Regexp, out []*syntax.Regexp) []*syntax.Regexp {
	switch re.Op {
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			out = flattenPattern(sub, out)
		}
	case syntax.OpCapture:
		out = flattenPattern(re.Sub[0], out)
	case syntax.OpEmptyMatch:
	default:
		out = append(out, re)
	}

	return out
}

// quotesFree reports whether a rune is written the same in a quoted argv join, so a literal made of
// such runes is the argument itself.
func quotesFree(r rune) bool {
	s := string(r)

	return model.EscapeArgs([]string{s}) == s
}

// startsAtBoundary reports whether every branch of an alternation begins with a literal space or
// the end of the text.
func startsAtBoundary(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpAlternate:
		for _, sub := range re.Sub {
			if !startsAtBoundary(sub) {
				return false
			}
		}

		return true
	case syntax.OpEndText:
		return true
	case syntax.OpLiteral:
		return len(re.Rune) > 0 && re.Rune[0] == ' '
	case syntax.OpConcat, syntax.OpCapture:
		return len(re.Sub) > 0 && startsAtBoundary(re.Sub[0])
	}

	return false
}

// onlyBoundary reports whether every branch of an alternation is exactly one space or the end of the
// text, so nothing else constrains the command.
func onlyBoundary(re *syntax.Regexp) bool {
	for _, sub := range re.Sub {
		switch {
		case sub.Op == syntax.OpEndText:
		case sub.Op == syntax.OpLiteral && len(sub.Rune) == 1 && sub.Rune[0] == ' ':
		default:
			return false
		}
	}

	return true
}

// ranked pairs a matching rule with the rank it competes at.
type ranked struct {
	rule *Rule
	rank rank
}

// decidingRules returns every item that no other item outranks, in whole-rule order. The result is
// the same for any order of items.
func decidingRules(items []ranked) []*Rule {
	var out []*Rule

	for i, it := range items {
		beaten := false
		for j, other := range items {
			if i != j && other.rank.outranks(it.rank) {
				beaten = true
				break
			}
		}

		if !beaten {
			out = append(out, it.rule)
		}
	}

	sortRules(out)

	return out
}

// sortRules orders rules by their whole text, so a choice among them never depends on list order.
func sortRules(rules []*Rule) {
	slices.SortStableFunc(rules, func(a, b *Rule) int {
		return strings.Compare(wholeRuleKey(a), wholeRuleKey(b))
	})
}

// wholeRuleKey renders every field of a rule that can tell two rules apart, so two rules with the
// same match text and different fields still order the same way whatever the list order.
func wholeRuleKey(r *Rule) string {
	return fmt.Sprintf("%s\x00as_basename=%t\x00permit=%s\x00message=%s\x00layer=%d",
		describeRule(r), r.AsBasename, strings.Join(r.PermitUnsafeEnvs, ","), r.Message, r.layer)
}

// chainRel is how a chain of wrapper rules compares with one ordinary allow.
type chainRel int

const (
	// chainIncomparable means neither outranks the other, so the two tie.
	chainIncomparable chainRel = iota
	chainAbove
	allowAbove
)

// compareChain compares a chain with one ordinary allow.
//
// The inner layer may settle on more than one rule, so the chain has one member rank per inner
// deciding rule. The chain outranks the allow only when it outranks it with every member. The allow
// outranks the chain only when it outranks every member.
//
// An allow equal to a member on every step decides over it, since an ordinary allow consented to the
// whole argv as typed.
func compareChain(members []rank, a rank) chainRel {
	above, below := true, true

	for _, m := range members {
		if !m.outranks(a) {
			above = false
		}
		if !a.outranks(m) && a != m {
			below = false
		}
	}

	switch {
	case below:
		return allowAbove
	case above:
		return chainAbove
	}

	return chainIncomparable
}

// chainMembers ranks a peel as one member per inner member.
//
// The count is the wrapper's tokens plus the inner count, and the kind is the inner member's. The
// chain is path-form only when every rule in it is.
//
// The layer is the least specific layer of the wrapper rules and the inner member, so the chain is
// a project rule only when every rule in it is.
func chainMembers(wrappers []*Rule, inner []rank) []rank {
	w := wrappers[0].ranking()
	n := w.count

	out := make([]rank, 0, len(inner))
	for _, m := range inner {
		out = append(out, rank{
			count: n + m.count,
			shape: m.shape,
			path:  w.path && m.path,
			layer: max(w.layer, m.layer),
		})
	}

	return out
}
