package approve

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/model"
)

const (
	tildeHome  = "/home/me"
	tildeAlice = "/home/alice"
)

func tildeHomeFn() (string, error) { return tildeHome, nil }

func tildeLookup(name string) (string, error) {
	if name == "alice" {
		return tildeAlice, nil
	}

	return "", errors.New("unknown user " + name)
}

func tildeOptions(t *testing.T, global, project string) LoadOptions {
	t.Helper()

	globalPath := filepath.Join(t.TempDir(), "absent.yaml")
	if global != "" {
		globalPath = writeGlobal(t, global)
	}

	return LoadOptions{
		GlobalPath:  globalPath,
		ProjectRoot: writeProject(t, project),
		Home:        tildeHomeFn,
		LookupUser:  tildeLookup,
	}
}

func loadTilde(t *testing.T, global, project string) *Store {
	t.Helper()

	s, err := Load(tildeOptions(t, global, project))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return s
}

type tildeMatchTest struct {
	Name      string
	Rules     string
	Argv      []string
	Canonical []string
	Want      Decision
}

var tildeMatchTests = []tildeMatchTest{
	{Name: "prefix ~/ matches the home path", Rules: "allow:\n  - prefix: [~/bin/foo]\n", Argv: []string{"foo", "x"}, Canonical: []string{"/home/me/bin/foo", "x"}, Want: DecisionRun},
	{Name: "exact ~/ matches the home path", Rules: "allow:\n  - exact: [~/bin/foo, x]\n", Argv: []string{"foo", "x"}, Canonical: []string{"/home/me/bin/foo", "x"}, Want: DecisionRun},
	{Name: "exact ~/ rejects an extra argument", Rules: "allow:\n  - exact: [~/bin/foo]\n", Argv: []string{"foo", "x"}, Canonical: []string{"/home/me/bin/foo", "x"}, Want: DecisionPrompt},
	{Name: "~user/ matches that user's home", Rules: "allow:\n  - prefix: [~alice/bin/foo]\n", Argv: []string{"foo"}, Canonical: []string{"/home/alice/bin/foo"}, Want: DecisionRun},
	{Name: "~user/ does not match the running user's home", Rules: "allow:\n  - prefix: [~alice/bin/foo]\n", Argv: []string{"foo"}, Canonical: []string{"/home/me/bin/foo"}, Want: DecisionPrompt},
	{Name: "a bare ~ names the home directory, never a program name", Rules: "allow:\n  - prefix: [\"~\"]\n", Argv: []string{"~"}, Canonical: nil, Want: DecisionPrompt},
	{Name: "a bare ~user names the user's home, never a program name", Rules: "allow:\n  - prefix: [\"~alice\"]\n", Argv: []string{"~alice"}, Canonical: nil, Want: DecisionPrompt},
	{Name: "a bare ~ matches the home path as a path token", Rules: "allow:\n  - prefix: [\"~\"]\n", Argv: []string{"x"}, Canonical: []string{"/home/me"}, Want: DecisionRun},
	{Name: "a ~/ rule never resolves under the project root", Rules: "allow:\n  - prefix: [~/bin/foo]\n", Argv: []string{"foo"}, Canonical: []string{"ROOT/~/bin/foo"}, Want: DecisionPrompt},
	{Name: "a mid-token ~ is literal", Rules: "allow:\n  - prefix: [/opt/~/foo]\n", Argv: []string{"foo"}, Canonical: []string{"/opt/~/foo"}, Want: DecisionRun},
	{Name: "a mid-token ~ is not expanded", Rules: "allow:\n  - prefix: [/opt/~/foo]\n", Argv: []string{"foo"}, Canonical: []string{"/opt//home/me/foo"}, Want: DecisionPrompt},
	{Name: "a later token stays literal", Rules: "allow:\n  - prefix: [ls, ~/x]\n", Argv: []string{"ls", "~/x"}, Want: DecisionRun},
	{Name: "a later token is not expanded", Rules: "allow:\n  - prefix: [ls, ~/x]\n", Argv: []string{"ls", "/home/me/x"}, Want: DecisionPrompt},
	{Name: "a glob stays literal", Rules: "allow:\n  - glob: \"*~/bin/*\"\n", Argv: []string{"~/bin/foo"}, Canonical: []string{"~/bin/foo"}, Want: DecisionRun},
	{Name: "a glob is not expanded", Rules: "allow:\n  - glob: \"*~/bin/*\"\n", Argv: []string{"foo"}, Canonical: []string{"/home/me/bin/foo"}, Want: DecisionPrompt},
	{Name: "a regex is not expanded", Rules: "allow:\n  - regex: \"^~/bin/.*\"\n", Argv: []string{"foo"}, Canonical: []string{"/home/me/bin/foo"}, Want: DecisionPrompt},
	{Name: "an unresolvable ~user in an allow rule matches nothing", Rules: "allow:\n  - prefix: [~ghost/bin/foo]\n", Argv: []string{"foo"}, Canonical: []string{"/home/ghost/bin/foo"}, Want: DecisionPrompt},
	{Name: "an inert rule does not match the same text under the project root", Rules: "allow:\n  - prefix: [~ghost/bin/foo]\n", Argv: []string{"foo"}, Canonical: []string{"ROOT/~ghost/bin/foo"}, Want: DecisionPrompt},
	{Name: "an inert exact rule matches nothing", Rules: "allow:\n  - exact: [~ghost/bin/foo]\n", Argv: []string{"~ghost/bin/foo"}, Canonical: []string{"ROOT/~ghost/bin/foo"}, Want: DecisionPrompt},
	{Name: "a ~/ deny rule refuses the home path", Rules: "deny:\n  - prefix: [~/bin/foo]\nallow:\n  - prefix: [foo]\n", Argv: []string{"foo"}, Canonical: []string{"/home/me/bin/foo"}, Want: DecisionRefuse},
	{Name: "a ~/ restrict rule refuses the home path", Rules: "restrict:\n  - prefix: [~/bin/foo]\n", Argv: []string{"foo"}, Canonical: []string{"/home/me/bin/foo"}, Want: DecisionRefuse},
}

