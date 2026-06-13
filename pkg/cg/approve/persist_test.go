package approve

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type suggestPrefixTest struct {
	Name string
	Argv []string
	Want []string
}

var suggestPrefixTests = []suggestPrefixTest{
	{Name: "single program", Argv: []string{"make"}, Want: []string{"make"}},
	{Name: "multi-verb make", Argv: []string{"make", "test"}, Want: []string{"make", "test"}},
	{Name: "make multiple targets", Argv: []string{"make", "foo", "bar"}, Want: []string{"make", "foo"}},
	{Name: "make flag before target", Argv: []string{"make", "-j8", "test"}, Want: []string{"make"}},
	{Name: "make -C flag", Argv: []string{"make", "-C", "/path", "foo"}, Want: []string{"make"}},
	{Name: "program with arg", Argv: []string{"echo", "hi"}, Want: []string{"echo"}},
	{Name: "multi-verb go", Argv: []string{"go", "test", "./..."}, Want: []string{"go", "test"}},
	{Name: "go flag before subcommand", Argv: []string{"go", "-v", "test"}, Want: []string{"go"}},
	{Name: "multi-verb git", Argv: []string{"git", "status"}, Want: []string{"git", "status"}},
	{Name: "git flag before subcommand", Argv: []string{"git", "--no-pager", "log"}, Want: []string{"git"}},
	{Name: "multi-verb path basename", Argv: []string{"/usr/bin/kubectl", "get", "pods"}, Want: []string{"/usr/bin/kubectl", "get"}},
	{Name: "multi-verb no subcommand", Argv: []string{"go"}, Want: []string{"go"}},
	{Name: "empty", Argv: nil, Want: nil},
}

func TestSuggestPrefix(t *testing.T) {
	t.Parallel()

	for _, tt := range suggestPrefixTests {
		t.Run(tt.Name, func(t *testing.T) {
			got := SuggestPrefix(tt.Argv)
			if !reflect.DeepEqual(got, tt.Want) {
				t.Errorf("SuggestPrefix(%v) = %v, want %v", tt.Argv, got, tt.Want)
			}
		})
	}
}

// loadProject loads a store with the global layer pointed at a nonexistent path
// so the real ~/.config/cg/approve.yaml is never read.
func loadProject(t *testing.T, root string) *Store {
	t.Helper()
	s, err := Load(LoadOptions{
		GlobalPath:  filepath.Join(t.TempDir(), "no-global.yaml"),
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return s
}

func TestAppendCreatesProjectFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := loadProject(t, root)
	if s.Project.Present {
		t.Fatalf("project layer should start absent")
	}

	if err := s.AppendProjectAllowPrefix([]string{"make"}, WriteDirect); err != nil {
		t.Fatalf("AppendProjectAllowPrefix: %v", err)
	}

	raw, err := os.ReadFile(ProjectPath(root, ""))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "version: 1") {
		t.Errorf("written file missing version:\n%s", got)
	}
	if !strings.Contains(got, "prefix: [make]") {
		t.Errorf("written file missing flow-style prefix rule:\n%s", got)
	}

	// The file must reload cleanly and the rule must be live.
	reloaded := loadProject(t, root)
	if got := reloaded.Ruleset().Match([]string{"make", "build"}); got.Decision != DecisionRun {
		t.Errorf("reloaded match = %v, want run", got.Decision)
	}
}

func TestAppendUpdatesLiveMatcher(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := loadProject(t, root)

	if got := s.Ruleset().Match([]string{"make"}); got.Decision != DecisionPrompt {
		t.Fatalf("before append, match = %v, want prompt", got.Decision)
	}
	if err := s.AppendProjectAllowPrefix([]string{"make"}, WriteDirect); err != nil {
		t.Fatalf("AppendProjectAllowPrefix: %v", err)
	}
	if got := s.Ruleset().Match([]string{"make"}); got.Decision != DecisionRun {
		t.Errorf("after append, match = %v, want run (live swap)", got.Decision)
	}
}

func TestAppendPreservesCommentsAndQuotesTokens(t *testing.T) {
	t.Parallel()

	content := "version: 1\n# keep this comment\nallow:\n  - prefix: [go, test] # trailing\n"
	root := writeProject(t, content)
	s := loadProject(t, root)

	if err := s.AppendProjectAllowPrefix([]string{"weird", "yes"}, WriteDirect); err != nil {
		t.Fatalf("AppendProjectAllowPrefix: %v", err)
	}

	raw, err := os.ReadFile(ProjectPath(root, ""))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "# keep this comment") {
		t.Errorf("comment not preserved:\n%s", got)
	}
	// yes must round-trip as a string, not the boolean true.
	if !strings.Contains(got, "yes") || strings.Contains(got, "true") {
		t.Errorf("token yes mis-rendered:\n%s", got)
	}

	// Existing rule survives and reloads alongside the new one.
	reloaded := loadProject(t, root)
	if len(reloaded.Project.Doc.Allow) != 2 {
		t.Errorf("allow count = %d, want 2", len(reloaded.Project.Doc.Allow))
	}
	if got := reloaded.Ruleset().Match([]string{"go", "test"}); got.Decision != DecisionRun {
		t.Errorf("existing rule lost: match = %v, want run", got.Decision)
	}
	if got := reloaded.Ruleset().Match([]string{"weird", "yes"}); got.Decision != DecisionRun {
		t.Errorf("new rule missing: match = %v, want run", got.Decision)
	}
}

