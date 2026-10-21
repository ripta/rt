package approve

import (
	"slices"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/model"
)

func wrapperRule(tokens ...string) Rule { return Rule{Wrapper: tokens, kind: KindWrapper} }

func permitting(r Rule, names ...string) Rule {
	r.PermitUnsafeEnvs = names
	return r
}

type wrapperTest struct {
	name     string
	deny     []Rule
	allow    []Rule
	restrict []Rule

	argv []string
	// canonical overrides the top command's canonical form; nil means argv.
	canonical []string
	env       map[string]string

	// paths maps an inner argv[0] to the path the stub resolver returns for it.
	// An inner command not listed does not resolve.
	paths map[string]string

	want           Decision
	wantRestricted bool
	wantBadEnvs    []string
	wantChain      int
	wantRule       []string
	wantStep       Step
	wantSkips      []SkipReason
}

var wrapperTests = []wrapperTest{
	{
		name:      "inner allow runs",
		allow:     []Rule{wrapperRule("nice", "-n", "19"), prefixRule("make")},
		argv:      []string{"nice", "-n", "19", "make", "test"},
		want:      DecisionRun,
		wantChain: 2,
		wantRule:  []string{"make"},
		wantStep:  StepAllow,
	},
	{
		name:      "unallowed inner command prompts",
		allow:     []Rule{wrapperRule("nice", "-n", "19"), prefixRule("make")},
		argv:      []string{"nice", "-n", "19", "foo"},
		want:      DecisionPrompt,
		wantChain: 2,
		wantStep:  StepNone,
	},
	{
		name:      "denied inner command refuses",
		deny:      []Rule{prefixRule("rm")},
		allow:     []Rule{wrapperRule("nice", "-n", "19"), prefixRule("make")},
		argv:      []string{"nice", "-n", "19", "rm", "x"},
		want:      DecisionRefuse,
		wantChain: 2,
		wantRule:  []string{"rm"},
		wantStep:  StepDeny,
	},
	{
		name:      "inner deny beats an ordinary allow on the full argv",
		deny:      []Rule{prefixRule("rm")},
		allow:     []Rule{exactRule("nice", "-n", "19", "rm", "x"), wrapperRule("nice", "-n", "19")},
		argv:      []string{"nice", "-n", "19", "rm", "x"},
		want:      DecisionRefuse,
		wantChain: 2,
		wantStep:  StepDeny,
	},
	{
		name:      "deny follows a wrapper rule that is not the longest",
		deny:      []Rule{prefixRule("rm")},
		allow:     []Rule{wrapperRule("nice"), wrapperRule("nice", "-n", "19"), prefixRule("make")},
		argv:      []string{"nice", "-n", "19", "rm", "x"},
		want:      DecisionRefuse,
		wantChain: 2,
		wantSkips: []SkipReason{SkipDash},
	},
	{
		name:      "bare-name deny matches an inner layer that does not resolve",
		deny:      []Rule{prefixRule("rm")},
		allow:     []Rule{wrapperRule("nice")},
		argv:      []string{"nice", "rm", "x"},
		want:      DecisionRefuse,
		wantChain: 2,
	},
	{
		name:      "path deny matches an inner layer by basename",
		deny:      []Rule{prefixRule("/usr/bin/foo")},
		allow:     []Rule{wrapperRule("sudo"), prefixRule("foo")},
		argv:      []string{"sudo", "foo"},
		paths:     map[string]string{"foo": "/usr/local/bin/foo"},
		want:      DecisionRefuse,
		wantChain: 2,
	},
	{
		name:  "path deny keeps exact matching on the top layer",
		deny:  []Rule{prefixRule("/usr/bin/foo")},
		allow: []Rule{prefixRule("foo")},
		argv:  []string{"foo"},
		want:  DecisionRun,
	},
	{
		name:           "restrict on the wrapper layer refuses when the peel would prompt",
		allow:          []Rule{wrapperRule("sudo")},
		restrict:       []Rule{prefixRule("sudo")},
		argv:           []string{"sudo", "rm", "-rf", "/var/x"},
		want:           DecisionRefuse,
		wantRestricted: true,
		wantStep:       StepRestrict,
	},
	{
		name:      "a peel that runs carves out of restrict",
		allow:     []Rule{wrapperRule("sudo"), prefixRule("ls")},
		restrict:  []Rule{prefixRule("sudo")},
		argv:      []string{"sudo", "ls"},
		want:      DecisionRun,
		wantChain: 2,
	},
	{
		name:      "nested wrapper peels twice",
		allow:     []Rule{wrapperRule("nice", "-n", "19"), wrapperRule("time"), prefixRule("make")},
		argv:      []string{"nice", "-n", "19", "time", "make", "test"},
		want:      DecisionRun,
		wantChain: 3,
		wantRule:  []string{"make"},
	},
	{
		name:      "nested wrapper refuses a denied innermost command",
		deny:      []Rule{prefixRule("time")},
		allow:     []Rule{wrapperRule("nice", "-n", "19"), wrapperRule("time"), prefixRule("make")},
		argv:      []string{"nice", "-n", "19", "time", "make", "test"},
		want:      DecisionRefuse,
		wantChain: 2,
	},
	{
		name:  "wrapper with nothing after it does not match",
		allow: []Rule{wrapperRule("nice", "-n", "19")},
		argv:  []string{"nice", "-n", "19"},
		want:  DecisionPrompt,
	},
	{
		name:      "dash guard falls through to a shorter correct rule",
		allow:     []Rule{wrapperRule("nohup"), wrapperRule("nohup", "nice", "-n", "19"), wrapperRule("nice", "-n", "19", "--"), prefixRule("make")},
		argv:      []string{"nohup", "nice", "-n", "19", "--", "make", "test"},
		want:      DecisionRun,
		wantChain: 3,
		wantSkips: []SkipReason{SkipDash},
	},
	{
		name:      "dash guard stops the peel and prompts on the full argv",
		allow:     []Rule{wrapperRule("nice", "-n", "19"), prefixRule("make")},
		argv:      []string{"nice", "-n", "19", "--", "make", "test"},
		want:      DecisionPrompt,
		wantSkips: []SkipReason{SkipDash},
	},
	{
		name:      "path-shaped wrapper token matches the canonical path",
		allow:     []Rule{wrapperRule("/usr/bin/nice", "-n", "19"), prefixRule("make")},
		argv:      []string{"nice", "-n", "19", "make"},
		canonical: []string{"/usr/bin/nice", "-n", "19", "make"},
		want:      DecisionRun,
		wantChain: 2,
	},
	{
		name:      "path-shaped wrapper token ignores another binary",
		allow:     []Rule{wrapperRule("/usr/bin/nice", "-n", "19"), prefixRule("make")},
		argv:      []string{"nice", "-n", "19", "make"},
		canonical: []string{"/tmp/evil/nice", "-n", "19", "make"},
		want:      DecisionPrompt,
	},
	{
		name:      "ordinary allow on the full argv decides and reports no chain",
		allow:     []Rule{wrapperRule("nice", "-n", "19"), exactRule("nice", "-n", "19", "make", "install")},
		argv:      []string{"nice", "-n", "19", "make", "install"},
		want:      DecisionRun,
		wantChain: 0,
		wantRule:  []string{"nice", "-n", "19", "make", "install"},
	},
	{
		name:      "wrapper rule never runs a command by itself",
		allow:     []Rule{wrapperRule("nice")},
		argv:      []string{"nice", "foo"},
		want:      DecisionPrompt,
		wantChain: 2,
	},
	{
		name:      "listed program never peels in the allow tier",
		allow:     []Rule{wrapperRule("watch"), prefixRule("make")},
		argv:      []string{"watch", "make", "test"},
		want:      DecisionPrompt,
		wantSkips: []SkipReason{SkipContract},
	},
	{
		name:      "listed program still peels for deny",
		deny:      []Rule{prefixRule("rm")},
		allow:     []Rule{wrapperRule("watch")},
		argv:      []string{"watch", "rm", "x"},
		want:      DecisionRefuse,
		wantChain: 2,
		wantSkips: []SkipReason{SkipContract},
	},
	{
		name:      "listed subcommand prefix stops the peel",
		allow:     []Rule{wrapperRule("docker"), prefixRule("ls")},
		argv:      []string{"docker", "exec", "c", "ls"},
		want:      DecisionPrompt,
		wantSkips: []SkipReason{SkipContract},
	},
	{
		name:      "unlisted subcommand of a listed program peels",
		allow:     []Rule{wrapperRule("docker"), prefixRule("ls")},
		argv:      []string{"docker", "ls"},
		want:      DecisionRun,
		wantChain: 2,
	},
	{
		name:      "alias of a listed program is caught by canonical basename",
		allow:     []Rule{wrapperRule("runner"), prefixRule("make")},
		argv:      []string{"runner", "make"},
		canonical: []string{"/usr/bin/ssh", "make"},
		want:      DecisionPrompt,
		wantSkips: []SkipReason{SkipContract},
	},
	{
		name:        "env override refuses when the inner layer lacks the permit",
		allow:       []Rule{permitting(wrapperRule("uv", "run"), "PATH"), prefixRule("make")},
		argv:        []string{"uv", "run", "make"},
		env:         map[string]string{"PATH": "/x"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantChain:   2,
	},
	{
		name:        "env override refuses when the wrapper lacks the permit",
		allow:       []Rule{wrapperRule("uv", "run"), permitting(prefixRule("make"), "PATH")},
		argv:        []string{"uv", "run", "make"},
		env:         map[string]string{"PATH": "/x"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantChain:   1,
		wantRule:    []string{"uv", "run"},
	},
	{
		name:      "env override runs when every layer permits",
		allow:     []Rule{permitting(wrapperRule("uv", "run"), "PATH"), permitting(prefixRule("make"), "PATH")},
		argv:      []string{"uv", "run", "make"},
		env:       map[string]string{"PATH": "/x"},
		want:      DecisionRun,
		wantChain: 2,
	},
	{
		name: "permits are not pooled within a tied set",
		allow: []Rule{
			permitting(wrapperRule("nice"), "PATH"),
			permitting(wrapperRule("/usr/bin/nice"), "LD_PRELOAD"),
			permitting(prefixRule("make"), "PATH", "LD_PRELOAD"),
		},
		argv:        []string{"nice", "make"},
		canonical:   []string{"/usr/bin/nice", "make"},
		env:         map[string]string{"PATH": "/x", "LD_PRELOAD": "y.so"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"LD_PRELOAD"},
		wantChain:   1,
	},
	{
		name: "one rule in a tied set permitting everything runs",
		allow: []Rule{
			wrapperRule("nice"),
			permitting(wrapperRule("/usr/bin/nice"), "PATH", "LD_PRELOAD"),
			permitting(prefixRule("make"), "PATH", "LD_PRELOAD"),
		},
		argv:      []string{"nice", "make"},
		canonical: []string{"/usr/bin/nice", "make"},
		env:       map[string]string{"PATH": "/x", "LD_PRELOAD": "y.so"},
		want:      DecisionRun,
		wantChain: 2,
	},
	{
		name:        "prompting inner command with a dangerous override refuses",
		allow:       []Rule{permitting(wrapperRule("nice"), "PATH")},
		argv:        []string{"nice", "foo"},
		env:         map[string]string{"PATH": "/x"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantChain:   2,
	},
	{
		name:        "ordinary allow's env refusal is reported over the peel's prompt",
		allow:       []Rule{wrapperRule("nice"), exactRule("nice", "make")},
		argv:        []string{"nice", "make"},
		env:         map[string]string{"PATH": "/x"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
		wantRule:    []string{"nice", "make"},
	},
}

func buildWrapperRuleset(tt wrapperTest, reverse bool) *Ruleset {
	rs := &Ruleset{Mode: ModeEnforce, Deny: slices.Clone(tt.deny), Allow: slices.Clone(tt.allow), Restrict: slices.Clone(tt.restrict)}
	if reverse {
		slices.Reverse(rs.Allow)
	}

	for i := range rs.Deny {
		compileMatch(&rs.Deny[i], "")
	}
	for i := range rs.Allow {
		compileMatch(&rs.Allow[i], "")
	}
	for i := range rs.Restrict {
		compileMatch(&rs.Restrict[i], "")
	}

	return rs
}

// stubResolver resolves an inner argv[0] through a fixed table and records every
// argv it is asked about.
func stubResolver(paths map[string]string, calls *[]string) Resolver {
	return func(argv []string) *model.Resolution {
		*calls = append(*calls, strings.Join(argv, " "))
		if p, ok := paths[argv[0]]; ok {
			return &model.Resolution{Argv: argv, Resolved: p, Canonical: p}
		}

		return &model.Resolution{Argv: argv}
	}
}

func TestEvaluateWrapper(t *testing.T) {
	t.Parallel()

	for _, tt := range wrapperTests {
		for _, reverse := range []bool{false, true} {
			name := tt.name
			if reverse {
				name += " reversed"
			}

			t.Run(name, func(t *testing.T) {
				t.Parallel()

				rs := buildWrapperRuleset(tt, reverse)
				subj := Subject{Argv: tt.argv, Canonical: tt.canonical}
				if subj.Canonical == nil {
					subj.Canonical = tt.argv
				}

				var calls []string
				got := rs.Evaluate(subj, tt.env, stubResolver(tt.paths, &calls))

				if got.Decision != tt.want {
					t.Fatalf("decision = %v, want %v (chain %d)", got.Decision, tt.want, len(got.Chain))
				}
				if got.Restricted != tt.wantRestricted {
					t.Errorf("restricted = %v, want %v", got.Restricted, tt.wantRestricted)
				}
				if !slices.Equal(got.BadEnvs, tt.wantBadEnvs) {
					t.Errorf("badEnvs = %v, want %v", got.BadEnvs, tt.wantBadEnvs)
				}
				if len(got.Chain) != tt.wantChain {
					t.Errorf("chain length = %d, want %d", len(got.Chain), tt.wantChain)
				}
				if tt.wantRule != nil {
					if got.Rule == nil || !slices.Equal(tokensOf(got.Rule), tt.wantRule) {
						t.Errorf("rule = %v, want tokens %v", got.Rule, tt.wantRule)
					}
				}
				if tt.wantStep != StepNone && len(got.Chain) > 0 {
					if last := got.Chain[len(got.Chain)-1]; last.Step != tt.wantStep {
						t.Errorf("last step = %v, want %v", last.Step, tt.wantStep)
					}
				}

				var reasons []SkipReason
				for _, sk := range got.Skipped {
					reasons = append(reasons, sk.Reason)
				}
				if !slices.Equal(reasons, tt.wantSkips) {
					t.Errorf("skips = %v, want %v", reasons, tt.wantSkips)
				}

				seen := make(map[string]bool)
				for _, c := range calls {
					if seen[c] {
						t.Errorf("resolver asked about %q more than once", c)
					}
					seen[c] = true
				}
			})
		}
	}
}

func TestEvaluateWrapperRecordsPinningInputs(t *testing.T) {
	t.Parallel()

	rs := buildWrapperRuleset(wrapperTest{
		allow: []Rule{wrapperRule("uv", "run"), prefixRule("/usr/bin/git"), prefixRule("python")},
	}, false)

	var calls []string
	paths := map[string]string{"git": "/usr/bin/git", "python": "/usr/bin/python"}

	v := rs.Evaluate(identitySubject([]string{"uv", "run", "git", "status"}), nil, stubResolver(paths, &calls))
	if v.Decision != DecisionRun || len(v.Chain) != 2 {
		t.Fatalf("verdict = %v with chain %d, want run with 2 layers", v.Decision, len(v.Chain))
	}
	inner := v.Chain[1]
	if !inner.PathForm || len(inner.Candidates) != 1 || inner.Resolution.RulePath() != "/usr/bin/git" {
		t.Errorf("inner layer = %+v, want one path-form candidate resolving to /usr/bin/git", inner)
	}

	v = rs.Evaluate(identitySubject([]string{"uv", "run", "python", "-m", "pytest"}), nil, stubResolver(paths, &calls))
	if inner = v.Chain[1]; inner.PathForm {
		t.Errorf("bare-name candidate reported as path form: %+v", inner)
	}

	top := v.Chain[0]
	if top.Step != StepPeel || len(top.Wrappers) != 1 || top.Rule == nil {
		t.Errorf("top layer = %+v, want a peel through one wrapper rule", top)
	}
}

func TestEvaluateWrapperResolvesEachOffsetOnce(t *testing.T) {
	t.Parallel()

	rs := buildWrapperRuleset(wrapperTest{
		allow: []Rule{wrapperRule("a"), wrapperRule("a", "b"), wrapperRule("a", "b", "c"), wrapperRule("/x/a", "b")},
	}, false)

	var calls []string
	subj := Subject{Argv: []string{"a", "b", "c", "d"}, Canonical: []string{"/x/a", "b", "c", "d"}}
	rs.Evaluate(subj, nil, stubResolver(nil, &calls))

	want := []string{"b c d", "c d", "d"}
	slices.Sort(calls)
	slices.Sort(want)
	if !slices.Equal(calls, want) {
		t.Errorf("resolver calls = %v, want %v", calls, want)
	}
}