func TestTildeMatch(t *testing.T) {
	t.Parallel()

	for _, tt := range tildeMatchTests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			s := loadTilde(t, "", "version: 1\n"+tt.Rules)

			canonical := slices.Clone(tt.Canonical)
			for i := range canonical {
				canonical[i] = strings.ReplaceAll(canonical[i], "ROOT", s.projectRoot)
			}

			subject := Subject{Argv: tt.Argv, Canonical: canonical, Resolved: canonical}
			if got := s.Ruleset().Match(subject).Decision; got != tt.Want {
				t.Errorf("Match = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestTildeRuleIsNeverProjectRelative(t *testing.T) {
	t.Parallel()

	s := loadTilde(t, "", "version: 1\nallow:\n  - prefix: [~/bin/foo]\n")
	rule := &s.Ruleset().Allow[0]

	if rule.relative || rule.wantBasename || rule.cmpArgv[0] != "/home/me/bin/foo" {
		t.Fatalf("rule = relative %v, basename %v, cmp %v; want an absolute path rule", rule.relative, rule.wantBasename, rule.cmpArgv)
	}

	aliased := matchForm{head: "/elsewhere/bin/foo", alias: "/home/me/bin/foo", ok: true}
	if matchRuleTokens(rule, aliased, false) {
		t.Error("a checkout alias matched a ~ rule, want it to apply only to project-relative rules")
	}
}

func TestTildeKeepsWrittenSpelling(t *testing.T) {
	t.Parallel()

	s := loadTilde(t, "", "version: 1\nallow:\n  - prefix: [~/bin/foo, -v]\n")

	if got := s.Ruleset().Allow[0].Pattern(); got != "[~/bin/foo, -v]" {
		t.Errorf("Pattern = %q, want the file's spelling", got)
	}
}

type tildeLoadErrorTest struct {
	Name    string
	Global  string
	Project string
	Home    func() (string, error)
	Want    []string
}

var tildeLoadErrorTests = []tildeLoadErrorTest{
	{Name: "deny names file and token", Project: "version: 1\ndeny:\n  - prefix: [~ghost/bin/foo]\n", Want: []string{".cg.yaml", "~ghost/bin/foo", "ghost", "absolutely"}},
	{Name: "restrict names file and token", Project: "version: 1\nrestrict:\n  - prefix: [~ghost/bin/foo]\n", Want: []string{".cg.yaml", "~ghost/bin/foo"}},
	{Name: "wrapper names file and token", Project: "version: 1\nallow:\n  - wrapper: [~ghost/bin/nice, -n, \"1\"]\n", Want: []string{".cg.yaml", "~ghost/bin/nice"}},
	{Name: "the global file is named for its own token", Global: "version: 1\ndeny:\n  - prefix: [~ghost/x]\n", Project: "version: 1\n", Want: []string{"approve.yaml", "~ghost/x"}},
	{Name: "an unresolvable home fails an allow rule with ~/", Project: "version: 1\nallow:\n  - prefix: [~/bin/foo]\n", Home: func() (string, error) { return "", errors.New("HOME unset") }, Want: []string{".cg.yaml", "~/bin/foo", "HOME unset"}},
	{Name: "an unresolvable home fails a deny rule with a bare ~", Project: "version: 1\ndeny:\n  - prefix: [\"~\"]\n", Home: func() (string, error) { return "", errors.New("HOME unset") }, Want: []string{".cg.yaml", "HOME unset"}},
}

func TestTildeLoadErrors(t *testing.T) {
	t.Parallel()

	for _, tt := range tildeLoadErrorTests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			opts := tildeOptions(t, tt.Global, tt.Project)
			if tt.Home != nil {
				opts.Home = tt.Home
			}

			_, err := Load(opts)
			if err == nil {
				t.Fatal("Load succeeded, want an error")
			}

			for _, want := range tt.Want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q missing %q", err, want)
				}
			}
		})
	}
}

