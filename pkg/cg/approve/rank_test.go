package approve

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// ruleSpec describes a rule for the ranking tests. A pattern kind keeps its text in pattern.
type ruleSpec struct {
	kind    RuleKind
	tokens  []string
	pattern string
	permit  []string
	message string
	asBase  bool
	layer   ruleLayer
}

func ex(tokens ...string) ruleSpec { return ruleSpec{kind: KindExact, tokens: tokens} }

func px(tokens ...string) ruleSpec { return ruleSpec{kind: KindPrefix, tokens: tokens} }

func wr(tokens ...string) ruleSpec { return ruleSpec{kind: KindWrapper, tokens: tokens} }

func re(pattern string) ruleSpec { return ruleSpec{kind: KindRegex, pattern: pattern} }

func (s ruleSpec) permits(names ...string) ruleSpec {
	s.permit = names
	return s
}

func (s ruleSpec) msg(text string) ruleSpec {
	s.message = text
	return s
}

func (s ruleSpec) base() ruleSpec {
	s.asBase = true
	return s
}

func (s ruleSpec) in(layer ruleLayer) ruleSpec {
	s.layer = layer
	return s
}

func (s ruleSpec) rule(t *testing.T) Rule {
	t.Helper()

	r := Rule{kind: s.kind, PermitUnsafeEnvs: s.permit, Message: s.message, AsBasename: s.asBase, layer: s.layer}
	switch s.kind {
	case KindExact:
		r.Exact = s.tokens
	case KindPrefix:
		r.Prefix = s.tokens
	case KindWrapper:
		r.Wrapper = s.tokens
	case KindGlob:
		r.Glob = s.pattern
	case KindRegex:
		r.Regex = s.pattern
	}

	if err := compileRule(&r); err != nil {
		t.Fatalf("compiling %q: %v", s.pattern, err)
	}
	compileMatch(&r, "")

	return r
}

func buildRules(t *testing.T, specs []ruleSpec, order []int) []Rule {
	t.Helper()

	out := make([]Rule, len(specs))
	for i, j := range order {
		out[i] = specs[j].rule(t)
	}

	return out
}

// permutations lists every ordering of 0..n-1.
func permutations(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}

	var out [][]int
	for _, rest := range permutations(n - 1) {
		for at := 0; at <= len(rest); at++ {
			p := slices.Insert(slices.Clone(rest), at, n-1)
			out = append(out, p)
		}
	}

	return out
}

func ruleKeyList(rules []*Rule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = wholeRuleKey(r)
	}

	return out
}

func ruleTexts(rules []*Rule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = describeRule(r)
	}

	return out
}

// summarize renders everything a verdict reports, so two verdicts for the same rules in different
// list orders can be compared.
func summarize(v Verdict) string {
	var b strings.Builder

	rule := "-"
	if v.Rule != nil {
		rule = wholeRuleKey(v.Rule)
	}
	fmt.Fprintf(&b, "%v restricted=%t bad=%v step=%v rule=%q deciding=%q", v.Decision, v.Restricted, v.BadEnvs, v.Step, rule, ruleKeyList(v.Deciding))

	for _, l := range v.Chain {
		own := "-"
		if l.Rule != nil {
			own = wholeRuleKey(l.Rule)
		}
		fmt.Fprintf(&b, "\n  [%d] step=%v own=%q wrappers=%q candidates=%q path=%t", l.Offset, l.Step, own, ruleKeyList(l.Wrappers), ruleKeyList(l.Candidates), l.PathForm)
	}

	for _, sk := range v.Skipped {
		fmt.Fprintf(&b, "\n  skip %d %v %q", sk.Offset, sk.Reason, wholeRuleKey(sk.Rule))
	}

	return b.String()
}

type rankEvalTest struct {
	name string

	deny     []ruleSpec
	allow    []ruleSpec
	restrict []ruleSpec

	argv      []string
	canonical []string
	env       map[string]string
	paths     map[string]string

	want        Decision
	wantBadEnvs []string

	// wantRule is the text of the rule the verdict names, such as `prefix [make]`.
	wantRule string

	// wantDeciding is the texts of the deciding set in order. It is not checked when nil.
	wantDeciding []string

	wantRestricted bool
	wantStep       Step

	// wantChain is the number of chain layers.
	wantChain int

	// wantPathForm, when set, is the PathForm of each chain layer.
	wantPathForm []bool
}

