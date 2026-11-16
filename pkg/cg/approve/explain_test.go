package approve

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// summarizeExplain renders everything an explanation reports, so two explanations of the same rules
// in different list orders can be compared.
func summarizeExplain(layers []LayerMatches) string {
	var b strings.Builder

	for _, lm := range layers {
		fmt.Fprintf(&b, "layer %d %q\n", lm.Offset, lm.Argv)

		for _, m := range lm.Matches {
			fmt.Fprintf(&b, "  level=%d count=%d kind=%s path=%t layer=%s chain=%t rule=%q wrappers=%q inner=%q\n",
				m.Level, m.Count, m.Kind, m.PathForm, m.Layer, m.IsChain(), ruleKeyOrNone(m.Rule), ruleKeyList(m.Wrappers), ruleKeyList(m.Inner))
		}
	}

	return b.String()
}

func ruleKeyOrNone(r *Rule) string {
	if r == nil {
		return "-"
	}

	return wholeRuleKey(r)
}

// checkExplain runs one ruleset through Evaluate and Explain and compares them. It returns a summary
// of the explanation, so a caller can compare list orders.
//
// Where allows decide, the explained deciding set of the last layer equals the verdict's. A deny or a
// restrict decides by something other than the matching allows, so there is nothing to compare. A
// wrapper that lacks the env permit decides by itself, and its chain must still be listed at level 0
// with that wrapper among its wrappers.
func checkExplain(t *testing.T, rs *Ruleset, subj Subject, env map[string]string, resolve Resolver) string {
	t.Helper()

	v := rs.Evaluate(subj, env, resolve)
	layers := rs.Explain(subj, env, resolve)

	wantLayers := max(1, len(v.Chain))
	if len(layers) != wantLayers {
		t.Fatalf("explained layers = %d, verdict layers = %d\n%s", len(layers), wantLayers, summarizeExplain(layers))
	}

	last := layers[len(layers)-1]

	step, rule := v.Step, v.Rule
	if n := len(v.Chain); n > 0 {
		cl := v.Chain[n-1]
		step, rule = cl.Step, cl.Rule

		if last.Offset != cl.Offset {
			t.Fatalf("last explained offset = %d, verdict's = %d", last.Offset, cl.Offset)
		}
	}

	switch {
	case step == StepDeny, step == StepRestrict:
	case step == StepEnvRefusal && rule != nil && rule.Kind() == KindWrapper:
		found := false

		for _, m := range last.Matches {
			if m.IsChain() && m.Deciding() && slices.Contains(m.Wrappers, rule) {
				found = true
			}
		}

		if !found {
			t.Errorf("no deciding chain lists the refusing wrapper %s\n%s", rule, summarizeExplain(layers))
		}
	default:
		got, want := ruleKeyList(last.DecidingRules()), ruleKeyList(v.Deciding)
		if !slices.Equal(got, want) {
			t.Errorf("explained deciding set = %q, verdict's = %q\n%s", got, want, summarizeExplain(layers))
		}
	}

	return summarizeExplain(layers)
}

// TestExplainDecidingSetMatchesVerdict guards the contract that the listing `cg check` prints and the
// verdict read one ranking. Every ranking fixture and every wrapper fixture runs through both calls, in
// two list orders. It cannot prove the ranking itself is right. The fixtures do that.
func TestExplainDecidingSetMatchesVerdict(t *testing.T) {
	t.Parallel()

	for _, tt := range rankEvalTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var first string

			for i, order := range [][]int{identity(len(tt.allow)), reversed(len(tt.allow))} {
				rs, subj, resolve := rankFixture(t, tt, order, identity(len(tt.deny)), identity(len(tt.restrict)))
				got := checkExplain(t, rs, subj, tt.env, resolve)

				if i == 0 {
					first = got
				} else if got != first {
					t.Errorf("allow order changes the explanation:\n%s\nwant:\n%s", got, first)
				}
			}
		})
	}

	for _, tt := range wrapperTests {
		t.Run("wrapper "+tt.name, func(t *testing.T) {
			t.Parallel()

			var first string

			for i, reverse := range []bool{false, true} {
				rs := buildWrapperRuleset(tt, reverse)

				subj := Subject{Argv: tt.argv, Canonical: tt.canonical}
				if subj.Canonical == nil {
					subj.Canonical = tt.argv
				}

				got := checkExplain(t, rs, subj, tt.env, stubResolver(tt.paths, new([]string)))

				if i == 0 {
					first = got
				} else if got != first {
					t.Errorf("allow order changes the explanation:\n%s\nwant:\n%s", got, first)
				}
			}
		})
	}
}

