package approve

import (
	"slices"
	"testing"
)

// rule constructors mirror what the loader produces: kind is set and glob/regex
// patterns are compiled. compileMatch, which derives the match form, is applied
// by runMatchCase so each case exercises the same path Load takes.

func exactRule(tokens ...string) Rule { return Rule{Exact: tokens, kind: KindExact} }

func prefixRule(tokens ...string) Rule { return Rule{Prefix: tokens, kind: KindPrefix} }

// asBase sets as_basename, which is meaningful only on glob and regex rules now
// that prefix and exact rules infer their form from the first token's shape.
func asBase(r Rule) Rule {
	r.AsBasename = true
	return r
}

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

// identitySubject builds a subject whose canonical form equals its argv, for
// tests that do not exercise resolution. Used across the approve test files.
func identitySubject(argv []string) Subject {
	return Subject{Argv: argv, Canonical: argv}
}

type matchTest struct {
	name     string
	mode     Mode
	deny     []Rule
	allow    []Rule
	restrict []Rule
	argv     []string
	// canonical overrides the canonical form; when nil it defaults to argv.
	canonical []string
	// resolved sets the subject's pre-symlink resolved form; nil leaves it unset.
	resolved []string
	// unresolved leaves the canonical form unavailable, as when canonicalization
	// fails. It takes precedence over canonical.
	unresolved bool
	// root is the project root relative rule tokens resolve against.
	root           string
	want           Decision
	wantMessage    string
	wantRestricted bool
}