func TestTildeUnresolvableHomeFailsOnlyWhereATildeAppears(t *testing.T) {
	t.Parallel()

	opts := tildeOptions(t, "", "version: 1\nallow:\n  - prefix: [make]\n  - prefix: [ls, ~/x]\n")
	opts.Home = func() (string, error) { return "", errors.New("HOME unset") }

	if _, err := Load(opts); err != nil {
		t.Errorf("Load with no ~ first token: %v", err)
	}
}

func TestTildeRelativeHomeIsUnresolvable(t *testing.T) {
	t.Parallel()

	opts := tildeOptions(t, "", "version: 1\nallow:\n  - prefix: [~/bin/foo]\n")
	opts.Home = func() (string, error) { return "relative/home", nil }

	if _, err := Load(opts); err == nil || !strings.Contains(err.Error(), "not an absolute path") {
		t.Errorf("Load error = %v, want a relative home to fail", err)
	}
}

func TestTildeInertAllowRuleIsReportedAgainstItsLayer(t *testing.T) {
	t.Parallel()

	opts := tildeOptions(t, "version: 1\nallow:\n  - exact: [~ghost/g]\n", "version: 1\nallow:\n  - prefix: [~ghost/p]\n")
	s, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var inert []string
	for _, w := range s.Warnings() {
		if w.Kind == WarnInertRule {
			inert = append(inert, w.Message)
		}
	}

	if len(inert) != 2 {
		t.Fatalf("inert warnings = %v, want two", inert)
	}
	if !strings.HasPrefix(inert[0], opts.GlobalPath) || !strings.Contains(inert[0], "~ghost/g") {
		t.Errorf("global warning %q should name its layer file and token", inert[0])
	}
	if !strings.HasPrefix(inert[1], s.Project.Path) || !strings.Contains(inert[1], "~ghost/p") {
		t.Errorf("project warning %q should name its layer file and token", inert[1])
	}

	global, project, err := Diagnose(opts)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if global.Err != nil || project.Err != nil {
		t.Fatalf("an inert allow rule is not an error: %v, %v", global.Err, project.Err)
	}
	if kinds := sortedKinds(project.Warnings); !slices.Equal(kinds, []WarningKind{WarnInertRule}) {
		t.Errorf("project lint warning kinds = %v, want one inert rule", kinds)
	}
}

func TestTildeDiagnoseReportsErrorAgainstTheLayerFile(t *testing.T) {
	t.Parallel()

	global, project, err := Diagnose(tildeOptions(t, "", "version: 1\ndeny:\n  - prefix: [~ghost/x]\n"))
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}

	if global.Err != nil {
		t.Errorf("global error = %v, want none", global.Err)
	}
	if project.Err == nil || !strings.Contains(project.Err.Error(), project.Path) || !strings.Contains(project.Err.Error(), "~ghost/x") {
		t.Errorf("project error = %v, want it to name the file and token", project.Err)
	}
}

type tildeCoverTest struct {
	Name    string
	Project string
	Want    []WarningKind
}

var tildeCoverTests = []tildeCoverTest{
	{Name: "an absolute prefix covers a ~ wrapper's program", Project: "allow:\n  - prefix: [/home/me/bin/foo]\n  - wrapper: [~/bin/foo, run]\n", Want: []WarningKind{WarnCoveredByAllow}},
	{Name: "a ~ prefix covers an absolute wrapper's program", Project: "allow:\n  - prefix: [~/bin/foo]\n  - wrapper: [/home/me/bin/foo, run]\n", Want: []WarningKind{WarnCoveredByAllow}},
	{Name: "a ~ prefix covers a ~ wrapper's program", Project: "allow:\n  - prefix: [~/bin/foo]\n  - wrapper: [~/bin/foo, run]\n", Want: []WarningKind{WarnCoveredByAllow}},
	{Name: "a bare prefix covers a ~ wrapper by basename", Project: "allow:\n  - prefix: [foo]\n  - wrapper: [~/bin/foo, run]\n", Want: []WarningKind{WarnCoveredByAllow}},
	{Name: "another user's prefix does not cover it", Project: "allow:\n  - prefix: [~alice/bin/foo]\n  - wrapper: [~/bin/foo, run]\n"},
	{Name: "an inert prefix covers nothing", Project: "allow:\n  - prefix: [~ghost/bin/foo]\n  - wrapper: [~/bin/foo, run]\n", Want: []WarningKind{WarnInertRule}},
	{Name: "an inert prefix does not cover a bare wrapper", Project: "allow:\n  - prefix: [~ghost/bin/nice]\n  - wrapper: [nice, -n, \"1\"]\n", Want: []WarningKind{WarnInertRule}},
	{Name: "a ~ prefix deny kills a ~ wrapper", Project: "deny:\n  - prefix: [~/bin/foo]\nallow:\n  - wrapper: [~/bin/foo, run]\n", Want: []WarningKind{WarnDeadByDeny}},
}