func TestAppendSortsAndDedupesEntries(t *testing.T) {
	t.Parallel()

	// Out-of-order entries with a hand-added duplicate of [make].
	content := "version: 1\nallow:\n  - prefix: [make]\n  - prefix: [echo]\n  - prefix: [make]\n"
	root := writeProject(t, content)
	s := loadProject(t, root)

	if err := s.AppendProjectAllowPrefix([]string{"go", "test"}, WriteDirect); err != nil {
		t.Fatalf("AppendProjectAllowPrefix: %v", err)
	}

	raw, err := os.ReadFile(ProjectPath(root, ""))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	got := string(raw)

	// The duplicate [make] collapses to one entry.
	if n := strings.Count(got, "prefix: [make]"); n != 1 {
		t.Errorf("expected one [make] entry after dedupe, got %d:\n%s", n, got)
	}

	// Entries are ordered by command text: echo, go test, make.
	idxEcho := strings.Index(got, "prefix: [echo]")
	idxGo := strings.Index(got, "prefix: [go, test]")
	idxMake := strings.Index(got, "prefix: [make]")
	if idxEcho < 0 || idxGo < 0 || idxMake < 0 {
		t.Fatalf("missing an expected entry:\n%s", got)
	}
	if !(idxEcho < idxGo && idxGo < idxMake) {
		t.Errorf("entries not sorted by command text (echo < go test < make):\n%s", got)
	}
}

func TestCheckProjectDivergence(t *testing.T) {
	t.Parallel()

	content := "version: 1\nallow:\n  - prefix: [make]\n"
	root := writeProject(t, content)
	s := loadProject(t, root)

	changed, current, err := s.CheckProjectDivergence()
	if err != nil {
		t.Fatalf("CheckProjectDivergence: %v", err)
	}
	if changed {
		t.Errorf("unchanged file reported changed")
	}
	if string(current) != content {
		t.Errorf("current = %q, want %q", current, content)
	}

	// Rewrite the file out of band.
	if err := os.WriteFile(ProjectPath(root, ""), []byte(content+"  - prefix: [go]\n"), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	changed, _, err = s.CheckProjectDivergence()
	if err != nil {
		t.Fatalf("CheckProjectDivergence: %v", err)
	}
	if !changed {
		t.Errorf("modified file reported unchanged")
	}
}

func TestCheckDivergenceAbsentStaysAbsent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := loadProject(t, root)

	changed, current, err := s.CheckProjectDivergence()
	if err != nil {
		t.Fatalf("CheckProjectDivergence: %v", err)
	}
	if changed {
		t.Errorf("absent-then-absent reported changed")
	}
	if current != nil {
		t.Errorf("current = %q, want nil", current)
	}
}

func TestReloadMergeUnionsRules(t *testing.T) {
	t.Parallel()

	root := writeProject(t, "version: 1\nallow:\n  - prefix: [make]\n")
	s := loadProject(t, root)

	// Remember a first rule directly; in-memory now has make + go test.
	if err := s.AppendProjectAllowPrefix([]string{"go", "test"}, WriteDirect); err != nil {
		t.Fatalf("first append: %v", err)
	}

	// Simulate a hand-edit on disk that adds a different rule and drops nothing.
	disk := "version: 1\nallow:\n  - prefix: [make]\n  - prefix: [npm, ci]\n"
	if err := os.WriteFile(ProjectPath(root, ""), []byte(disk), 0o600); err != nil {
		t.Fatalf("hand edit: %v", err)
	}

	// Reload-merge should keep the disk rules and re-add the in-memory go test
	// rule, then add the new one.
	if err := s.AppendProjectAllowPrefix([]string{"cargo", "build"}, WriteReloadMerge); err != nil {
		t.Fatalf("reload-merge append: %v", err)
	}

	reloaded := loadProject(t, root)
	wants := [][]string{
		{"make"},
		{"npm", "ci", "extra"},
		{"go", "test"},
		{"cargo", "build", "--release"},
	}
	for _, argv := range wants {
		if got := reloaded.Ruleset().Match(argv); got.Decision != DecisionRun {
			t.Errorf("after reload-merge, match %v = %v, want run", argv, got.Decision)
		}
	}
}

func TestOverwriteDropsDiskChanges(t *testing.T) {
	t.Parallel()

	root := writeProject(t, "version: 1\nallow:\n  - prefix: [make]\n")
	s := loadProject(t, root)

	// Hand-edit on disk adds a rule cg's in-memory view does not have.
	if err := os.WriteFile(ProjectPath(root, ""), []byte("version: 1\nallow:\n  - prefix: [make]\n  - prefix: [npm, ci]\n"), 0o600); err != nil {
		t.Fatalf("hand edit: %v", err)
	}

	if err := s.AppendProjectAllowPrefix([]string{"go", "vet"}, WriteOverwrite); err != nil {
		t.Fatalf("overwrite append: %v", err)
	}

	reloaded := loadProject(t, root)
	if got := reloaded.Ruleset().Match([]string{"npm", "ci"}); got.Decision == DecisionRun {
		t.Errorf("overwrite kept the dropped on-disk rule")
	}
	if got := reloaded.Ruleset().Match([]string{"go", "vet"}); got.Decision != DecisionRun {
		t.Errorf("overwrite did not add the new rule")
	}
	if got := reloaded.Ruleset().Match([]string{"make"}); got.Decision != DecisionRun {
		t.Errorf("overwrite lost the in-memory make rule")
	}
}
