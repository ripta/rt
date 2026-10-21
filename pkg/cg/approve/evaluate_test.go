package approve

import (
	"slices"
	"testing"
)

type evaluateTest struct {
	name string

	mode  Mode
	deny  []Rule
	allow []Rule

	argv []string
	env  map[string]string

	want        Decision
	wantBadEnvs []string
	wantRuleNil bool

	// wantRuleTokens, when set, is the exact or prefix tokens of the rule the
	// verdict reports.
	wantRuleTokens []string
}

func permitRule(kind RuleKind, permit []string, tokens ...string) Rule {
	r := Rule{kind: kind, PermitUnsafeEnvs: permit}
	if kind == KindExact {
		r.Exact = tokens
	} else {
		r.Prefix = tokens
	}

	return r
}

func ruleTokens(r *Rule) []string {
	if r.kind == KindExact {
		return r.Exact
	}

	return r.Prefix
}

var evaluateTests = []evaluateTest{
	{
		name:  "allow match, no env",
		allow: []Rule{prefixRule("git")},
		argv:  []string{"git", "status"},
		want:  DecisionRun,
	},
	{
		name:  "allow match, permitted env",
		allow: []Rule{{Prefix: []string{"make"}, kind: KindPrefix, PermitUnsafeEnvs: []string{"PATH"}}},
		argv:  []string{"make"},
		env:   map[string]string{"PATH": "/custom"},
		want:  DecisionRun,
	},
	{
		name:        "allow match, unpermitted env downgrades to refuse",
		allow:       []Rule{prefixRule("make")},
		argv:        []string{"make"},
		env:         map[string]string{"LD_PRELOAD": "evil.so"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"LD_PRELOAD"},
	},
	{
		name: "permitting allow after an unpermitting one runs",
		allow: []Rule{
			permitRule(KindExact, nil, "make", "test"),
			permitRule(KindPrefix, []string{"PATH"}, "make"),
		},
		argv:           []string{"make", "test"},
		env:            map[string]string{"PATH": "/custom"},
		want:           DecisionRun,
		wantRuleTokens: []string{"make"},
	},
	{
		name: "permitting allow before an unpermitting one runs",
		allow: []Rule{
			permitRule(KindPrefix, nil, "make"),
			permitRule(KindExact, []string{"PATH"}, "make", "test"),
		},
		argv:           []string{"make", "test"},
		env:            map[string]string{"PATH": "/custom"},
		want:           DecisionRun,
		wantRuleTokens: []string{"make", "test"},
	},
	{
		name: "no matching allow permits, refuse names the first match",
		allow: []Rule{
			permitRule(KindPrefix, nil, "make"),
			permitRule(KindExact, nil, "make", "test"),
		},
		argv:           []string{"make", "test"},
		env:            map[string]string{"PATH": "/custom"},
		want:           DecisionRefuse,
		wantBadEnvs:    []string{"PATH"},
		wantRuleTokens: []string{"make"},
	},
	{
		name: "a permitting allow that does not match is ignored",
		allow: []Rule{
			permitRule(KindPrefix, nil, "make"),
			permitRule(KindExact, []string{"PATH"}, "make", "install"),
		},
		argv:        []string{"make", "test"},
		env:         map[string]string{"PATH": "/custom"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"PATH"},
	},
	{
		name: "permits split across rules do not combine",
		allow: []Rule{
			permitRule(KindPrefix, []string{"PATH"}, "make"),
			permitRule(KindExact, []string{"LD_PRELOAD"}, "make", "test"),
		},
		argv:        []string{"make", "test"},
		env:         map[string]string{"PATH": "/custom", "LD_PRELOAD": "x.so"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"LD_PRELOAD"},
	},
	{
		name: "allow-all bypasses the env gate",
		mode: ModeAllowAll,
		argv: []string{"anything"},
		env:  map[string]string{"LD_PRELOAD": "evil.so"},
		want: DecisionRun,
	},
	{
		name: "no match, no env, stays prompt",
		argv: []string{"unmatched"},
		want: DecisionPrompt,
	},
	{
		name:        "no match, bad env refuses outright",
		argv:        []string{"unmatched"},
		env:         map[string]string{"LD_PRELOAD": "evil.so"},
		want:        DecisionRefuse,
		wantBadEnvs: []string{"LD_PRELOAD"},
		wantRuleNil: true,
	},
	{
		name: "deny match ignores env",
		deny: []Rule{prefixRule("rm")},
		argv: []string{"rm", "-rf", "/"},
		env:  map[string]string{"LD_PRELOAD": "evil.so"},
		want: DecisionRefuse,
	},
}

func TestEvaluate(t *testing.T) {
	t.Parallel()

	for _, tt := range evaluateTests {
		t.Run(tt.name, func(t *testing.T) {
			rs := &Ruleset{Mode: tt.mode, Deny: slices.Clone(tt.deny), Allow: slices.Clone(tt.allow)}
			for i := range rs.Deny {
				compileMatch(&rs.Deny[i], "")
			}
			for i := range rs.Allow {
				compileMatch(&rs.Allow[i], "")
			}

			got := rs.Evaluate(identitySubject(tt.argv), tt.env, nil)
			if got.Decision != tt.want {
				t.Fatalf("Evaluate() decision = %v, want %v", got.Decision, tt.want)
			}
			if !slices.Equal(got.BadEnvs, tt.wantBadEnvs) {
				t.Errorf("Evaluate() badEnvs = %v, want %v", got.BadEnvs, tt.wantBadEnvs)
			}
			if tt.wantRuleNil && got.Rule != nil {
				t.Errorf("Evaluate() rule = %v, want nil", got.Rule)
			}
			if tt.wantRuleTokens != nil {
				if got.Rule == nil {
					t.Fatalf("Evaluate() rule = nil, want %v", tt.wantRuleTokens)
				}
				if !slices.Equal(ruleTokens(got.Rule), tt.wantRuleTokens) {
					t.Errorf("Evaluate() rule = %v, want %v", ruleTokens(got.Rule), tt.wantRuleTokens)
				}
			}
		})
	}
}

func TestEvaluateEmptyArgv(t *testing.T) {
	t.Parallel()

	rs := &Ruleset{Mode: ModeEnforce}
	got := rs.Evaluate(Subject{}, nil, nil)
	if got.Decision != DecisionRefuse {
		t.Errorf("Evaluate(empty) = %v, want refuse", got.Decision)
	}
}