func TestTildeCoverageWarnings(t *testing.T) {
	t.Parallel()

	for _, tt := range tildeCoverTests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			s := loadTilde(t, "", "version: 1\n"+tt.Project)

			want := slices.Clone(tt.Want)
			slices.Sort(want)

			if got := sortedKinds(s.Warnings()); !slices.Equal(got, want) {
				t.Errorf("warning kinds = %v, want %v", got, want)
			}
		})
	}
}

func TestTildeCoverageThroughMergedLint(t *testing.T) {
	t.Parallel()

	opts := tildeOptions(t, "version: 1\nallow:\n  - prefix: [/home/me/bin/foo]\n", "version: 1\nallow:\n  - wrapper: [~/bin/foo, run]\n")
	global, project, err := Diagnose(opts)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}

	merged, ok := MergedWarnings(global, project)
	if !ok {
		t.Fatal("MergedWarnings ok = false")
	}
	if got := sortedKinds(merged); !slices.Equal(got, []WarningKind{WarnCoveredByAllow}) {
		t.Errorf("merged kinds = %v, want the covering prefix", got)
	}
}

type tildeEditTest struct {
	Name      string
	Tokens    []string
	Argv      []string
	Canonical string

	WantKind   RuleKind
	WantReject bool
}

var tildeEditTests = []tildeEditTest{
	{Name: "~/ matches the program's home path", Tokens: []string{"~/bin/foo", "x"}, Argv: []string{"foo", "x"}, Canonical: "/home/me/bin/foo", WantKind: KindExact},
	{Name: "~/ trimmed is a prefix", Tokens: []string{"~/bin/foo"}, Argv: []string{"foo", "x"}, Canonical: "/home/me/bin/foo", WantKind: KindPrefix},
	{Name: "~/ misses another program", Tokens: []string{"~/bin/foo", "x"}, Argv: []string{"foo", "x"}, Canonical: "/home/me/bin/bar", WantReject: true},
	{Name: "~/ misses the same text under the project root", Tokens: []string{"~/bin/foo", "x"}, Argv: []string{"foo", "x"}, Canonical: "ROOT/~/bin/foo", WantReject: true},
	{Name: "an unresolvable ~user misses even the path it would name", Tokens: []string{"~ghost/bin/foo", "x"}, Argv: []string{"foo", "x"}, Canonical: "/home/ghost/bin/foo", WantReject: true},
	{Name: "an unresolvable ~user misses the project-root path", Tokens: []string{"~ghost/bin/foo", "x"}, Argv: []string{"foo", "x"}, Canonical: "ROOT/~ghost/bin/foo", WantReject: true},
	{Name: "a resolvable ~user matches", Tokens: []string{"~alice/bin/foo", "x"}, Argv: []string{"foo", "x"}, Canonical: "/home/alice/bin/foo", WantKind: KindExact},
}

func TestTildeCheckEdit(t *testing.T) {
	t.Parallel()

	s := loadTilde(t, "", "version: 1\n")

	for _, tt := range tildeEditTests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			canonical := strings.ReplaceAll(tt.Canonical, "ROOT", s.projectRoot)
			targets := []EditTarget{{Argv: tt.Argv, Resolution: &model.Resolution{Argv: tt.Argv, Resolved: canonical, Canonical: canonical}}}

			kind, rejection := s.CheckEdit(tt.Tokens, false, targets)
			if tt.WantReject {
				if rejection == nil || rejection.Reason != EditMismatch {
					t.Fatalf("CheckEdit = %v, %+v; want a mismatch", kind, rejection)
				}

				return
			}

			if rejection != nil || kind != tt.WantKind {
				t.Errorf("CheckEdit = %v, %+v; want kind %v", kind, rejection, tt.WantKind)
			}
		})
	}
}

