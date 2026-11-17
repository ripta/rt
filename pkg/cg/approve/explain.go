package approve

import (
	"slices"
	"strings"
)

// Match is one allow that matched a layer, with the rank it competed at.
//
// An ordinary rule sets Rule. A wrapper chain that competes with the ordinary rules sets Wrappers,
// the wrapper rules the peel chose, and Inner, the rules the inner layer settled on.
//
// For a chain, Count, Kind, PathForm, and Layer are those of its first inner member. Its Level comes
// from comparing it against every inner member, so a chain with several inner rules can sit at a level
// its printed rank alone would not give.
type Match struct {
	Rule     *Rule
	Wrappers []*Rule
	Inner    []*Rule

	Count    int
	Kind     string
	PathForm bool
	Layer    string

	// Level is the dominance level. Level 0 is the deciding set. Level 1 holds the matches only level
	// 0 outranks, and so on.
	Level int
}

// Deciding reports whether the match is in the deciding set.
func (m Match) Deciding() bool { return m.Level == 0 }

// IsChain reports whether the match is a wrapper chain rather than an ordinary rule.
func (m Match) IsChain() bool { return m.Rule == nil }

// Rules lists the rules a match stands for: the ordinary rule, or the inner rules of a chain.
func (m Match) Rules() []*Rule {
	if m.Rule != nil {
		return []*Rule{m.Rule}
	}

	return m.Inner
}

// LayerMatches is every allow that matched one layer the verdict reached, ordered by dominance
// level, then by rule text.
type LayerMatches struct {
	Offset  int
	Argv    []string
	Matches []Match
}

// DecidingRules lists the rules of the level 0 matches in whole-rule order.
func (lm LayerMatches) DecidingRules() []*Rule {
	var out []*Rule

	for _, m := range lm.Matches {
		if m.Deciding() {
			out = append(out, m.Rules()...)
		}
	}

	sortRules(out)

	return slices.CompactFunc(out, func(a, b *Rule) bool { return a == b })
}

// Explain lists, for each layer the verdict for subj reaches, every allow that matched it with its
// rank, and marks the deciding set.
//
// Level 0 comes from the same decidingAllows call Evaluate makes. Evaluate and the gate never call
// Explain, so a rule the ranking put below another can never supply a field to a call.
//
// It lists allows only. A deny or a restrict that matched is not a match here. A wrapper chain is
// listed whenever it competes, which includes a peel refused because a wrapper lacks the env permit,
// and is then listed with the rules its inner layer settled on. A wrapper whose peel does not compete,
// because the inner layer prompts or a restrict refuses it, is not listed.
func (rs *Ruleset) Explain(subj Subject, env map[string]string, resolve Resolver) []LayerMatches {
	if len(subj.Argv) == 0 || rs.Mode == ModeAllowAll || rs.Mode == ModeDenyAll {
		return nil
	}

	e := rs.newEvaluation(subj, env, resolve)
	v := e.run()

	offsets := []int{0}
	if len(v.Chain) > 0 {
		offsets = offsets[:0]
		for _, cl := range v.Chain {
			offsets = append(offsets, cl.Offset)
		}
	}

	out := make([]LayerMatches, 0, len(offsets))
	for _, offset := range offsets {
		l := e.layerAt(offset)
		out = append(out, LayerMatches{Offset: offset, Argv: l.argv, Matches: explainLayer(matching(e.rs.Allow, l), e.layerPeel(l))})
	}

	return out
}

// explainLayer assigns every matching allow of a layer its dominance level.
//
// Level 0 is what decidingAllows returns, the same call the verdict makes. Each later level applies
// decidingAllows again to the matches the earlier levels left.
func explainLayer(matched []*Rule, peel *settled) []Match {
	var out []Match

	if peel != nil && len(peel.members) == 0 {
		peel = nil
	}

	for level := 0; len(matched) > 0 || peel != nil; level++ {
		tied, chainIn := decidingAllows(matched, peel)

		// The relation is a partial order, so some match is always undominated. A cycle would
		// otherwise never end.
		if len(tied) == 0 && !chainIn {
			tied, chainIn = matched, peel != nil
		}

		var top []Match

		for _, rule := range tied {
			top = append(top, newMatch(Match{Rule: rule}, rule.ranking()))
		}

		if chainIn && peel != nil {
			top = append(top, newMatch(Match{Wrappers: peel.wrappers, Inner: peel.chainInner}, peel.members[0]))
		}

		slices.SortStableFunc(top, func(a, b Match) int { return strings.Compare(matchKey(a), matchKey(b)) })

		for _, m := range top {
			m.Level = level
			out = append(out, m)
		}

		matched = slices.DeleteFunc(slices.Clone(matched), func(r *Rule) bool { return slices.Contains(tied, r) })

		if chainIn {
			peel = nil
		}
	}

	return out
}

// newMatch fills the display fields of a match from the rank it competed at.
func newMatch(m Match, rk rank) Match {
	m.Count, m.Kind, m.PathForm, m.Layer = rk.count, rk.shape.String(), rk.path, rk.layer.String()

	return m
}

// matchKey is the whole-rule key a level is ordered by. A chain is keyed by its first wrapper.
func matchKey(m Match) string {
	if m.IsChain() {
		return wholeRuleKey(m.Wrappers[0])
	}

	return wholeRuleKey(m.Rule)
}

// String renders a rank shape for display.
func (s rankShape) String() string {
	switch s {
	case shapeExact:
		return "exact"
	case shapePattern:
		return "pattern"
	default:
		return "prefix"
	}
}

// String renders a rule layer for display.
func (l ruleLayer) String() string {
	switch l {
	case layerGlobal:
		return "global"
	case layerBuiltin:
		return "builtin"
	default:
		return "project"
	}
}
