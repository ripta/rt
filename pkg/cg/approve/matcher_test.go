package approve

import "testing"

// rule constructors mirror what the loader produces: kind is set and glob/regex
// patterns are compiled.

func exactRule(tokens ...string) Rule { return Rule{Exact: tokens, kind: KindExact} }

func prefixRule(tokens ...string) Rule { return Rule{Prefix: tokens, kind: KindPrefix} }

func globRule(t *testing.T, pattern string) Rule {
	t.Helper()
	r := Rule{Glob: pattern, kind: KindGlob}
	if err := compileRule(&r); err != nil {
		t.Fatalf("compiling glob %q: %v", pattern, err)
	}

	return r
}

func regexRule(t *testing.T, pattern string) Rule {
	t.Helper()
	r := Rule{Regex: pattern, kind: KindRegex}
	if err := compileRule(&r); err != nil {
		t.Fatalf("compiling regex %q: %v", pattern, err)
	}

	return r
}

type matchTest struct {
	name        string
	mode        Mode
	deny        []Rule
	allow       []Rule
	argv        []string
	want        Decision
	wantMessage string
}

func TestMatch(t *testing.T) {
	t.Parallel()

	// glob/regex rules need compilation, so cases that use them build the
	// ruleset inside the loop via helpers; static cases are listed in the table.
	staticTests := []matchTest{
		// mode short-circuits
		{name: "allow-all overrides deny", mode: ModeAllowAll, deny: []Rule{prefixRule("rm")}, argv: []string{"rm", "-rf", "/"}, want: DecisionRun},
		{name: "deny-all overrides allow", mode: ModeDenyAll, allow: []Rule{prefixRule("git")}, argv: []string{"git", "status"}, want: DecisionRefuse},

		// exact
		{name: "exact match", allow: []Rule{exactRule("git", "status")}, argv: []string{"git", "status"}, want: DecisionRun},
		{name: "exact longer argv no match", allow: []Rule{exactRule("git", "status")}, argv: []string{"git", "status", "-s"}, want: DecisionPrompt},
		{name: "exact different arg no match", allow: []Rule{exactRule("git", "status")}, argv: []string{"git", "log"}, want: DecisionPrompt},

		// prefix
		{name: "prefix match with extra args", allow: []Rule{prefixRule("go", "test")}, argv: []string{"go", "test", "./..."}, want: DecisionRun},
		{name: "prefix argv shorter no match", allow: []Rule{prefixRule("go", "test")}, argv: []string{"go"}, want: DecisionPrompt},
		{name: "prefix differing token no match", allow: []Rule{prefixRule("go", "test")}, argv: []string{"go", "vet"}, want: DecisionPrompt},

		// deny program-token normalization (basename broadens)
		{name: "deny sh plain", deny: []Rule{prefixRule("sh")}, argv: []string{"sh", "-c", "x"}, want: DecisionRefuse},
		{name: "deny sh absolute path", deny: []Rule{prefixRule("sh")}, argv: []string{"/bin/sh", "-c", "x"}, want: DecisionRefuse},
		{name: "deny sh relative path", deny: []Rule{prefixRule("sh")}, argv: []string{"./sh", "-c", "x"}, want: DecisionRefuse},

		// allow program-token normalization (slash in argv0 falls through)
		{name: "allow make plain", allow: []Rule{prefixRule("make")}, argv: []string{"make"}, want: DecisionRun},
		{name: "allow make planted absolute path", allow: []Rule{prefixRule("make")}, argv: []string{"/tmp/evil/make"}, want: DecisionPrompt},
		{name: "allow make relative path", allow: []Rule{prefixRule("make")}, argv: []string{"./make"}, want: DecisionPrompt},

		// rule token with slash matches literally
		{name: "deny literal path match", deny: []Rule{prefixRule("/bin/sh")}, argv: []string{"/bin/sh"}, want: DecisionRefuse},
		{name: "deny literal path no bare match", deny: []Rule{prefixRule("/bin/sh")}, argv: []string{"sh"}, want: DecisionPrompt},
		{name: "deny literal path other path no match", deny: []Rule{prefixRule("/bin/sh")}, argv: []string{"/usr/bin/sh"}, want: DecisionPrompt},
		{name: "allow literal path exact", allow: []Rule{exactRule("/usr/bin/git", "status")}, argv: []string{"/usr/bin/git", "status"}, want: DecisionRun},
		{name: "allow literal path bare no match", allow: []Rule{exactRule("/usr/bin/git", "status")}, argv: []string{"git", "status"}, want: DecisionPrompt},

		// argv[1:] compares byte-exact, no basename normalization
		{name: "deny rm -rf exact tail", deny: []Rule{prefixRule("rm", "-rf")}, argv: []string{"rm", "-rf"}, want: DecisionRefuse},
		{name: "deny rm -rf with target", deny: []Rule{prefixRule("rm", "-rf")}, argv: []string{"rm", "-rf", "/tmp"}, want: DecisionRefuse},
		{name: "deny rm -rf differing flag", deny: []Rule{prefixRule("rm", "-rf")}, argv: []string{"rm", "-r"}, want: DecisionPrompt},

		// deny precedence and layering
		{name: "deny wins over allow", deny: []Rule{prefixRule("git", "push", "--force")}, allow: []Rule{prefixRule("git")}, argv: []string{"git", "push", "--force"}, want: DecisionRefuse},
		{name: "allow when no deny matches", deny: []Rule{prefixRule("git", "push", "--force")}, allow: []Rule{prefixRule("git")}, argv: []string{"git", "status"}, want: DecisionRun},
		{name: "no match prompts", allow: []Rule{prefixRule("go")}, argv: []string{"cargo", "build"}, want: DecisionPrompt},

		// deny message propagation
		{name: "deny message surfaced", deny: []Rule{{Prefix: []string{"rm", "-rf"}, Message: "delete specific paths", kind: KindPrefix}}, argv: []string{"rm", "-rf", "/"}, want: DecisionRefuse, wantMessage: "delete specific paths"},
	}

	for _, tt := range staticTests {
		t.Run(tt.name, func(t *testing.T) {
			runMatchCase(t, tt)
		})
	}
}