func TestTildePathNamedByAndProblem(t *testing.T) {
	t.Parallel()

	s := loadTilde(t, "", "version: 1\n")

	if got := s.PathNamedBy("~/bin/foo"); got != "/home/me/bin/foo" {
		t.Errorf("PathNamedBy(~/bin/foo) = %q, want the expanded home path", got)
	}
	if got := s.PathNamedBy("~"); got != "/home/me" {
		t.Errorf("PathNamedBy(~) = %q, want the home directory", got)
	}
	if got := s.PathNamedBy("~ghost/bin"); got != "" {
		t.Errorf("PathNamedBy(~ghost/bin) = %q, want none", got)
	}
	if got := s.PathNamedBy("/opt/~/x"); got != "/opt/~/x" {
		t.Errorf("PathNamedBy(/opt/~/x) = %q, want the literal path", got)
	}

	if got := s.TildeProblem("~ghost/bin"); !strings.Contains(got, `"ghost"`) {
		t.Errorf("TildeProblem(~ghost/bin) = %q, want it to name the user", got)
	}
	if got := s.TildeProblem("~/bin"); got != "" {
		t.Errorf("TildeProblem(~/bin) = %q, want none", got)
	}
}

func TestTildeCollidesWithWrapper(t *testing.T) {
	t.Parallel()

	s := loadTilde(t, "", "version: 1\nallow:\n  - wrapper: [~/bin/run]\n  - wrapper: [/opt/bin/timeout, \"5\"]\n")

	cases := []collidesTest{
		{Name: "a ~ token equal to a ~ head", First: "~/bin/run", Want: true},
		{Name: "the absolute spelling of a ~ head", First: "/home/me/bin/run", Want: true},
		{Name: "a bare name equal to a ~ head's basename", First: "run", Want: true},
		{Name: "a ~ token whose basename is a path head's", First: "~/timeout", Want: false},
		{Name: "another ~ path", First: "~/bin/other", Want: false},
		{Name: "an unresolvable ~user", First: "~ghost/bin/run", Want: false},
		{Name: "a ~user path is another file", First: "~alice/bin/run", Want: false},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			if got := s.CollidesWithWrapper(tt.First); got != tt.Want {
				t.Errorf("CollidesWithWrapper(%q) = %v, want %v", tt.First, got, tt.Want)
			}
		})
	}
}

// TestTildeEmptyResolutionPrefillFallsBackToAsWritten pins the fallback when resolution produced no
// path at all. The pre-fill is the token as written, and the edit check refuses it. The failed
// resolution with a joined cwd is covered by TestTildeFailedResolutionPrefillIsAbsolute.
func TestTildeEmptyResolutionPrefillFallsBackToAsWritten(t *testing.T) {
	t.Parallel()

	s := loadTilde(t, "", "version: 1\n")

	argv := []string{"~/bin/foo", "x"}
	suggestion := s.Ruleset().Suggest(argv, &model.Resolution{Argv: argv})

	if suggestion.Tokens[0] != "~/bin/foo" {
		t.Fatalf("pre-fill head = %q, want argv[0] as written", suggestion.Tokens[0])
	}

	targets := []EditTarget{{Argv: argv, Resolution: &model.Resolution{Argv: argv}}}
	if _, rejection := s.CheckEdit(suggestion.Tokens, false, targets); rejection == nil || rejection.Reason != EditMismatch {
		t.Errorf("rejection = %+v, want the as-written ~ pre-fill flagged as a mismatch", rejection)
	}
}

