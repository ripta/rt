package approve

import "testing"

type builtinTest struct {
	name string
	argv []string
	want Decision
}

var builtinTests = []builtinTest{
	// bare shells, spelled three ways
	{name: "sh", argv: []string{"sh", "-c", "echo hi"}, want: DecisionRefuse},
	{name: "sh absolute", argv: []string{"/bin/sh", "-c", "echo hi"}, want: DecisionRefuse},
	{name: "sh relative", argv: []string{"./sh", "-c", "echo hi"}, want: DecisionRefuse},
	{name: "bash", argv: []string{"bash"}, want: DecisionRefuse},
	{name: "bash absolute", argv: []string{"/usr/bin/bash", "script.sh"}, want: DecisionRefuse},
	{name: "zsh", argv: []string{"zsh"}, want: DecisionRefuse},
	{name: "env", argv: []string{"env", "FOO=1", "make"}, want: DecisionRefuse},
	{name: "xargs", argv: []string{"xargs", "rm"}, want: DecisionRefuse},

	// interpreter inline-code forms
	{name: "python -c", argv: []string{"python", "-c", "import os"}, want: DecisionRefuse},
	{name: "python -c absolute", argv: []string{"/usr/bin/python", "-c", "x"}, want: DecisionRefuse},
	{name: "python3 -c", argv: []string{"python3", "-c", "x"}, want: DecisionRefuse},
	{name: "node -e", argv: []string{"node", "-e", "x"}, want: DecisionRefuse},
	{name: "node --eval", argv: []string{"node", "--eval", "x"}, want: DecisionRefuse},
	{name: "perl -e", argv: []string{"perl", "-e", "x"}, want: DecisionRefuse},
	{name: "ruby -e", argv: []string{"ruby", "-e", "x"}, want: DecisionRefuse},

	// not the inline-code form: program alone is not denied
	{name: "bare python prompts", argv: []string{"python", "script.py"}, want: DecisionPrompt},
	{name: "python -c at wrong position", argv: []string{"python", "script.py", "-c"}, want: DecisionPrompt},
	{name: "node check flag not eval", argv: []string{"node", "-c", "x"}, want: DecisionPrompt},
}

func TestBuiltinDeny(t *testing.T) {
	t.Parallel()

	rs := &Ruleset{Mode: ModeEnforce, Deny: builtinDenyRules()}

	for _, tt := range builtinTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rs.Match(tt.argv); got.Decision != tt.want {
				t.Errorf("Match(%v) = %v, want %v", tt.argv, got.Decision, tt.want)
			}
		})
	}
}

func TestBuiltinDenyNotOverridable(t *testing.T) {
	t.Parallel()

	// A project allow for sh cannot re-allow it: the built-in deny leads the
	// merged deny list and deny is evaluated first.
	rs := &Ruleset{
		Mode:  ModeEnforce,
		Deny:  builtinDenyRules(),
		Allow: []Rule{prefixRule("sh")},
	}
	if got := rs.Match([]string{"sh", "-c", "x"}); got.Decision != DecisionRefuse {
		t.Errorf("Match(sh -c x) = %v, want refuse (builtin deny not overridable)", got.Decision)
	}
}