func TestMatch(t *testing.T) {
	t.Parallel()

	// glob/regex rules need compilation, so cases that use them build the
	// ruleset inside the loop via helpers; static cases are listed in the table.
	staticTests := []matchTest{
		// mode short-circuits
		{name: "allow-all overrides deny", mode: ModeAllowAll, deny: []Rule{prefixRule("rm")}, argv: []string{"rm", "-rf", "/"}, want: DecisionRun},
		{name: "deny-all overrides allow", mode: ModeDenyAll, allow: []Rule{prefixRule("git")}, argv: []string{"git", "status"}, want: DecisionRefuse},

		// bare-name exact matches by basename
		{name: "exact match", allow: []Rule{exactRule("git", "status")}, argv: []string{"git", "status"}, want: DecisionRun},
		{name: "exact longer argv no match", allow: []Rule{exactRule("git", "status")}, argv: []string{"git", "status", "-s"}, want: DecisionPrompt},
		{name: "exact different arg no match", allow: []Rule{exactRule("git", "status")}, argv: []string{"git", "log"}, want: DecisionPrompt},

		// bare-name prefix matches by basename
		{name: "prefix match with extra args", allow: []Rule{prefixRule("go", "test")}, argv: []string{"go", "test", "./..."}, want: DecisionRun},
		{name: "prefix argv shorter no match", allow: []Rule{prefixRule("go", "test")}, argv: []string{"go"}, want: DecisionPrompt},
		{name: "prefix differing token no match", allow: []Rule{prefixRule("go", "test")}, argv: []string{"go", "vet"}, want: DecisionPrompt},

		// absolute path policy: the token matches the resolved executable path
		{name: "canonical path allow", allow: []Rule{prefixRule("/opt/foo/bin/foo")}, argv: []string{"foo"}, canonical: []string{"/opt/foo/bin/foo"}, want: DecisionRun},
		{name: "canonical path allow with tail", allow: []Rule{prefixRule("/opt/foo/bin/foo")}, argv: []string{"foo", "--bar"}, canonical: []string{"/opt/foo/bin/foo", "--bar"}, want: DecisionRun},
		{name: "bare token matches by basename", allow: []Rule{prefixRule("foo")}, argv: []string{"foo"}, canonical: []string{"/opt/foo/bin/foo"}, want: DecisionRun},
		{name: "canonical exact full path", allow: []Rule{exactRule("/usr/bin/git", "status")}, argv: []string{"git", "status"}, canonical: []string{"/usr/bin/git", "status"}, want: DecisionRun},

		// an absolute token compares element-wise; no implicit normalization
		{name: "literal path prefix match", deny: []Rule{prefixRule("/bin/sh")}, argv: []string{"/bin/sh"}, want: DecisionRefuse},
		{name: "literal path other path no match", deny: []Rule{prefixRule("/bin/sh")}, argv: []string{"/usr/bin/sh"}, want: DecisionPrompt},

		// argv[1:] compares byte-exact
		{name: "deny rm -rf exact tail", deny: []Rule{prefixRule("rm", "-rf")}, argv: []string{"rm", "-rf"}, want: DecisionRefuse},
		{name: "deny rm -rf with target", deny: []Rule{prefixRule("rm", "-rf")}, argv: []string{"rm", "-rf", "/tmp"}, want: DecisionRefuse},
		{name: "deny rm -rf differing flag", deny: []Rule{prefixRule("rm", "-rf")}, argv: []string{"rm", "-r"}, want: DecisionPrompt},

		// a bare-name rule matches the invoked basename, however it is spelled
		{name: "basename deny plain", deny: []Rule{prefixRule("sh")}, argv: []string{"sh", "-c", "x"}, want: DecisionRefuse},
		{name: "basename deny absolute path", deny: []Rule{prefixRule("sh")}, argv: []string{"/bin/sh", "-c", "x"}, canonical: []string{"/bin/dash", "-c", "x"}, want: DecisionRefuse},
		{name: "basename deny relative path", deny: []Rule{prefixRule("sh")}, argv: []string{"./sh", "-c", "x"}, want: DecisionRefuse},
		{name: "basename allow ignores install path", allow: []Rule{prefixRule("make")}, argv: []string{"/tmp/evil/make"}, canonical: []string{"/tmp/evil/make"}, want: DecisionRun},
		{name: "basename allow exact", allow: []Rule{exactRule("go", "version")}, argv: []string{"/usr/local/go/bin/go", "version"}, canonical: []string{"/usr/local/go/bin/go", "version"}, want: DecisionRun},

		// canonical unavailable: path rules cannot match, bare-name rules can
		{name: "unresolved path allow falls through", allow: []Rule{prefixRule("/opt/foo")}, argv: []string{"foo"}, unresolved: true, want: DecisionPrompt},
		{name: "unresolved path deny does not fire", deny: []Rule{prefixRule("/tmp/x")}, allow: []Rule{prefixRule("foo")}, argv: []string{"foo"}, unresolved: true, want: DecisionRun},
		{name: "unresolved basename deny still fires", deny: []Rule{prefixRule("sh")}, argv: []string{"sh", "-c", "x"}, unresolved: true, want: DecisionRefuse},

		// deny precedence and layering
		{name: "deny wins over allow", deny: []Rule{prefixRule("git", "push", "--force")}, allow: []Rule{prefixRule("git")}, argv: []string{"git", "push", "--force"}, want: DecisionRefuse},
		{name: "allow when no deny matches", deny: []Rule{prefixRule("git", "push", "--force")}, allow: []Rule{prefixRule("git")}, argv: []string{"git", "status"}, want: DecisionRun},
		{name: "no match prompts", allow: []Rule{prefixRule("go")}, argv: []string{"cargo", "build"}, want: DecisionPrompt},

		// deny wins across canonical and basename forms
		{name: "basename deny beats canonical allow", deny: []Rule{prefixRule("sh")}, allow: []Rule{prefixRule("/bin/sh")}, argv: []string{"/bin/sh", "-c", "x"}, canonical: []string{"/bin/sh", "-c", "x"}, want: DecisionRefuse},
		{name: "canonical path deny beats basename allow", deny: []Rule{prefixRule("/tmp/make")}, allow: []Rule{prefixRule("make")}, argv: []string{"make"}, canonical: []string{"/tmp/make"}, want: DecisionRefuse},

		// deny message propagation
		{name: "deny message surfaced", deny: []Rule{{Prefix: []string{"rm", "-rf"}, Message: "delete specific paths", kind: KindPrefix}}, argv: []string{"rm", "-rf", "/"}, want: DecisionRefuse, wantMessage: "delete specific paths"},

		// restrict tier: an in-scope command refuses, out of scope still prompts
		{name: "restrict prefix refuses in scope", restrict: []Rule{prefixRule("git")}, argv: []string{"git", "push"}, want: DecisionRefuse, wantRestricted: true},
		{name: "restrict prefix out of scope prompts", restrict: []Rule{prefixRule("git")}, argv: []string{"make"}, want: DecisionPrompt},
		{name: "restrict exact refuses in scope", restrict: []Rule{exactRule("git", "push")}, argv: []string{"git", "push"}, want: DecisionRefuse, wantRestricted: true},

		// allow carves out of restrict; the unmatched rest of the scope refuses
		{name: "allow carves out of restrict", allow: []Rule{prefixRule("git", "show")}, restrict: []Rule{prefixRule("git")}, argv: []string{"git", "show", "HEAD"}, want: DecisionRun},
		{name: "restrict refuses uncarved command", allow: []Rule{prefixRule("git", "show")}, restrict: []Rule{prefixRule("git")}, argv: []string{"git", "push"}, want: DecisionRefuse, wantRestricted: true},

		// deny still wins over an allow that would carve out of restrict; a deny
		// refusal is not flagged restricted
		{name: "deny beats allow carve and restrict", deny: []Rule{prefixRule("git", "push")}, allow: []Rule{prefixRule("git")}, restrict: []Rule{prefixRule("git")}, argv: []string{"git", "push"}, want: DecisionRefuse},

		// restrict message propagation
		{name: "restrict message surfaced", restrict: []Rule{{Prefix: []string{"git"}, Message: "only read-only git is permitted here", kind: KindPrefix}}, argv: []string{"git", "push"}, want: DecisionRefuse, wantMessage: "only read-only git is permitted here", wantRestricted: true},

		// path rules match the resolved form too, so a rule can pin a multiplexer
		// shim like rustup's cargo, whose canonical path erases which tool ran
		{name: "resolved path allow matches shim", allow: []Rule{prefixRule("/x/cargo", "fmt")}, argv: []string{"cargo", "fmt"}, resolved: []string{"/x/cargo", "fmt"}, canonical: []string{"/x/rustup", "fmt"}, want: DecisionRun},
		{name: "resolved path rule other shim no match", allow: []Rule{prefixRule("/x/cargo", "fmt")}, argv: []string{"rustc", "fmt"}, resolved: []string{"/x/rustc", "fmt"}, canonical: []string{"/x/rustup", "fmt"}, want: DecisionPrompt},
		{name: "canonical rule still matches shim", allow: []Rule{prefixRule("/x/rustup", "fmt")}, argv: []string{"cargo", "fmt"}, resolved: []string{"/x/cargo", "fmt"}, canonical: []string{"/x/rustup", "fmt"}, want: DecisionRun},
		{name: "resolved path deny fires", deny: []Rule{prefixRule("/x/cargo")}, allow: []Rule{prefixRule("cargo")}, argv: []string{"cargo", "fmt"}, resolved: []string{"/x/cargo", "fmt"}, canonical: []string{"/x/rustup", "fmt"}, want: DecisionRefuse},
		{name: "resolved form needs canonical", allow: []Rule{prefixRule("/x/cargo")}, argv: []string{"cargo"}, resolved: []string{"/x/cargo"}, unresolved: true, want: DecisionPrompt},
	}

	for _, tt := range staticTests {
		t.Run(tt.name, func(t *testing.T) {
			runMatchCase(t, tt)
		})
	}
}