// TestExplainListsInnerRulesOfAChainRefusedForTheEnv pins that a peel refused because its wrapper lacks
// the env permit still competes, and its entry names the rules the inner layer settled on. The refused
// verdict names the wrapper alone, and the listing must not copy that into the inner rules.
func TestExplainListsInnerRulesOfAChainRefusedForTheEnv(t *testing.T) {
	t.Parallel()

	rs := &Ruleset{Mode: ModeEnforce, Allow: buildRules(t, []ruleSpec{wr("nice"), px("make").permits("LD_PRELOAD")}, []int{0, 1})}
	subj := Subject{Argv: []string{"nice", "make"}, Canonical: []string{"nice", "make"}}
	env := map[string]string{"LD_PRELOAD": "y.so"}
	resolve := stubResolver(defaultPaths, new([]string))

	v := rs.Evaluate(subj, env, resolve)
	if v.Decision != DecisionRefuse || v.Step != StepEnvRefusal || v.Rule == nil || v.Rule.Kind() != KindWrapper {
		t.Fatalf("verdict = %s, want an env refusal naming the wrapper", summarize(v))
	}

	layers := rs.Explain(subj, env, resolve)
	if len(layers) != 1 || len(layers[0].Matches) != 1 {
		t.Fatalf("explanation = %s, want one layer with one chain", summarizeExplain(layers))
	}

	m := layers[0].Matches[0]
	if !m.IsChain() || !m.Deciding() || !slices.Equal(ruleTexts(m.Wrappers), []string{"wrapper [nice]"}) || !slices.Equal(ruleTexts(m.Inner), []string{"prefix [make]"}) {
		t.Errorf("match = %s, want a deciding chain of wrapper [nice] over prefix [make]", renderMatch(m))
	}
}

func reversed(n int) []int {
	out := identity(n)
	slices.Reverse(out)

	return out
}

type explainTest struct {
	Name      string
	Allow     []ruleSpec
	Argv      []string
	Canonical []string

	// Want lists "level count kind form layer rule" per match, per layer, in order.
	Want [][]string
}

