package approvecmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/approve"
)

const wrapperRules = "version: 1\nallow:\n  - wrapper: [nice, -n, \"19\"]\n  - prefix: [echo]\n  - wrapper: [watch]\n"

func TestCheckWrapperRunsInnerAllow(t *testing.T) {
	out, err := runCheck(t, t.TempDir(), wrapperRules, "--", "nice", "-n", "19", "echo", "hi")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}

	for _, want := range []string{"run: nice -n 19 echo hi", "chain:", "[0] nice -n 19 echo hi", "peeled by wrapper [nice, -n, 19]", "[1] echo hi"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCheckWrapperPromptsForUnallowedInner(t *testing.T) {
	out, err := runCheck(t, t.TempDir(), wrapperRules, "--", "nice", "-n", "19", "unknown-tool")
	if exitCode(t, err) != 3 {
		t.Fatalf("exit code = %d, want 3; out = %s", exitCode(t, err), out)
	}
	if !strings.Contains(out, "[1] unknown-tool") {
		t.Errorf("output should show the inner layer:\n%s", out)
	}
}

func TestCheckWrapperJSONCarriesChain(t *testing.T) {
	out, err := runCheck(t, t.TempDir(), wrapperRules, "--output", "json", "--", "nice", "-n", "19", "echo", "hi")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}

	var res checkResult
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("unmarshalling %q: %v", out, jsonErr)
	}
	if len(res.Chain) != 2 {
		t.Fatalf("chain length = %d, want 2: %s", len(res.Chain), out)
	}
	if res.Chain[0].Wrappers[0].Kind != "wrapper" || res.Chain[1].Step != "allow" {
		t.Errorf("chain = %+v", res.Chain)
	}
	if res.Rule == nil || res.Rule.Pattern != "[echo]" {
		t.Errorf("rule = %+v, want the inner [echo] rule", res.Rule)
	}
}

func TestCheckWrapperExplainsWhyItDidNotPeel(t *testing.T) {
	out, err := runCheck(t, t.TempDir(), wrapperRules, "--", "watch", "echo", "hi")
	if exitCode(t, err) != 3 {
		t.Fatalf("exit code = %d, want 3; out = %s", exitCode(t, err), out)
	}
	if !strings.Contains(out, "wrapper [watch] did not peel at offset 0") || !strings.Contains(out, "breaks the wrapper contract") {
		t.Errorf("output should explain the listed program:\n%s", out)
	}

	out, err = runCheck(t, t.TempDir(), wrapperRules, "--", "nice", "-n", "19", "--", "echo", "hi")
	if exitCode(t, err) != 3 {
		t.Fatalf("exit code = %d, want 3; out = %s", exitCode(t, err), out)
	}
	if !strings.Contains(out, "starts with '-'") {
		t.Errorf("output should explain the dash guard:\n%s", out)
	}
}

func TestCheckWrapperLabelsTheRuleThatRefusedTheEnv(t *testing.T) {
	rules := "version: 1\nallow:\n  - wrapper: [nice]\n  - prefix: [echo]\n    permit_unsafe_envs: [PATH]\n"

	out, err := runCheck(t, t.TempDir(), rules, "--env", "PATH=/x", "--", "nice", "echo", "hi")
	if exitCode(t, err) != 1 {
		t.Fatalf("exit code = %d, want 1; out = %s", exitCode(t, err), out)
	}
	if !strings.Contains(out, "env refusal: wrapper [nice]") || strings.Contains(out, "allow [nice]") {
		t.Errorf("output should label the refusing rule as a wrapper:\n%s", out)
	}

	out, _ = runCheck(t, t.TempDir(), wrapperRules, "--", "nice", "-n", "19", "echo", "hi")
	if !strings.Contains(out, "peel runs, permitted by wrapper [nice, -n, 19]") || !strings.Contains(out, "        allow [echo]") {
		t.Errorf("output should label peel and allow layers:\n%s", out)
	}
}

func TestCheckWrapperShell(t *testing.T) {
	out, err := runCheck(t, t.TempDir(), wrapperRules, "--shell", "--", "nice -n 19 echo hi && echo done")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}
	if !strings.Contains(out, "chain:") {
		t.Errorf("shell output should carry the chain:\n%s", out)
	}
}

const tiedRules = "version: 1\nallow:\n  - wrapper: [nice]\n  - prefix: [echo, hi]\n  - regex: '^echo hi( |$)'\n    as_basename: true\n    permit_unsafe_envs: [PATH]\n"

func TestCheckNamesEveryRuleOfATie(t *testing.T) {
	out, err := runCheck(t, t.TempDir(), tiedRules, "--output", "json", "--", "echo", "hi")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}

	var res checkResult
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("unmarshalling %q: %v", out, jsonErr)
	}
	if len(res.Deciding) != 2 || res.Deciding[0].Kind != "prefix" || res.Deciding[1].Kind != "regex" {
		t.Errorf("deciding = %+v, want the prefix and the regex rule", res.Deciding)
	}
	if res.Rule == nil || res.Rule.Kind != "prefix" {
		t.Errorf("rule = %+v, want the first tied rule, the prefix", res.Rule)
	}

	text, _ := runCheck(t, t.TempDir(), tiedRules, "--", "echo", "hi")
	for _, want := range []string{"decided together by 2 equally specific rules", "prefix [echo, hi]", `regex "^echo hi( |$)"`} {
		if !strings.Contains(text, want) {
			t.Errorf("text output missing %q:\n%s", want, text)
		}
	}
}

func TestCheckChainCandidatesDescribeTheDecidingSet(t *testing.T) {
	rules := "version: 1\nallow:\n  - wrapper: [nice]\n  - prefix: [echo]\n"

	out, err := runCheck(t, t.TempDir(), rules, "--output", "json", "--", "nice", "echo", "hi")
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; out = %s", exitCode(t, err), out)
	}

	var res checkResult
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("unmarshalling %q: %v", out, jsonErr)
	}
	if len(res.Chain) != 2 || len(res.Chain[1].Candidates) != 1 || res.Chain[1].Candidates[0].Pattern != "[echo]" || res.Chain[1].PathForm {
		t.Errorf("chain = %+v, want one bare-name candidate [echo]", res.Chain)
	}
}

// TestRuleSectionForTieRefusal builds the verdicts by hand, since no allow field exists yet that
// two tied rules can differ in. A refusal with no blocked env and no deny must still read as an
// allow refusal when equally specific allows disagree, at the top or behind a wrapper.
func TestRuleSectionForTieRefusal(t *testing.T) {
	_, doc, err := approve.ParseDocument([]byte("version: 1\nallow:\n  - prefix: [echo]\n"))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	rule := &doc.Allow[0]
	top := approve.Verdict{Decision: approve.DecisionRefuse, Rule: rule, Step: approve.StepTieRefusal}
	if got := ruleSectionFor(top); got != "allow" {
		t.Errorf("section of a top-layer tie refusal = %q, want allow", got)
	}

	chained := approve.Verdict{
		Decision: approve.DecisionRefuse,
		Rule:     rule,
		Step:     approve.StepInner,
		Chain:    []approve.ChainLayer{{Step: approve.StepInner}, {Offset: 1, Step: approve.StepTieRefusal}},
	}
	if got := ruleSectionFor(chained); got != "allow" {
		t.Errorf("section of a chained tie refusal = %q, want allow", got)
	}

	deny := approve.Verdict{Decision: approve.DecisionRefuse, Rule: rule, Step: approve.StepDeny}
	if got := ruleSectionFor(deny); got != "deny" {
		t.Errorf("section of a deny = %q, want deny", got)
	}
}