// TestMatchRelativeTokens covers prefix/exact rules whose first token is a
// relative path. The token resolves against the project root, then matches the
// subject's canonical path; it never consults the per-call cwd, so the rule means
// the same file regardless of where the command is run.
func TestMatchRelativeTokens(t *testing.T) {
	t.Parallel()

	tests := []matchTest{
		{name: "relative allow matches under project root", root: "/proj", allow: []Rule{prefixRule("./bin/tool")}, argv: []string{"./bin/tool"}, canonical: []string{"/proj/bin/tool"}, want: DecisionRun},
		{name: "relative allow no leading dot", root: "/proj", allow: []Rule{prefixRule("zig-out/bin/1z")}, argv: []string{"zig-out/bin/1z"}, canonical: []string{"/proj/zig-out/bin/1z"}, want: DecisionRun},
		{name: "relative allow other root no match", root: "/proj", allow: []Rule{prefixRule("./bin/tool")}, argv: []string{"./bin/tool"}, canonical: []string{"/other/bin/tool"}, want: DecisionPrompt},
		{name: "relative allow with tail", root: "/proj", allow: []Rule{prefixRule("./run.sh")}, argv: []string{"./run.sh", "--fast"}, canonical: []string{"/proj/run.sh", "--fast"}, want: DecisionRun},
		{name: "relative deny fires", root: "/proj", deny: []Rule{prefixRule("./scripts/danger.sh")}, argv: []string{"./scripts/danger.sh"}, canonical: []string{"/proj/scripts/danger.sh"}, want: DecisionRefuse},
		{name: "relative exact resolves", root: "/proj", allow: []Rule{exactRule("./bin/tool", "--once")}, argv: []string{"./bin/tool", "--once"}, canonical: []string{"/proj/bin/tool", "--once"}, want: DecisionRun},
		{name: "relative unresolved falls through", root: "/proj", allow: []Rule{prefixRule("./bin/tool")}, argv: []string{"./bin/tool"}, unresolved: true, want: DecisionPrompt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runMatchCase(t, tt)
		})
	}
}