func TestMatchPatterns(t *testing.T) {
	t.Parallel()

	tests := []matchTest{
		// glob is fully anchored
		{name: "glob trailing star matches tail", allow: []Rule{globRule(t, "kubectl get *")}, argv: []string{"kubectl", "get", "pods", "-n", "x"}, want: DecisionRun},
		{name: "glob different verb no match", allow: []Rule{globRule(t, "kubectl get *")}, argv: []string{"kubectl", "describe", "pods"}, want: DecisionPrompt},
		{name: "glob no wildcard exact", allow: []Rule{globRule(t, "make")}, argv: []string{"make"}, want: DecisionRun},
		{name: "glob no wildcard rejects extra", allow: []Rule{globRule(t, "make")}, argv: []string{"make", "build"}, want: DecisionPrompt},
		{name: "glob over quoted join", allow: []Rule{globRule(t, "git commit -m *")}, argv: []string{"git", "commit", "-m", "hello world"}, want: DecisionRun},

		// regex is unanchored search
		{name: "regex sudo leading", deny: []Rule{regexRule(t, `(^|\s)sudo(\s|$)`)}, argv: []string{"sudo", "rm"}, want: DecisionRefuse},
		{name: "regex sudo substring no match", deny: []Rule{regexRule(t, `(^|\s)sudo(\s|$)`)}, argv: []string{"mysudo", "foo"}, want: DecisionPrompt},
		{name: "regex sudo mid line", deny: []Rule{regexRule(t, `(^|\s)sudo(\s|$)`)}, argv: []string{"echo", "sudo", "hi"}, want: DecisionRefuse},
		{name: "regex npm test", allow: []Rule{regexRule(t, `^npm (run )?(test|lint)$`)}, argv: []string{"npm", "test"}, want: DecisionRun},
		{name: "regex npm run lint", allow: []Rule{regexRule(t, `^npm (run )?(test|lint)$`)}, argv: []string{"npm", "run", "lint"}, want: DecisionRun},
		{name: "regex npm install no match", allow: []Rule{regexRule(t, `^npm (run )?(test|lint)$`)}, argv: []string{"npm", "install"}, want: DecisionPrompt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runMatchCase(t, tt)
		})
	}
}

func runMatchCase(t *testing.T, tt matchTest) {
	t.Helper()
	rs := &Ruleset{Mode: tt.mode, Deny: tt.deny, Allow: tt.allow}
	got := rs.Match(tt.argv)
	if got.Decision != tt.want {
		t.Fatalf("Match(%v) decision = %v, want %v", tt.argv, got.Decision, tt.want)
	}
	if tt.wantMessage != "" {
		if got.Rule == nil {
			t.Fatalf("Match(%v) returned nil rule, want message %q", tt.argv, tt.wantMessage)
		}
		if got.Rule.Message != tt.wantMessage {
			t.Errorf("Match(%v) message = %q, want %q", tt.argv, got.Rule.Message, tt.wantMessage)
		}
	}
}

func TestMatchEmptyArgv(t *testing.T) {
	t.Parallel()
	rs := &Ruleset{Mode: ModeEnforce}
	if got := rs.Match(nil); got.Decision != DecisionRefuse {
		t.Errorf("Match(nil) = %v, want refuse", got.Decision)
	}
}