var explainTests = []explainTest{
	{
		Name:  "levels peel off the deciding set",
		Allow: []ruleSpec{px("make"), ex("make", "test"), px("make", "test")},
		Argv:  []string{"make", "test"},
		Want: [][]string{{
			`0 2 exact bare project exact [make, test]`,
			`1 2 prefix bare project prefix [make, test]`,
			`2 1 prefix bare project prefix [make]`,
		}},
	},
	{
		Name:      "a tie shares level 0 and sorts by rule text",
		Allow:     []ruleSpec{re(`^make test x`).base(), px("/usr/bin/make", "test")},
		Argv:      []string{"make", "test", "x"},
		Canonical: []string{"/usr/bin/make", "test", "x"},
		Want: [][]string{{
			`0 2 prefix path project prefix [/usr/bin/make, test]`,
			`0 2 pattern bare project regex "^make test x"`,
		}},
	},
	{
		Name:  "a pattern that ranks below a prefix is listed",
		Allow: []ruleSpec{px("git", "push"), re(`^git (push|pull) --force( |$)`).base()},
		Argv:  []string{"git", "push", "--force"},
		Want: [][]string{{
			`0 2 prefix bare project prefix [git, push]`,
			`1 1 pattern bare project regex "^git (push|pull) --force( |$)"`,
		}},
	},
	{
		Name:  "a rule that does not match is not listed",
		Allow: []ruleSpec{px("git", "push"), px("make")},
		Argv:  []string{"git", "push"},
		Want:  [][]string{{`0 2 prefix bare project prefix [git, push]`}},
	},
	{
		Name:  "a global rule ranks below an equal project rule",
		Allow: []ruleSpec{px("make").in(layerGlobal), px("make")},
		Argv:  []string{"make"},
		Want: [][]string{{
			`0 1 prefix bare project prefix [make]`,
			`1 1 prefix bare global prefix [make]`,
		}},
	},
	{
		Name:  "a path rule decides over a bare rule on a wrapped layer",
		Allow: []ruleSpec{wr("nice"), px("/usr/bin/make", "test"), px("make", "test")},
		Argv:  []string{"nice", "make", "test"},
		Want: [][]string{
			{`0 3 prefix bare project chain wrapper [nice] over prefix [/usr/bin/make, test]`},
			{
				`0 2 prefix path project prefix [/usr/bin/make, test]`,
				`1 2 prefix bare project prefix [make, test]`,
			},
		},
	},
	{
		Name:  "an ordinary allow that outranks the chain decides and the chain is listed below it",
		Allow: []ruleSpec{wr("nice"), px("make"), ex("nice", "make", "test")},
		Argv:  []string{"nice", "make", "test"},
		Want: [][]string{{
			`0 3 exact bare project exact [nice, make, test]`,
			`1 2 prefix bare project chain wrapper [nice] over prefix [make]`,
		}},
	},
}

// TestExplain pins the listing: which matches appear, their count, kind, form, and layer, and their
// level and order. A change to the level assignment or the sort fails here. It does not prove that the
// ranking itself is right.
func TestExplain(t *testing.T) {
	t.Parallel()

	for _, tt := range explainTests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			rs := &Ruleset{Mode: ModeEnforce, Allow: buildRules(t, tt.Allow, identity(len(tt.Allow)))}
			layers := rs.Explain(Subject{Argv: tt.Argv, Canonical: cmpOr(tt.Canonical, tt.Argv)}, nil, stubResolver(defaultPaths, new([]string)))

			if len(layers) != len(tt.Want) {
				t.Fatalf("layers = %d, want %d\n%s", len(layers), len(tt.Want), summarizeExplain(layers))
			}

			for i, lm := range layers {
				var got []string
				for _, m := range lm.Matches {
					got = append(got, renderMatch(m))
				}

				if !slices.Equal(got, tt.Want[i]) {
					t.Errorf("layer %d =\n%s\nwant\n%s", i, strings.Join(got, "\n"), strings.Join(tt.Want[i], "\n"))
				}
			}
		})
	}
}

func renderMatch(m Match) string {
	form := "bare"
	if m.PathForm {
		form = "path"
	}

	what := ""
	if m.IsChain() {
		what = fmt.Sprintf("chain wrapper %s over %s", m.Wrappers[0].Pattern(), strings.Join(ruleTexts(m.Inner), ", "))
	} else {
		what = describeRule(m.Rule)
	}

	return fmt.Sprintf("%d %d %s %s %s %s", m.Level, m.Count, m.Kind, form, m.Layer, what)
}

func TestExplainSkipsNonEnforceModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []Mode{ModeAllowAll, ModeDenyAll} {
		rs := &Ruleset{Mode: mode}
		if got := rs.Explain(Subject{Argv: []string{"make"}}, nil, nil); got != nil {
			t.Errorf("mode %v explained %v, want nothing", mode, got)
		}
	}
}

func cmpOr(a, b []string) []string {
	if a != nil {
		return a
	}

	return b
}