var defaultPaths = map[string]string{"git": "/usr/bin/git", "make": "/usr/bin/make", "python": "/usr/bin/python", "nice": "/usr/bin/nice", "foo": "/usr/local/bin/foo"}

var rankEvalTests = []rankEvalTest{
	{
		name:         "a broader permit does not rescue the most specific rule",
		allow:        []ruleSpec{ex("make", "test"), px("make").permits("PATH")},
		argv:         []string{"make", "test"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `exact [make, test]`,
		wantDeciding: []string{`exact [make, test]`},
		wantStep:     StepEnvRefusal,
	},
	{
		name:         "the most specific rule's permit runs the call",
		allow:        []ruleSpec{ex("make", "test").permits("PATH"), px("make")},
		argv:         []string{"make", "test"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRun,
		wantRule:     `exact [make, test]`,
		wantDeciding: []string{`exact [make, test]`},
		wantStep:     StepAllow,
	},
	{
		name:     "a deny refuses whatever the specificity of an allow",
		deny:     []ruleSpec{px("make")},
		allow:    []ruleSpec{ex("make", "test").permits("PATH")},
		argv:     []string{"make", "test"},
		want:     DecisionRefuse,
		wantRule: `prefix [make]`,
		wantStep: StepDeny,
	},
	{
		name:        "exact outranks a pattern of the same form",
		allow:       []ruleSpec{ex("git", "status"), re(`^git status( -s|$)`).base().permits("PATH"), px("git", "status").permits("PATH")},
		argv:        []string{"git", "status"},
		env:         map[string]string{"PATH": "/x"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantRule:    `exact [git, status]`,
	},
	{
		name:        "a pattern outranks prefix of the same form",
		allow:       []ruleSpec{re(`^git status( -s|$)`).base(), px("git", "status").permits("PATH")},
		argv:        []string{"git", "status"},
		env:         map[string]string{"PATH": "/x"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantRule:    `regex "^git status( -s|$)"`,
	},
	{
		name:     "a pattern that outranks prefix runs on its own permit",
		allow:    []ruleSpec{re(`^git status( -s|$)`).base().permits("PATH"), px("git", "status")},
		argv:     []string{"git", "status"},
		env:      map[string]string{"PATH": "/x"},
		want:     DecisionRun,
		wantRule: `regex "^git status( -s|$)"`,
	},
	{
		name:         "a boundary regex ranks as prefix and ties with it",
		allow:        []ruleSpec{re(`^make test( |$)`).base(), px("make", "test").permits("PATH")},
		argv:         []string{"make", "test"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `regex "^make test( |$)"`,
		wantDeciding: []string{`prefix [make, test]`, `regex "^make test( |$)"`},
		wantStep:     StepEnvRefusal,
	},
	{
		name:         "an end-anchored regex ranks as exact over prefix",
		allow:        []ruleSpec{re(`^make test$`).base(), px("make", "test").permits("PATH")},
		argv:         []string{"make", "test"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `regex "^make test$"`,
		wantDeciding: []string{`regex "^make test$"`},
	},
	{
		name:         "an end-anchored regex ties with exact",
		allow:        []ruleSpec{re(`^make test$`).base(), ex("make", "test").permits("PATH")},
		argv:         []string{"make", "test"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `regex "^make test$"`,
		wantDeciding: []string{`exact [make, test]`, `regex "^make test$"`},
	},
	{
		name:         "a bare pattern and a path-form prefix are incomparable and tie",
		allow:        []ruleSpec{re(`^make test x`).base().permits("PATH"), px("/usr/bin/make", "test")},
		argv:         []string{"make", "test", "x"},
		canonical:    []string{"/usr/bin/make", "test", "x"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `prefix [/usr/bin/make, test]`,
		wantDeciding: []string{`prefix [/usr/bin/make, test]`, `regex "^make test x"`},
	},
	{
		name:         "a remembered path-form prefix outranks a bare prefix with a permit",
		allow:        []ruleSpec{px("/usr/bin/make", "test"), px("make", "test").permits("PATH")},
		argv:         []string{"make", "test"},
		canonical:    []string{"/usr/bin/make", "test"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `prefix [/usr/bin/make, test]`,
		wantDeciding: []string{`prefix [/usr/bin/make, test]`},
	},
	{
		name:         "a global rule that is more specific decides over a project rule",
		allow:        []ruleSpec{px("make").permits("PATH").in(layerProject), ex("make", "test").in(layerGlobal)},
		argv:         []string{"make", "test"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `exact [make, test]`,
		wantDeciding: []string{`exact [make, test]`},
	},
	{
		name:         "a project rule outranks an otherwise equal global rule",
		allow:        []ruleSpec{px("make", "test").in(layerGlobal), px("make", "test").permits("PATH").in(layerProject)},
		argv:         []string{"make", "test"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRun,
		wantRule:     `prefix [make, test]`,
		wantDeciding: []string{`prefix [make, test]`},
	},
	{
		name:         "equal rules that both permit run together",
		allow:        []ruleSpec{re(`^make test( |$)`).base().permits("PATH"), px("make", "test").permits("PATH")},
		argv:         []string{"make", "test"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRun,
		wantRule:     `prefix [make, test]`,
		wantDeciding: []string{`prefix [make, test]`, `regex "^make test( |$)"`},
		wantStep:     StepAllow,
	},
	{
		name:         "an env refusal among tied rules names the one missing fewest variables",
		allow:        []ruleSpec{px("make").permits("PATH"), px("make").permits("LD_PRELOAD", "PYTHONPATH"), re(`^make( |$)`).base()},
		argv:         []string{"make"},
		env:          map[string]string{"PATH": "/x", "LD_PRELOAD": "y", "PYTHONPATH": "z"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `prefix [make]`,
		wantDeciding: []string{`prefix [make]`, `prefix [make]`, `regex "^make( |$)"`},
	},
	{
		name:        "the tie-break compares whole rules, not match text",
		allow:       []ruleSpec{px("make").permits("PATH"), px("make").permits("LD_PRELOAD")},
		argv:        []string{"make"},
		env:         map[string]string{"PATH": "/x", "LD_PRELOAD": "y"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantRule:    `prefix [make]`,
	},
	{
		name:      "equal denies on one layer are all named in rule-text order",
		deny:      []ruleSpec{re(`^rm( |$)`).base().msg("b"), px("rm").msg("a")},
		argv:      []string{"rm", "x"},
		want:      DecisionRefuse,
		wantRule:  `prefix [rm]`,
		wantStep:  StepDeny,
		wantChain: 0,
		wantDeciding: []string{
			`prefix [rm]`, `regex "^rm( |$)"`,
		},
	},
	{
		name:         "equal denies with the same match text order by their whole rule",
		deny:         []ruleSpec{px("rm").msg("z"), px("rm").msg("a")},
		argv:         []string{"rm", "x"},
		want:         DecisionRefuse,
		wantRule:     `prefix [rm]`,
		wantDeciding: []string{`prefix [rm]`, `prefix [rm]`},
	},
	{
		name:         "a more specific deny is named alone",
		deny:         []ruleSpec{px("rm"), px("rm", "-rf").in(layerGlobal)},
		argv:         []string{"rm", "-rf", "x"},
		want:         DecisionRefuse,
		wantRule:     `prefix [rm, -rf]`,
		wantDeciding: []string{`prefix [rm, -rf]`},
	},
	{
		name:         "a builtin deny ranks below a global one",
		deny:         []ruleSpec{px("sh").in(layerBuiltin), px("sh").msg("global").in(layerGlobal)},
		argv:         []string{"sh"},
		want:         DecisionRefuse,
		wantRule:     `prefix [sh]`,
		wantDeciding: []string{`prefix [sh]`},
	},
	{
		name:           "a restrict refusal names the deciding set on its layer",
		restrict:       []ruleSpec{px("git").msg("broad"), px("git", "push").msg("narrow"), re(`^git push( |$)`).base().msg("same")},
		argv:           []string{"git", "push"},
		want:           DecisionRefuse,
		wantRestricted: true,
		wantRule:       `prefix [git, push]`,
		wantDeciding:   []string{`prefix [git, push]`, `regex "^git push( |$)"`},
		wantStep:       StepRestrict,
	},
	{
		name:           "restrict stays a tier below allow",
		allow:          []ruleSpec{re(`^git (show|log)( |$)`).base()},
		restrict:       []ruleSpec{px("git")},
		argv:           []string{"git", "show"},
		want:           DecisionRun,
		wantRestricted: false,
	},
	{
		name:        "wrapper rules of equal length are chosen by form first",
		allow:       []ruleSpec{wr("/usr/bin/nice", "-n", "19"), wr("nice", "-n", "19").permits("PATH"), px("make").permits("PATH")},
		argv:        []string{"nice", "-n", "19", "make", "test"},
		canonical:   []string{"/usr/bin/nice", "-n", "19", "make", "test"},
		env:         map[string]string{"PATH": "/x"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantRule:    `wrapper [/usr/bin/nice, -n, 19]`,
		wantChain:   1,
	},
	{
		name:        "wrapper rules of equal form are chosen by layer",
		allow:       []ruleSpec{wr("nice").permits("PATH").in(layerGlobal), wr("nice").in(layerProject), px("make").permits("PATH")},
		argv:        []string{"nice", "make"},
		env:         map[string]string{"PATH": "/x"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantRule:    `wrapper [nice]`,
		wantChain:   1,
	},
	{
		name:         "a chain outranks a covering prefix and names the inner rule that refuses",
		allow:        []ruleSpec{px("nice").permits("PATH"), wr("nice", "-n", "19").permits("PATH"), px("make")},
		argv:         []string{"nice", "-n", "19", "make", "test"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `prefix [make]`,
		wantDeciding: []string{`prefix [make]`},
		wantChain:    2,
	},
	{
		name:      "a covering prefix decides when the inner command has no rule",
		allow:     []ruleSpec{px("nice"), wr("nice", "-n", "19")},
		argv:      []string{"nice", "-n", "19", "foo"},
		want:      DecisionRun,
		wantRule:  `prefix [nice]`,
		wantChain: 0,
	},
	{
		name:      "an exact carve-out decides over an equal bare-form chain",
		allow:     []ruleSpec{ex("nice", "-n", "19", "make", "test").permits("PATH"), wr("nice", "-n", "19"), ex("make", "test")},
		argv:      []string{"nice", "-n", "19", "make", "test"},
		env:       map[string]string{"PATH": "/x"},
		want:      DecisionRun,
		wantRule:  `exact [nice, -n, 19, make, test]`,
		wantChain: 0,
	},
	{
		name:           "a restrict scope refuses a peel whose inner command has no rule",
		allow:          []ruleSpec{wr("sudo", "-u", "build")},
		restrict:       []ruleSpec{px("sudo")},
		argv:           []string{"sudo", "-u", "build", "rm", "-rf", "x"},
		want:           DecisionRefuse,
		wantRestricted: true,
		wantRule:       `prefix [sudo]`,
		wantStep:       StepRestrict,
	},
	{
		name: "a chain whose inner deciding set is incomparable is compared with each member",
		allow: []ruleSpec{
			wr("/usr/bin/nice").permits("PATH"),
			re(`^make test x`).base().permits("PATH"),
			px("/usr/bin/make", "test"),
			px("nice", "make", "test").permits("PATH"),
		},
		argv:         []string{"nice", "make", "test", "x"},
		canonical:    []string{"/usr/bin/nice", "make", "test", "x"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `prefix [/usr/bin/make, test]`,
		wantDeciding: []string{`prefix [/usr/bin/make, test]`, `regex "^make test x"`},
		wantChain:    2,
	},
	{
		name:         "an incomparable chain and ordinary allow tie and both run",
		allow:        []ruleSpec{wr("/usr/bin/nice").permits("PATH"), px("/usr/bin/make").permits("PATH"), ex("nice", "make").permits("PATH")},
		argv:         []string{"nice", "make"},
		canonical:    []string{"/usr/bin/nice", "make"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRun,
		wantRule:     `exact [nice, make]`,
		wantDeciding: []string{`exact [nice, make]`, `prefix [/usr/bin/make]`},
		wantStep:     StepAllow,
	},
	{
		name:         "a tie refuses when the ordinary allow lacks the permit",
		allow:        []ruleSpec{wr("/usr/bin/nice").permits("PATH"), px("/usr/bin/make").permits("PATH"), ex("nice", "make")},
		argv:         []string{"nice", "make"},
		canonical:    []string{"/usr/bin/nice", "make"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `exact [nice, make]`,
		wantDeciding: []string{`exact [nice, make]`, `prefix [/usr/bin/make]`},
		wantStep:     StepEnvRefusal,
	},
	{
		name:         "a tie refuses when the chain lacks the permit",
		allow:        []ruleSpec{wr("/usr/bin/nice").permits("PATH"), px("/usr/bin/make"), ex("nice", "make").permits("PATH")},
		argv:         []string{"nice", "make"},
		canonical:    []string{"/usr/bin/nice", "make"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `prefix [/usr/bin/make]`,
		wantDeciding: []string{`prefix [/usr/bin/make]`},
		wantChain:    2,
	},
	{
		name: "a path-form chain with an exact inner rule outranks a bare exact carve-out",
		allow: []ruleSpec{
			wr("/usr/bin/uv", "run").permits("PATH"),
			ex("/usr/bin/python", "-m", "pytest"),
			ex("uv", "run", "python", "-m", "pytest").permits("PATH"),
		},
		argv:        []string{"uv", "run", "python", "-m", "pytest"},
		canonical:   []string{"/usr/bin/uv", "run", "python", "-m", "pytest"},
		env:         map[string]string{"PATH": "/x"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantRule:    `exact [/usr/bin/python, -m, pytest]`,
		wantChain:   2,
	},
	{
		name: "an inner tie refusal still competes against a broad ordinary allow",
		allow: []ruleSpec{
			px("nice").permits("PATH"),
			wr("nice").permits("PATH"),
			re(`^make test x`).base().permits("PATH"),
			px("/usr/bin/make", "test"),
		},
		argv:        []string{"nice", "make", "test", "x"},
		env:         map[string]string{"PATH": "/x"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantRule:    `prefix [/usr/bin/make, test]`,
		wantChain:   2,
	},
	{
		name:        "tied wrapper rules each must permit and none supplies a permit for another",
		allow:       []ruleSpec{wr("nice").permits("PATH"), wr("nice").permits("LD_PRELOAD"), px("make").permits("PATH", "LD_PRELOAD")},
		argv:        []string{"nice", "make"},
		env:         map[string]string{"PATH": "/x", "LD_PRELOAD": "y"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantRule:    `wrapper [nice]`,
		wantChain:   1,
	},
	{
		name:         "a path-form inner rule that decides pins its layer",
		allow:        []ruleSpec{wr("uv", "run"), px("/usr/bin/git"), px("git")},
		argv:         []string{"uv", "run", "git", "status"},
		want:         DecisionRun,
		wantRule:     `prefix [/usr/bin/git]`,
		wantDeciding: []string{`prefix [/usr/bin/git]`},
		wantChain:    2,
		wantPathForm: []bool{false, true},
	},
	{
		name:         "a tied set of mixed forms is not pinned",
		allow:        []ruleSpec{wr("uv", "run"), re(`^git status x`).base(), px("/usr/bin/git", "status")},
		argv:         []string{"uv", "run", "git", "status", "x"},
		want:         DecisionRun,
		wantDeciding: []string{`prefix [/usr/bin/git, status]`, `regex "^git status x"`},
		wantChain:    2,
		wantPathForm: []bool{false, false},
	},
	{
		name:         "a chain tied with an ordinary allow leaves the layer unpinned",
		allow:        []ruleSpec{wr("sudo"), wr("/usr/bin/nice"), px("/usr/bin/make"), ex("nice", "make")},
		argv:         []string{"sudo", "nice", "make"},
		want:         DecisionRun,
		wantDeciding: []string{`exact [nice, make]`, `prefix [/usr/bin/make]`},
		wantChain:    2,
		wantPathForm: []bool{false, false},
	},
	{
		name:         "a prompted chain pins a middle layer only when its wrapper rules are path-form",
		allow:        []ruleSpec{wr("sudo"), wr("/usr/bin/nice")},
		argv:         []string{"sudo", "nice", "foo"},
		want:         DecisionPrompt,
		wantChain:    3,
		wantPathForm: []bool{false, true, false},
	},
	{
		name:         "a prompted chain through a bare wrapper rule is not pinned",
		allow:        []ruleSpec{wr("sudo"), wr("nice")},
		argv:         []string{"sudo", "nice", "foo"},
		want:         DecisionPrompt,
		wantChain:    3,
		wantPathForm: []bool{false, false, false},
	},
	{
		name:         "a refusal names the outermost layer with a matching deny whatever the allow order",
		allow:        []ruleSpec{wr("env"), wr("env", "nice")},
		deny:         []ruleSpec{px("nice").msg("never nice"), px("rm", "-rf").msg("use trash")},
		argv:         []string{"env", "nice", "rm", "-rf", "x"},
		want:         DecisionRefuse,
		wantRule:     `prefix [nice]`,
		wantDeciding: []string{`prefix [nice]`},
		wantStep:     StepDeny,
		wantChain:    2,
	},
	{
		name:      "the outermost deny can sit behind a wrapper the allow tier never takes",
		allow:     []ruleSpec{wr("a"), wr("a", "b")},
		deny:      []ruleSpec{px("b").msg("x"), px("c").msg("y")},
		argv:      []string{"a", "b", "c", "d"},
		want:      DecisionRefuse,
		wantRule:  `prefix [b]`,
		wantChain: 2,
	},
	{
		name:      "the wrapper path to a denied layer is the one the allow tier takes",
		allow:     []ruleSpec{wr("a"), wr("a", "b"), wr("b")},
		deny:      []ruleSpec{px("c")},
		argv:      []string{"a", "b", "c", "d"},
		want:      DecisionRefuse,
		wantRule:  `prefix [c]`,
		wantChain: 2,
	},
	{
		name:         "a path-form deny matched by basename on an inner layer ranks as bare",
		allow:        []ruleSpec{wr("sudo")},
		deny:         []ruleSpec{px("/usr/bin/foo").msg("path"), px("foo").msg("bare")},
		argv:         []string{"sudo", "foo"},
		want:         DecisionRefuse,
		wantRule:     `prefix [/usr/bin/foo]`,
		wantDeciding: []string{`prefix [/usr/bin/foo]`, `prefix [foo]`},
		wantChain:    2,
	},
	{
		name:         "a path-form deny on the top layer keeps its path rank",
		deny:         []ruleSpec{px("/usr/bin/foo").msg("path"), px("foo").msg("bare")},
		argv:         []string{"foo"},
		canonical:    []string{"/usr/bin/foo"},
		want:         DecisionRefuse,
		wantRule:     `prefix [/usr/bin/foo]`,
		wantDeciding: []string{`prefix [/usr/bin/foo]`},
	},
	{
		name:         "a global wrapper over a project inner rule loses to an equal project ordinary allow",
		allow:        []ruleSpec{px("nice", "make").permits("PATH"), wr("nice").permits("PATH").in(layerGlobal), px("make")},
		argv:         []string{"nice", "make"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRun,
		wantRule:     `prefix [nice, make]`,
		wantDeciding: []string{`prefix [nice, make]`},
	},
	{
		name:         "an all-project chain outranks an equal global ordinary allow",
		allow:        []ruleSpec{px("nice", "make").permits("PATH").in(layerGlobal), wr("nice").permits("PATH"), px("make")},
		argv:         []string{"nice", "make"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `prefix [make]`,
		wantDeciding: []string{`prefix [make]`},
		wantChain:    2,
	},
	{
		name: "a chain takes the global layer from its inner rule",
		allow: []ruleSpec{
			px("nice", "make").permits("PATH").in(layerGlobal),
			wr("nice").permits("PATH"),
			px("make").in(layerGlobal),
		},
		argv:         []string{"nice", "make"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRun,
		wantRule:     `prefix [nice, make]`,
		wantDeciding: []string{`prefix [nice, make]`},
	},
	{
		name: "a chain takes the global layer from its wrapper rule",
		allow: []ruleSpec{
			px("nice", "make").permits("PATH").in(layerGlobal),
			wr("nice").permits("PATH").in(layerGlobal),
			px("make"),
		},
		argv:         []string{"nice", "make"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRun,
		wantRule:     `prefix [nice, make]`,
		wantDeciding: []string{`prefix [nice, make]`},
	},
	{
		name: "a global exact carve-out loses to an equal all-project chain",
		allow: []ruleSpec{
			ex("nice", "make").permits("PATH").in(layerGlobal),
			wr("nice").permits("PATH"),
			ex("make"),
		},
		argv:         []string{"nice", "make"},
		env:          map[string]string{"PATH": "/x"},
		want:         DecisionRefuse,
		wantBadEnvs:  []string{"PATH"},
		wantRule:     `exact [make]`,
		wantDeciding: []string{`exact [make]`},
		wantChain:    2,
	},
	{
		name:        "a wrapper that lacks the env permit refuses and names itself",
		allow:       []ruleSpec{wr("nice"), px("make").permits("LD_PRELOAD")},
		argv:        []string{"nice", "make"},
		env:         map[string]string{"LD_PRELOAD": "y.so"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"LD_PRELOAD"},
		wantRule:    `wrapper [nice]`,
		wantStep:    StepEnvRefusal,
		wantChain:   1,
	},
}

func TestEvaluateRanking(t *testing.T) {
	t.Parallel()

	for _, tt := range rankEvalTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runRankEval(t, tt)
		})
	}
}

func evalRankTest(t *testing.T, tt rankEvalTest, allow, deny, restrict []int) Verdict {
	t.Helper()

	rs, subj, resolve := rankFixture(t, tt, allow, deny, restrict)

	return rs.Evaluate(subj, tt.env, resolve)
}

// rankFixture builds the ruleset, subject, and resolver of a case, with the lists in the given orders.
func rankFixture(t *testing.T, tt rankEvalTest, allow, deny, restrict []int) (*Ruleset, Subject, Resolver) {
	t.Helper()

	rs := &Ruleset{
		Mode:     ModeEnforce,
		Deny:     buildRules(t, tt.deny, deny),
		Allow:    buildRules(t, tt.allow, allow),
		Restrict: buildRules(t, tt.restrict, restrict),
	}

	subj := Subject{Argv: tt.argv, Canonical: tt.canonical}
	if subj.Canonical == nil {
		subj.Canonical = tt.argv
	}

	var calls []string

	return rs, subj, stubResolver(pathsOrDefault(tt.paths), &calls)
}

func pathsOrDefault(paths map[string]string) map[string]string {
	if paths != nil {
		return paths
	}

	return defaultPaths
}

func identity(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}

	return out
}

// runRankEval checks a case's wants once, then checks that no ordering of the allow, deny, or
// restrict list changes anything a verdict reports.
func runRankEval(t *testing.T, tt rankEvalTest) {
	t.Helper()

	base := evalRankTest(t, tt, identity(len(tt.allow)), identity(len(tt.deny)), identity(len(tt.restrict)))
	if base.Decision != tt.want {
		t.Fatalf("decision = %v, want %v\n%s", base.Decision, tt.want, summarize(base))
	}
	if !slices.Equal(base.BadEnvs, tt.wantBadEnvs) {
		t.Errorf("badEnvs = %v, want %v", base.BadEnvs, tt.wantBadEnvs)
	}
	if base.Restricted != tt.wantRestricted {
		t.Errorf("restricted = %v, want %v", base.Restricted, tt.wantRestricted)
	}
	if tt.wantRule != "" {
		if base.Rule == nil || describeRule(base.Rule) != tt.wantRule {
			t.Errorf("rule = %v, want %s", base.Rule, tt.wantRule)
		}
	}
	if tt.wantDeciding != nil && !slices.Equal(ruleTexts(base.Deciding), tt.wantDeciding) {
		t.Errorf("deciding = %q, want %q", ruleTexts(base.Deciding), tt.wantDeciding)
	}
	if tt.wantStep != StepNone && base.Step != tt.wantStep {
		t.Errorf("step = %v, want %v", base.Step, tt.wantStep)
	}
	if len(base.Chain) != tt.wantChain {
		t.Errorf("chain length = %d, want %d\n%s", len(base.Chain), tt.wantChain, summarize(base))
	}
	if tt.wantPathForm != nil {
		var got []bool
		for _, l := range base.Chain {
			got = append(got, l.PathForm)
		}
		if !slices.Equal(got, tt.wantPathForm) {
			t.Errorf("path forms = %v, want %v", got, tt.wantPathForm)
		}
	}

	want := summarize(base)
	n := len(tt.allow)
	d := len(tt.deny)
	r := len(tt.restrict)

	for _, p := range permutations(n) {
		if got := summarize(evalRankTest(t, tt, p, identity(d), identity(r))); got != want {
			t.Fatalf("allow order %v changes the verdict:\n%s\nwant:\n%s", p, got, want)
		}
	}
	for _, p := range permutations(d) {
		if got := summarize(evalRankTest(t, tt, identity(n), p, identity(r))); got != want {
			t.Fatalf("deny order %v changes the verdict:\n%s\nwant:\n%s", p, got, want)
		}
	}
	for _, p := range permutations(r) {
		if got := summarize(evalRankTest(t, tt, identity(n), identity(d), p)); got != want {
			t.Fatalf("restrict order %v changes the verdict:\n%s\nwant:\n%s", p, got, want)
		}
	}
}

type patternRankTest struct {
	name      string
	kind      RuleKind
	pattern   string
	wantCount int
	wantShape rankShape
}

var patternRankTests = []patternRankTest{
	{name: "boundary group after the tokens", kind: KindRegex, pattern: `^go test( |$)`, wantCount: 2, wantShape: shapePrefix},
	{name: "end anchor after the tokens", kind: KindRegex, pattern: `^make test$`, wantCount: 2, wantShape: shapeExact},
	{name: "unanchored pattern counts nothing", kind: KindRegex, pattern: `push --force`, wantCount: 0, wantShape: shapePattern},
	{name: "unterminated last token is not counted", kind: KindRegex, pattern: `^git push --force`, wantCount: 2, wantShape: shapePattern},
	{name: "capture groups are read through", kind: KindRegex, pattern: `^(go) (test)$`, wantCount: 2, wantShape: shapeExact},
	{name: "a boundary group with a tail is a pattern", kind: KindRegex, pattern: `^git status( -s|$)`, wantCount: 2, wantShape: shapePattern},
	{name: "a group after the boundary is a pattern", kind: KindRegex, pattern: `^go test( |$)x`, wantCount: 2, wantShape: shapePattern},
	{name: "alternation of whole tokens stops the count", kind: KindRegex, pattern: `^git (push|pull) --force( |$)`, wantCount: 1, wantShape: shapePattern},
	{name: "top-level alternation counts nothing", kind: KindRegex, pattern: `^go test$|^go vet$`, wantCount: 0, wantShape: shapePattern},
	{name: "a case-folded literal after counted tokens stops the count there", kind: KindRegex, pattern: `^go test (?i:foo)`, wantCount: 2, wantShape: shapePattern},
	{name: "case folding stops the count", kind: KindRegex, pattern: `(?i)^go test$`, wantCount: 0, wantShape: shapePattern},
	{name: "multi-line anchor stops the count", kind: KindRegex, pattern: `(?m)^go test$`, wantCount: 0, wantShape: shapePattern},
	{name: "a literal that needs quoting stops the count", kind: KindRegex, pattern: `^go 'a b' test$`, wantCount: 1, wantShape: shapePattern},
	{name: "a star after the tokens", kind: KindRegex, pattern: `^go test .*`, wantCount: 2, wantShape: shapePattern},
	{name: "double space never matches and stops the count", kind: KindRegex, pattern: `^go  test$`, wantCount: 1, wantShape: shapePattern},
	{name: "glob without wildcards is exact", kind: KindGlob, pattern: "go test", wantCount: 2, wantShape: shapeExact},
	{name: "glob with a trailing wildcard token", kind: KindGlob, pattern: "go test *", wantCount: 2, wantShape: shapePattern},
	{name: "glob with a wildcard inside a token", kind: KindGlob, pattern: "go test*", wantCount: 1, wantShape: shapePattern},
	{name: "glob of only a wildcard", kind: KindGlob, pattern: "*", wantCount: 0, wantShape: shapePattern},
	{name: "glob escapes metacharacters into literals", kind: KindGlob, pattern: "git log --oneline", wantCount: 3, wantShape: shapeExact},
}

func TestPatternRank(t *testing.T) {
	t.Parallel()

	for _, tt := range patternRankTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec := ruleSpec{kind: tt.kind, pattern: tt.pattern}
			r := spec.rule(t)

			if r.rankCount != tt.wantCount || r.rankShape != tt.wantShape {
				t.Errorf("rank of %s = (%d, %d), want (%d, %d)", describeRule(&r), r.rankCount, r.rankShape, tt.wantCount, tt.wantShape)
			}
		})
	}
}

type outranksTest struct {
	name string
	a, b rank
	ab   bool
	ba   bool
}

var outranksTests = []outranksTest{
	{name: "kind wins and form ties", a: rank{count: 2, shape: shapeExact, path: true}, b: rank{count: 2, shape: shapePrefix, path: true}, ab: true},
	{name: "global over builtin", a: rank{count: 1, layer: layerGlobal}, b: rank{count: 1, layer: layerBuiltin}, ab: true},
	{name: "layer never overrides a kind", a: rank{count: 2, shape: shapePrefix, layer: layerProject}, b: rank{count: 2, shape: shapeExact, layer: layerGlobal}, ba: true},
}

// TestOutranks pins the pairs the Evaluate cases in rankEvalTests do not reach. Those are a kind win
// when the form ties, a global rule over a builtin one, and a layer that never overrides a kind.
func TestOutranks(t *testing.T) {
	t.Parallel()

	for _, tt := range outranksTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.a.outranks(tt.b); got != tt.ab {
				t.Errorf("a.outranks(b) = %v, want %v", got, tt.ab)
			}
			if got := tt.b.outranks(tt.a); got != tt.ba {
				t.Errorf("b.outranks(a) = %v, want %v", got, tt.ba)
			}
		})
	}
}