func TestMatchPatterns(t *testing.T) {
	t.Parallel()

	tests := []matchTest{
		// glob is fully anchored, over the canonical join
		{name: "glob trailing star matches tail", allow: []Rule{globRule(t, "kubectl get *")}, argv: []string{"kubectl", "get", "pods", "-n", "x"}, want: DecisionRun},
		{name: "glob different verb no match", allow: []Rule{globRule(t, "kubectl get *")}, argv: []string{"kubectl", "describe", "pods"}, want: DecisionPrompt},
		{name: "glob no wildcard exact", allow: []Rule{globRule(t, "make")}, argv: []string{"make"}, want: DecisionRun},
		{name: "glob no wildcard rejects extra", allow: []Rule{globRule(t, "make")}, argv: []string{"make", "build"}, want: DecisionPrompt},
		{name: "glob over quoted join", allow: []Rule{globRule(t, "git commit -m *")}, argv: []string{"git", "commit", "-m", "hello world"}, want: DecisionRun},

		// regex is unanchored search, over the canonical join
		{name: "regex sudo leading", deny: []Rule{regexRule(t, `(^|\s)sudo(\s|$)`)}, argv: []string{"sudo", "rm"}, want: DecisionRefuse},
		{name: "regex sudo substring no match", deny: []Rule{regexRule(t, `(^|\s)sudo(\s|$)`)}, argv: []string{"mysudo", "foo"}, want: DecisionPrompt},
		{name: "regex sudo mid line", deny: []Rule{regexRule(t, `(^|\s)sudo(\s|$)`)}, argv: []string{"echo", "sudo", "hi"}, want: DecisionRefuse},
		{name: "regex npm test", allow: []Rule{regexRule(t, `^npm (run )?(test|lint)$`)}, argv: []string{"npm", "test"}, want: DecisionRun},
		{name: "regex npm run lint", allow: []Rule{regexRule(t, `^npm (run )?(test|lint)$`)}, argv: []string{"npm", "run", "lint"}, want: DecisionRun},
		{name: "regex npm install no match", allow: []Rule{regexRule(t, `^npm (run )?(test|lint)$`)}, argv: []string{"npm", "install"}, want: DecisionPrompt},

		// regex over the canonical path: directory policies
		{name: "regex deny tmp directory", deny: []Rule{regexRule(t, `^/tmp/`)}, argv: []string{"x"}, canonical: []string{"/tmp/x"}, want: DecisionRefuse},
		{name: "regex allow opt directory", allow: []Rule{regexRule(t, `^/opt/foo/bin/`)}, argv: []string{"foo"}, canonical: []string{"/opt/foo/bin/foo"}, want: DecisionRun},
		{name: "regex path bare token no match", allow: []Rule{regexRule(t, `^go test`)}, argv: []string{"go", "test"}, canonical: []string{"/usr/bin/go", "test"}, want: DecisionPrompt},

		// as_basename glob/regex match the basename join, the only way to express
		// name-based matching for a pattern rule
		{name: "basename regex matches by name", allow: []Rule{asBase(regexRule(t, `^go test`))}, argv: []string{"go", "test"}, canonical: []string{"/usr/bin/go", "test"}, want: DecisionRun},
		{name: "basename regex deny sudo by name", deny: []Rule{asBase(regexRule(t, `^sudo(\s|$)`))}, argv: []string{"/usr/bin/sudo", "rm"}, canonical: []string{"/usr/bin/sudo", "rm"}, want: DecisionRefuse},
		{name: "basename glob by name", allow: []Rule{asBase(globRule(t, "kubectl get *"))}, argv: []string{"/usr/local/bin/kubectl", "get", "pods"}, canonical: []string{"/usr/local/bin/kubectl", "get", "pods"}, want: DecisionRun},

		// restrict tier with pattern kinds
		{name: "restrict glob refuses in scope", restrict: []Rule{globRule(t, "git *")}, argv: []string{"git", "push"}, want: DecisionRefuse, wantRestricted: true},
		{name: "restrict regex refuses in scope", restrict: []Rule{regexRule(t, `^git `)}, argv: []string{"git", "push"}, want: DecisionRefuse, wantRestricted: true},
		{name: "restrict regex out of scope prompts", restrict: []Rule{regexRule(t, `^git `)}, argv: []string{"make"}, want: DecisionPrompt},

		// pattern rules also see the resolved join, so a directory policy can name
		// the shim's location rather than its symlink target
		{name: "regex allow over resolved join", allow: []Rule{regexRule(t, `^/x/cargo fmt$`)}, argv: []string{"cargo", "fmt"}, resolved: []string{"/x/cargo", "fmt"}, canonical: []string{"/x/rustup", "fmt"}, want: DecisionRun},
		{name: "glob deny over resolved join", deny: []Rule{globRule(t, "/x/cargo *")}, argv: []string{"cargo", "fmt"}, resolved: []string{"/x/cargo", "fmt"}, canonical: []string{"/x/rustup", "fmt"}, want: DecisionRefuse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runMatchCase(t, tt)
		})
	}
}

