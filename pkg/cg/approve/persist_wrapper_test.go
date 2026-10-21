package approve

import (
	"os"
	"strings"
	"testing"
)

func TestWrapperRulesRoundTripThroughRemember(t *testing.T) {
	t.Parallel()

	content := "version: 1\nallow:\n  - wrapper: [nice, -n, \"19\"]\n    permit_unsafe_envs: [PATH]\n  - wrapper: [nice, -n, \"19\"]\n  - prefix: [nice]\n"
	root := writeProject(t, content)
	s := loadProject(t, root)

	if err := s.AppendProjectAllowPrefix([]string{"make", "test"}, WriteDirect); err != nil {
		t.Fatalf("AppendProjectAllowPrefix: %v", err)
	}

	raw, err := os.ReadFile(ProjectPath(root, ""))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	got := string(raw)

	if n := strings.Count(got, "wrapper: [nice, -n, \"19\"]"); n != 1 {
		t.Errorf("expected the duplicate wrapper rules to collapse to one, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "permit_unsafe_envs: [PATH]") {
		t.Errorf("the kept wrapper rule lost its permit:\n%s", got)
	}

	reloaded := loadProject(t, root)
	var wrappers int
	for _, r := range reloaded.Ruleset().Allow {
		if r.Kind() == KindWrapper {
			wrappers++
		}
	}
	if wrappers != 1 {
		t.Errorf("reloaded wrapper rules = %d, want 1", wrappers)
	}
}

func TestWrapperRuleSurvivesReloadMerge(t *testing.T) {
	t.Parallel()

	root := writeProject(t, "version: 1\nallow:\n  - wrapper: [nice]\n  - prefix: [make]\n")
	s := loadProject(t, root)

	disk := "version: 1\nallow:\n  - prefix: [make]\n  - prefix: [npm, ci]\n"
	if err := os.WriteFile(ProjectPath(root, ""), []byte(disk), 0o600); err != nil {
		t.Fatalf("hand edit: %v", err)
	}

	if err := s.AppendProjectAllowPrefix([]string{"go", "vet"}, WriteReloadMerge); err != nil {
		t.Fatalf("reload-merge append: %v", err)
	}

	reloaded := loadProject(t, root)
	if got := reloaded.Ruleset().Evaluate(identitySubject([]string{"nice", "make"}), nil, nil); got.Decision != DecisionRun {
		t.Errorf("wrapped command after reload-merge = %v, want run", got.Decision)
	}
	if got := reloaded.Ruleset().Evaluate(identitySubject([]string{"nice", "npm", "ci"}), nil, nil); got.Decision != DecisionRun {
		t.Errorf("wrapped on-disk rule after reload-merge = %v, want run", got.Decision)
	}
}

func TestRuleEqualWrapper(t *testing.T) {
	t.Parallel()

	a := Rule{Wrapper: []string{"nice"}, kind: KindWrapper}
	b := Rule{Wrapper: []string{"nice"}, kind: KindWrapper, PermitUnsafeEnvs: []string{"PATH"}}
	c := Rule{Prefix: []string{"nice"}, kind: KindPrefix}
	d := Rule{Wrapper: []string{"nice", "-n"}, kind: KindWrapper}

	if !ruleEqual(&a, &b) {
		t.Errorf("wrapper rules with the same tokens should be equal")
	}
	if ruleEqual(&a, &c) {
		t.Errorf("a wrapper rule should not equal a prefix rule with the same tokens")
	}
	if ruleEqual(&a, &d) {
		t.Errorf("wrapper rules with different tokens should differ")
	}
}
