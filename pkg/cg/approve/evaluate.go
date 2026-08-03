package approve

// Verdict is Evaluate's outcome: a Match decision plus the environment-override
// disposition layered on top of it. BadEnvs is non-nil when Decision was forced
// to DecisionRefuse by a dangerous environment override that no rule permits.
// Rule and Restricted carry the same meaning as in MatchResult; Rule is nil
// when the refusal came from the env gate on an unmatched command rather than
// from a deny, allow, or restrict rule.
type Verdict struct {
	Decision   Decision
	Rule       *Rule
	Restricted bool
	BadEnvs    []string
}

// Evaluate runs Match and then applies the dangerous-environment gate on top of
// its verdict.
//
// A DecisionRun from a matched allow rule is downgraded to DecisionRefuse when
// env sets a variable the rule's PermitUnsafeEnvs does not list; allow-all and
// an unmatched allow rule (Rule nil) are not subject to this, since there is no
// rule to consult. A DecisionPrompt is downgraded the same way: a command with
// no matching rule has no PermitUnsafeEnvs list to grant an exemption, so any
// dangerous override refuses it outright rather than reaching a prompt.
// DecisionRefuse from Match passes through unchanged, since a deny or restrict
// match already refuses regardless of env.
func (rs *Ruleset) Evaluate(subj Subject, env map[string]string) Verdict {
	res := rs.Match(subj)
	v := Verdict{Decision: res.Decision, Rule: res.Rule, Restricted: res.Restricted}

	switch res.Decision {
	case DecisionRun:
		if res.Rule != nil {
			if bad := res.Rule.DisallowedEnvs(env); len(bad) > 0 {
				v.Decision = DecisionRefuse
				v.BadEnvs = bad
			}
		}
	case DecisionPrompt:
		if bad := (&Rule{}).DisallowedEnvs(env); len(bad) > 0 {
			v.Decision = DecisionRefuse
			v.BadEnvs = bad
		}
	}

	return v
}