// subjectFor builds the match subject for a test case. unresolved leaves the
// canonical form nil; otherwise canonical defaults to argv. resolved is carried
// as given, including alongside unresolved, so tests can cover a subject whose
// resolution succeeded but whose canonicalization failed.
func subjectFor(tt matchTest) Subject {
	if tt.unresolved {
		return Subject{Argv: tt.argv, Resolved: tt.resolved}
	}
	canonical := tt.canonical
	if canonical == nil {
		canonical = tt.argv
	}

	return Subject{Argv: tt.argv, Canonical: canonical, Resolved: tt.resolved}
}

func runMatchCase(t *testing.T, tt matchTest) {
	t.Helper()
	rs := &Ruleset{Mode: tt.mode, Deny: slices.Clone(tt.deny), Allow: slices.Clone(tt.allow), Restrict: slices.Clone(tt.restrict)}
	for i := range rs.Deny {
		compileMatch(&rs.Deny[i], tt.root)
	}
	for i := range rs.Allow {
		compileMatch(&rs.Allow[i], tt.root)
	}
	for i := range rs.Restrict {
		compileMatch(&rs.Restrict[i], tt.root)
	}
	got := rs.Match(subjectFor(tt))
	if got.Decision != tt.want {
		t.Fatalf("Match(%v) decision = %v, want %v", tt.argv, got.Decision, tt.want)
	}
	if got.Restricted != tt.wantRestricted {
		t.Errorf("Match(%v) restricted = %v, want %v", tt.argv, got.Restricted, tt.wantRestricted)
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
	if got := rs.Match(Subject{}); got.Decision != DecisionRefuse {
		t.Errorf("Match(empty) = %v, want refuse", got.Decision)
	}
}
