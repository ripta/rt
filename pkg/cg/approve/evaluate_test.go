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

			got := rs.Evaluate(identitySubject(tt.argv), tt.env)
			if got.Decision != tt.want {
				t.Fatalf("Evaluate() decision = %v, want %v", got.Decision, tt.want)
			}
			if !slices.Equal(got.BadEnvs, tt.wantBadEnvs) {
				t.Errorf("Evaluate() badEnvs = %v, want %v", got.BadEnvs, tt.wantBadEnvs)
			}
			if tt.wantRuleNil && got.Rule != nil {
				t.Errorf("Evaluate() rule = %v, want nil", got.Rule)
			}
		})
	}
}

func TestEvaluateEmptyArgv(t *testing.T) {
	t.Parallel()

	rs := &Ruleset{Mode: ModeEnforce}
	got := rs.Evaluate(Subject{}, nil)
	if got.Decision != DecisionRefuse {
		t.Errorf("Evaluate(empty) = %v, want refuse", got.Decision)
	}
}
