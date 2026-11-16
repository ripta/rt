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
// It reads the ranking Evaluate reads. Evaluate and the gate never call it, so a rule the ranking
// put below another can never supply a field to a call.
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

// explainItem is a match under comparison. A chain carries its member ranks.
type explainItem struct {
	match   Match
	rank    rank
	members []rank
	key     string
}

// beats reports whether x outranks y. Two chains never meet, since a layer has one peel.
func (x explainItem) beats(y explainItem) bool {
	switch {
	case x.members == nil && y.members == nil:
		return x.rank.outranks(y.rank)
	case x.members != nil && y.members == nil:
		return compareChain(x.members, y.rank) == chainAbove
	case x.members == nil:
		return compareChain(y.members, x.rank) == allowAbove
	}

	return false
}

// explainLayer assigns every matching allow of a layer its dominance level.
func explainLayer(matched []*Rule, peel *settled) []Match {
	items := make([]explainItem, 0, len(matched)+1)

	for _, rule := range matched {
		rk := rule.ranking()
		items = append(items, explainItem{match: Match{Rule: rule}, rank: rk, key: wholeRuleKey(rule)})
	}

	if peel != nil && len(peel.members) > 0 {
		items = append(items, explainItem{
			match:   Match{Wrappers: peel.wrappers, Inner: peel.chainInner},
			rank:    peel.members[0],
			members: peel.members,
			key:     wholeRuleKey(peel.wrappers[0]),
		})
	}

	for i := range items {
		rk := items[i].rank
		items[i].match.Count, items[i].match.Kind, items[i].match.PathForm, items[i].match.Layer = rk.count, rk.shape.String(), rk.path, rk.layer.String()
	}

	var out []Match

	remaining := items
	for level := 0; len(remaining) > 0; level++ {
		var top, rest []explainItem

		for i, it := range remaining {
			beaten := false
			for j, other := range remaining {
				if i != j && other.beats(it) {
					beaten = true
					break
				}
			}

			if beaten {
				rest = append(rest, it)
			} else {
				top = append(top, it)
			}
		}

		// The relation is a partial order, so some item is always undominated. A cycle would
		// otherwise never end.
		if len(top) == 0 {
			top, rest = rest, nil
		}

		slices.SortStableFunc(top, func(a, b explainItem) int { return strings.Compare(a.key, b.key) })

		for _, it := range top {
			it.match.Level = level
			out = append(out, it.match)
		}

		remaining = rest
	}

	return out
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