func TestTildeAppendKeepsSpellingAndStoredHome(t *testing.T) {
	root := writeProject(t, "version: 1\n")
	s, err := Load(LoadOptions{
		GlobalPath:  filepath.Join(t.TempDir(), "absent.yaml"),
		ProjectRoot: root,
		Home:        tildeHomeFn,
		LookupUser:  tildeLookup,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	t.Setenv("HOME", "/somewhere/else")

	if err := s.AppendProjectAllowPrefix([]string{"~/bin/foo"}, WriteDirect); err != nil {
		t.Fatalf("AppendProjectAllowPrefix: %v", err)
	}

	raw, err := os.ReadFile(ProjectPath(root, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "~/bin/foo") {
		t.Errorf("written file lost the ~ spelling:\n%s", raw)
	}

	if got := s.Project.Doc.Allow[0].tildeHead; got != "/home/me/bin/foo" {
		t.Errorf("re-parsed head = %q, want the home the load read", got)
	}

	live := Subject{Argv: []string{"foo"}, Canonical: []string{"/home/me/bin/foo"}}
	if got := s.Ruleset().Match(live).Decision; got != DecisionRun {
		t.Errorf("live match = %v, want run", got)
	}

	other := Subject{Argv: []string{"foo"}, Canonical: []string{"/somewhere/else/bin/foo"}}
	if got := s.Ruleset().Match(other).Decision; got != DecisionPrompt {
		t.Errorf("a fresh $HOME matched, got %v, want prompt", got)
	}
}

func TestTildeAppendRefusesAnInertRule(t *testing.T) {
	t.Parallel()

	root := writeProject(t, "version: 1\n")
	s := loadTildeAt(t, root)
	before, _ := os.ReadFile(ProjectPath(root, ""))

	if err := s.AppendProjectAllowPrefix([]string{"~ghost/bin/foo"}, WriteDirect); err == nil {
		t.Fatal("AppendProjectAllowPrefix succeeded, want a refusal")
	}

	after, _ := os.ReadFile(ProjectPath(root, ""))
	if string(before) != string(after) {
		t.Errorf("file changed:\n%s", after)
	}
}

func loadTildeAt(t *testing.T, root string) *Store {
	t.Helper()

	s, err := Load(LoadOptions{
		GlobalPath:  filepath.Join(t.TempDir(), "absent.yaml"),
		ProjectRoot: root,
		Home:        tildeHomeFn,
		LookupUser:  tildeLookup,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return s
}

func TestTildeReloadMergeRefusesAnUnresolvableDenyBeforeWriting(t *testing.T) {
	t.Parallel()

	root := writeProject(t, "version: 1\nallow:\n  - prefix: [make]\n")
	s := loadTildeAt(t, root)

	disk := "version: 1\nallow:\n  - prefix: [make]\ndeny:\n  - prefix: [~ghost/bin/evil]\n"
	if err := os.WriteFile(ProjectPath(root, ""), []byte(disk), 0o600); err != nil {
		t.Fatal(err)
	}

	err := s.AppendProjectAllowPrefix([]string{"go", "test"}, WriteReloadMerge)
	if err == nil || !strings.Contains(err.Error(), "~ghost/bin/evil") {
		t.Fatalf("error = %v, want a refusal naming the token", err)
	}

	raw, _ := os.ReadFile(ProjectPath(root, ""))
	if string(raw) != disk {
		t.Errorf("file changed before the refusal:\n%s", raw)
	}
}

func TestTildeReloadMergeKeepsAnInertAllowRule(t *testing.T) {
	t.Parallel()

	root := writeProject(t, "version: 1\n")
	s := loadTildeAt(t, root)

	disk := "version: 1\nallow:\n  - prefix: [~ghost/bin/x]\n"
	if err := os.WriteFile(ProjectPath(root, ""), []byte(disk), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := s.AppendProjectAllowPrefix([]string{"make"}, WriteReloadMerge); err != nil {
		t.Fatalf("AppendProjectAllowPrefix: %v", err)
	}

	raw, _ := os.ReadFile(ProjectPath(root, ""))
	if !strings.Contains(string(raw), "~ghost/bin/x") {
		t.Errorf("the hand-written rule was dropped:\n%s", raw)
	}
}

type tildeProjectConfigTest struct {
	Name  string
	Files func(t *testing.T, root string) []string
	Rules string
	Argv  []string
	Canon string
	Want  Decision
}

var tildeProjectConfigTests = []tildeProjectConfigTest{
	{
		Name: "an absolute value names that exact file",
		Files: func(t *testing.T, root string) []string {
			return []string{writeAt(t, filepath.Join(t.TempDir(), "rules.yaml"), "version: 1\nallow:\n  - prefix: [make]\n")}
		},
		Argv: []string{"make"}, Want: DecisionRun,
	},
	{
		Name: "a ~ value names that exact file",
		Files: func(t *testing.T, root string) []string {
			return []string{"~/cfg/rules.yaml"}
		},
		Argv: []string{"make"}, Want: DecisionRun,
	},
	{
		Name: "a relative value joins the project root",
		Files: func(t *testing.T, root string) []string {
			writeAt(t, filepath.Join(root, "alt.yaml"), "version: 1\nallow:\n  - prefix: [make]\n")
			return []string{"alt.yaml"}
		},
		Argv: []string{"make"}, Want: DecisionRun,
	},
	{
		Name: "project-relative tokens resolve against the project root, not the file",
		Files: func(t *testing.T, root string) []string {
			return []string{writeAt(t, filepath.Join(t.TempDir(), "rules.yaml"), "version: 1\nallow:\n  - prefix: [./bin/tool]\n")}
		},
		Argv: []string{"tool"}, Canon: "ROOT/bin/tool", Want: DecisionRun,
	},
}

func writeAt(t *testing.T, path, content string) string {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestTildeProjectConfig(t *testing.T) {
	t.Parallel()

	for _, tt := range tildeProjectConfigTests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			home := t.TempDir()
			writeAt(t, filepath.Join(home, "cfg", "rules.yaml"), "version: 1\nallow:\n  - prefix: [make]\n")

			s, err := Load(LoadOptions{
				GlobalPath:   filepath.Join(t.TempDir(), "absent.yaml"),
				ProjectRoot:  root,
				ProjectFiles: tt.Files(t, root),
				Home:         func() (string, error) { return home, nil },
				LookupUser:   tildeLookup,
			})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			canon := []string(nil)
			if tt.Canon != "" {
				canon = []string{strings.ReplaceAll(tt.Canon, "ROOT", root)}
			}

			subject := Subject{Argv: tt.Argv, Canonical: canon, Resolved: canon}
			if got := s.Ruleset().Match(subject).Decision; got != tt.Want {
				t.Errorf("Match = %v, want %v (project file %s)", got, tt.Want, s.Project.Path)
			}
		})
	}
}

func TestTildeProjectConfigUnresolvableUserFails(t *testing.T) {
	t.Parallel()

	opts := tildeOptions(t, "", "version: 1\n")
	opts.ProjectFiles = []string{".cg.yaml", "~ghost/rules.yaml"}

	if _, err := Load(opts); err == nil || !strings.Contains(err.Error(), "~ghost/rules.yaml") {
		t.Errorf("Load error = %v, want it to name the token", err)
	}

	if _, _, err := Diagnose(opts); err == nil || !strings.Contains(err.Error(), "~ghost/rules.yaml") {
		t.Errorf("Diagnose error = %v, want it to name the token", err)
	}
}

func TestTildeProjectConfigWriteTargetWhenNoCandidateExists(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	opts := LoadOptions{
		GlobalPath:   filepath.Join(t.TempDir(), "absent.yaml"),
		ProjectRoot:  t.TempDir(),
		ProjectFiles: []string{"~/new/rules.yaml"},
		Home:         func() (string, error) { return home, nil },
	}

	s, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := filepath.Join(home, "new", "rules.yaml"); s.Project.Path != want {
		t.Errorf("project path = %q, want %q", s.Project.Path, want)
	}
}

type tildeWordingTest struct {
	Name       string
	PasswdOnly bool
	Lookup     func(string) (string, error)

	WantPasswd bool
	WantText   string
}

var tildeWordingTests = []tildeWordingTest{
	{Name: "passwd-only build, unknown user", PasswdOnly: true, Lookup: func(n string) (string, error) { return "", user.UnknownUserError(n) }, WantPasswd: true, WantText: "absolutely"},
	{Name: "passwd-only build, empty home field", PasswdOnly: true, Lookup: func(string) (string, error) { return "", nil }, WantText: "not an absolute path"},
	{Name: "passwd-only build, relative home field", PasswdOnly: true, Lookup: func(string) (string, error) { return "rel/home", nil }, WantText: "not an absolute path"},
	{Name: "passwd-only build, other lookup failure", PasswdOnly: true, Lookup: func(string) (string, error) { return "", errors.New("boom") }, WantText: "boom"},
	{Name: "directory-service build, unknown user", Lookup: func(n string) (string, error) { return "", user.UnknownUserError(n) }, WantText: "absolutely"},
}

func TestTildeUnresolvedUserWording(t *testing.T) {
	t.Parallel()

	for _, tt := range tildeWordingTests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			env := &tildeEnv{home: tildeHome, lookupUser: tt.Lookup, passwdOnly: tt.PasswdOnly}

			_, err := env.expand("~ghost/x")
			if err == nil {
				t.Fatal("expand succeeded, want an error")
			}

			if got := strings.Contains(err.Error(), "/etc/passwd"); got != tt.WantPasswd {
				t.Errorf("error %q mentions /etc/passwd = %v, want %v", err, got, tt.WantPasswd)
			}
			if !strings.Contains(err.Error(), tt.WantText) {
				t.Errorf("error %q missing %q", err, tt.WantText)
			}
		})
	}
}

func TestTildePasswdOnlyBuildReportsUnknownUserByType(t *testing.T) {
	t.Parallel()

	if !lookupReadsPasswdOnly {
		t.Skip("this build asks the operating system for users")
	}

	_, err := lookupUserHome("no-such-user-for-cg-test")

	var unknown user.UnknownUserError
	if !errors.As(err, &unknown) {
		t.Errorf("lookupUserHome error = %v, want user.UnknownUserError, which the /etc/passwd wording relies on", err)
	}
}

func TestTildeRoundTripKeepsEveryWrittenToken(t *testing.T) {
	t.Parallel()

	disk := "version: 1\nallow:\n  - prefix: [~/bin/a, -v]\n  - exact: [\"~alice/bin/b\"]\n  - wrapper: [~/bin/nice, -n, \"1\"]\n" +
		"deny:\n  - prefix: [~/bin/evil]\n"
	root := writeProject(t, disk)
	s := loadTildeAt(t, root)

	if err := s.AppendProjectAllowPrefix([]string{"~/bin/new"}, WriteDirect); err != nil {
		t.Fatalf("AppendProjectAllowPrefix: %v", err)
	}

	raw, err := os.ReadFile(ProjectPath(root, ""))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"~/bin/a", "~alice/bin/b", "~/bin/nice", "~/bin/evil", "~/bin/new"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("written file lost %q:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), tildeHome) || strings.Contains(string(raw), tildeAlice) {
		t.Errorf("written file holds an expanded path:\n%s", raw)
	}
}

// TestTildePrefillAndAbsoluteEditWithDivergentCanonical covers what the mcp prompt tests cannot
// reach. The resolved path and the canonical path differ. Both the ~ pre-fill and the absolute
// edit must pass the edit check, and the absolute rule must match live on the resolved path.
func TestTildePrefillAndAbsoluteEditWithDivergentCanonical(t *testing.T) {
	t.Parallel()

	root := writeProject(t, "version: 1\n")
	s := loadTildeAt(t, root)

	argv := []string{"foo", "x"}
	res := &model.Resolution{Argv: argv, Resolved: "/home/me/bin/foo", Canonical: "/opt/real/foo"}
	targets := []EditTarget{{Argv: argv, Resolution: res}}

	pre := s.Ruleset().Suggest(argv, res).Tokens
	if !slices.Equal(pre, []string{"~/bin/foo", "x"}) {
		t.Fatalf("pre-fill = %v, want the ~ spelling", pre)
	}

	kind, rejection := s.CheckEdit(pre, false, targets)
	if rejection != nil || kind != KindExact {
		t.Fatalf("CheckEdit(pre-fill) = %v, %+v", kind, rejection)
	}

	absolute := []string{"/home/me/bin/foo", "x"}
	if _, rejection := s.CheckEdit(absolute, false, targets); rejection != nil {
		t.Fatalf("CheckEdit(absolute) = %+v, want accepted", rejection)
	}
	if err := s.AppendProjectAllowExact(absolute, WriteDirect); err != nil {
		t.Fatalf("AppendProjectAllowExact(absolute): %v", err)
	}

	subject := Subject{Argv: argv, Canonical: []string{"/opt/real/foo", "x"}, Resolved: []string{"/home/me/bin/foo", "x"}}
	if got := s.Ruleset().Match(subject).Decision; got != DecisionRun {
		t.Errorf("live match after the absolute edit = %v, want run", got)
	}
}

// TestTildeReloadMergeKeepsBothSpellings pins that rules compare as written. The in-memory rule
// is the absolute spelling and the disk holds the ~ spelling of the same program, so the merge
// keeps both.
func TestTildeReloadMergeKeepsBothSpellings(t *testing.T) {
	t.Parallel()

	root := writeProject(t, "version: 1\nallow:\n  - prefix: [/home/me/bin/foo]\n")
	s := loadTildeAt(t, root)

	disk := "version: 1\nallow:\n  - prefix: [~/bin/foo]\n"
	if err := os.WriteFile(ProjectPath(root, ""), []byte(disk), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := s.AppendProjectAllowPrefix([]string{"make"}, WriteReloadMerge); err != nil {
		t.Fatalf("AppendProjectAllowPrefix: %v", err)
	}

	raw, _ := os.ReadFile(ProjectPath(root, ""))
	for _, want := range []string{"~/bin/foo", "/home/me/bin/foo", "make"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("merged file lost %q:\n%s", want, raw)
		}
	}
}

// TestTildeFailedResolutionPrefillIsAbsolute covers a program that does not exist, written
// ~/bin/foo. It runs as <cwd>/~/bin/foo, and resolution has already joined the cwd, so the
// pre-fill is that absolute path. It is never the home-relative spelling, and the edit check
// refuses the home-relative one because the program has no canonical path to match.
func TestTildeFailedResolutionPrefillIsAbsolute(t *testing.T) {
	t.Parallel()

	s := loadTilde(t, "", "version: 1\n")
	cwd := t.TempDir()

	argv := []string{"~/bin/foo", "x"}
	res, err := model.ResolveCommand(argv, cwd)
	if err == nil {
		t.Fatal("ResolveCommand succeeded for a program that does not exist")
	}

	suggestion := s.Ruleset().Suggest(argv, res)
	if want := filepath.Join(cwd, "~", "bin", "foo"); suggestion.Tokens[0] != want {
		t.Errorf("pre-fill head = %q, want %q", suggestion.Tokens[0], want)
	}

	targets := []EditTarget{{Argv: argv, Resolution: res}}
	for _, tokens := range [][]string{suggestion.Tokens, argv} {
		if _, rejection := s.CheckEdit(tokens, false, targets); rejection == nil {
			t.Errorf("CheckEdit(%v) accepted a program with no canonical path", tokens)
		}
	}
}
