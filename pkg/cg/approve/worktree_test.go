package approve

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ripta/rt/pkg/cg/model"
)

const worktreeScript = "scripts/tool.sh"

// worktreeFixture is a real repository with one linked worktree, each holding its own copy of
// worktreeScript. It uses real git because the layout of a linked worktree's .git file and admin
// directory is git's contract, and a hand-written copy could only prove agreement with itself.
type worktreeFixture struct {
	Main     string
	Worktree string
}

func newWorktreeFixture(t *testing.T) worktreeFixture {
	t.Helper()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}

	f := worktreeFixture{Main: filepath.Join(base, "main"), Worktree: filepath.Join(base, "main", ".claude", "worktrees", "wt")}

	initRepo(t, f.Main)
	writeScript(t, filepath.Join(f.Main, worktreeScript))
	runGit(t, f.Main, "add", worktreeScript)
	runGit(t, f.Main, "commit", "-m", "script")
	runGit(t, f.Main, "worktree", "add", "-b", "wt", f.Worktree)

	return f
}

func initRepo(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}

	runGit(t, dir, "init", "-b", "main")
}

func writeScript(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func loadWorktreeStore(t *testing.T, root, rules string) *Store {
	t.Helper()

	path := filepath.Join(root, DefaultProjectFile)
	if err := os.WriteFile(path, []byte(rules), 0o644); err != nil {
		t.Fatalf("writing rules: %v", err)
	}

	store, err := Load(LoadOptions{GlobalPath: filepath.Join(t.TempDir(), "absent.yaml"), ProjectRoot: root})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return store
}

// decide evaluates a command typed as typed from cwd, the way the gate does.
func decide(t *testing.T, store *Store, cwd, typed string) Decision {
	t.Helper()

	argv := []string{typed, "--flag"}

	res, err := model.ResolveCommand(argv, cwd)
	if err != nil {
		t.Fatalf("ResolveCommand(%v, %s): %v", argv, cwd, err)
	}

	subject := Subject{Argv: argv, Canonical: res.CanonicalArgv(), Resolved: res.ResolvedArgv()}

	return store.Ruleset().Evaluate(subject, nil, CommandResolver(cwd)).Decision
}

// TestRelativeRulesFollowLinkedWorktrees proves a project-relative rule names the same file in
// every checkout of the project. A regression that compares only the project root's path turns the
// first two cases back into prompts, and a deny rule silently stops applying in a worktree.
func TestRelativeRulesFollowLinkedWorktrees(t *testing.T) {
	t.Parallel()

	f := newWorktreeFixture(t)

	allow := "version: 1\nallow:\n  - prefix: [./" + worktreeScript + "]\n"
	deny := "version: 1\ndeny:\n  - prefix: [./" + worktreeScript + "]\n"

	if got := decide(t, loadWorktreeStore(t, f.Main, allow), f.Worktree, "./"+worktreeScript); got != DecisionRun {
		t.Errorf("relative allow in a linked worktree = %v, want run", got)
	}
	if got := decide(t, loadWorktreeStore(t, f.Main, deny), f.Worktree, "./"+worktreeScript); got != DecisionRefuse {
		t.Errorf("relative deny in a linked worktree = %v, want refuse", got)
	}
	if got := decide(t, loadWorktreeStore(t, f.Main, allow), f.Main, "./"+worktreeScript); got != DecisionRun {
		t.Errorf("relative allow in the root checkout = %v, want run", got)
	}
}

// TestRelativeRulesFollowFromAWorktreeSession covers a session whose project root is the linked
// worktree itself. The rule names the same file in the root checkout there too.
func TestRelativeRulesFollowFromAWorktreeSession(t *testing.T) {
	t.Parallel()

	f := newWorktreeFixture(t)
	store := loadWorktreeStore(t, f.Worktree, "version: 1\nallow:\n  - prefix: [./"+worktreeScript+"]\n")

	if got := decide(t, store, f.Main, "./"+worktreeScript); got != DecisionRun {
		t.Errorf("relative allow from a worktree session = %v, want run", got)
	}
}

// TestWorktreeMatchingStaysScoped proves the widening reaches only relative rules and only real
// worktrees of this repository. Each case is a way the path comparison could over-match and run a
// file nobody approved.
func TestWorktreeMatchingStaysScoped(t *testing.T) {
	t.Parallel()

	relative := "version: 1\nallow:\n  - prefix: [./" + worktreeScript + "]\n"

	t.Run("absolute rule keeps naming one file", func(t *testing.T) {
		t.Parallel()

		f := newWorktreeFixture(t)
		rules := "version: 1\nallow:\n  - prefix: [" + filepath.Join(f.Main, worktreeScript) + "]\n"

		if got := decide(t, loadWorktreeStore(t, f.Main, rules), f.Worktree, "./"+worktreeScript); got != DecisionPrompt {
			t.Errorf("absolute rule on a worktree copy = %v, want prompt", got)
		}
	})

	t.Run("worktree of another repository", func(t *testing.T) {
		t.Parallel()

		f := newWorktreeFixture(t)
		other := newWorktreeFixture(t)

		if got := decide(t, loadWorktreeStore(t, f.Main, relative), other.Worktree, "./"+worktreeScript); got != DecisionPrompt {
			t.Errorf("another repository's worktree = %v, want prompt", got)
		}
	})

	t.Run("directory that forges a worktree's git file", func(t *testing.T) {
		t.Parallel()

		f := newWorktreeFixture(t)

		forged := filepath.Join(t.TempDir(), "forged")
		writeScript(t, filepath.Join(forged, worktreeScript))

		gitFile, err := os.ReadFile(filepath.Join(f.Worktree, ".git"))
		if err != nil {
			t.Fatalf("reading worktree git file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(forged, ".git"), gitFile, 0o644); err != nil {
			t.Fatalf("writing forged git file: %v", err)
		}

		if got := decide(t, loadWorktreeStore(t, f.Main, relative), forged, "./"+worktreeScript); got != DecisionPrompt {
			t.Errorf("forged worktree = %v, want prompt", got)
		}
	})

	t.Run("directory that forges its own admin directory", func(t *testing.T) {
		t.Parallel()

		f := newWorktreeFixture(t)

		forged := filepath.Join(t.TempDir(), "forged")
		writeScript(t, filepath.Join(forged, worktreeScript))

		admin := filepath.Join(forged, "admin")
		if err := os.MkdirAll(admin, 0o755); err != nil {
			t.Fatalf("creating %s: %v", admin, err)
		}

		files := map[string]string{
			filepath.Join(forged, ".git"):     "gitdir: " + admin + "\n",
			filepath.Join(admin, "commondir"): filepath.Join(f.Main, ".git") + "\n",
			filepath.Join(admin, "gitdir"):    filepath.Join(forged, ".git") + "\n",
		}
		for path, content := range files {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("writing %s: %v", path, err)
			}
		}

		if got := decide(t, loadWorktreeStore(t, f.Main, relative), forged, "./"+worktreeScript); got != DecisionPrompt {
			t.Errorf("forged admin directory = %v, want prompt", got)
		}
	})

	t.Run("repository nested inside a worktree", func(t *testing.T) {
		t.Parallel()

		f := newWorktreeFixture(t)

		nested := filepath.Join(f.Worktree, "vendor", "nested")
		initRepo(t, nested)
		writeScript(t, filepath.Join(nested, worktreeScript))

		if got := decide(t, loadWorktreeStore(t, f.Main, relative), nested, "./"+worktreeScript); got != DecisionPrompt {
			t.Errorf("nested repository = %v, want prompt", got)
		}
	})

	t.Run("different path in the worktree", func(t *testing.T) {
		t.Parallel()

		f := newWorktreeFixture(t)
		writeScript(t, filepath.Join(f.Worktree, "scripts", "other.sh"))

		if got := decide(t, loadWorktreeStore(t, f.Main, relative), f.Worktree, "./scripts/other.sh"); got != DecisionPrompt {
			t.Errorf("other script in the worktree = %v, want prompt", got)
		}
	})
}

type suggestPortableTest struct {
	Name string
	Cwd  string
	Argv []string
	Want string
}

// TestSuggestPrefillsAPortableRule proves a form for a file in the project offers a rule that works
// for anyone in any checkout. A regression pre-fills the absolute path again, and remember then
// commits a rule holding one person's home directory and one worktree's name. The program outside
// the project keeps its absolute path, because the same relative spelling would name nothing.
func TestSuggestPrefillsAPortableRule(t *testing.T) {
	t.Parallel()

	f := newWorktreeFixture(t)
	store := loadWorktreeStore(t, f.Main, "version: 1\n")

	outsideDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}

	outside := filepath.Join(outsideDir, "tool")
	writeScript(t, outside)

	tests := []suggestPortableTest{
		{Name: "linked worktree", Cwd: f.Worktree, Argv: []string{"./" + worktreeScript, "go"}, Want: "./" + worktreeScript},
		{Name: "root checkout", Cwd: f.Main, Argv: []string{"./" + worktreeScript, "go"}, Want: "./" + worktreeScript},
		{Name: "outside the project", Cwd: f.Main, Argv: []string{outside, "go"}, Want: outside},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			res, err := model.ResolveCommand(test.Argv, test.Cwd)
			if err != nil {
				t.Fatalf("ResolveCommand: %v", err)
			}

			tokens := store.Ruleset().Suggest(test.Argv, res).Tokens
			if tokens[0] != test.Want {
				t.Fatalf("suggested first token = %q, want %q", tokens[0], test.Want)
			}

			if _, rejection := store.CheckEdit(tokens, false, []EditTarget{{Argv: test.Argv, Resolution: res}}); rejection != nil {
				t.Errorf("the pre-filled rule %v does not match its own command", tokens)
			}
		})
	}
}

// TestCheckEditAcceptsARelativeRuleInAWorktree pins the loop that kept re-asking: a form in a linked
// worktree, trimmed to the project-relative script, was refused as not matching the command.
func TestCheckEditAcceptsARelativeRuleInAWorktree(t *testing.T) {
	t.Parallel()

	f := newWorktreeFixture(t)
	store := loadWorktreeStore(t, f.Main, "version: 1\n")

	argv := []string{"./" + worktreeScript, "xcodebuild", "test"}

	res, err := model.ResolveCommand(argv, f.Worktree)
	if err != nil {
		t.Fatalf("ResolveCommand: %v", err)
	}

	kind, rejection := store.CheckEdit([]string{"./" + worktreeScript}, true, []EditTarget{{Argv: argv, Resolution: res}})
	if rejection != nil {
		t.Fatalf("CheckEdit rejected the relative rule: %+v", rejection)
	}
	if kind != KindPrefix {
		t.Errorf("kind = %v, want prefix", kind)
	}
}
