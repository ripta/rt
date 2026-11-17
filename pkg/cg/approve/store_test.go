package approve

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeGlobal writes a global layer file in a temp dir and returns its path.
func writeGlobal(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "approve.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing global file: %v", err)
	}

	return path
}

// writeProject writes a project layer file under <root>/.cg.yaml and returns
// the project root.
func writeProject(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".cg.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("writing project file: %v", err)
	}

	return root
}

func TestLoadBothMissing(t *testing.T) {
	t.Parallel()

	missingGlobal := filepath.Join(t.TempDir(), "nope.yaml")
	emptyRoot := t.TempDir()

	s, err := Load(LoadOptions{GlobalPath: missingGlobal, ProjectRoot: emptyRoot})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if s.Global.Present || s.Project.Present {
		t.Errorf("expected both layers absent, got global=%v project=%v", s.Global.Present, s.Project.Present)
	}
	rs := s.Ruleset()
	if rs.Mode != ModeEnforce {
		t.Errorf("mode = %q, want enforce", rs.Mode)
	}
	if len(rs.Deny) != len(builtinDenyRules()) {
		t.Errorf("deny count = %d, want %d (builtin only)", len(rs.Deny), len(builtinDenyRules()))
	}
	if len(rs.Allow) != 0 {
		t.Errorf("allow count = %d, want 0", len(rs.Allow))
	}
}

func TestLoadGlobalOnly(t *testing.T) {
	t.Parallel()

	global := writeGlobal(t, "version: 1\nallow:\n  - prefix: [go, test]\n")
	emptyRoot := t.TempDir()

	s, err := Load(LoadOptions{GlobalPath: global, ProjectRoot: emptyRoot})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !s.Global.Present {
		t.Errorf("global layer should be present")
	}
	if s.Project.Present {
		t.Errorf("project layer should be absent")
	}
	if s.Global.Node == nil {
		t.Errorf("present layer should retain its yaml.Node")
	}
	if len(s.Ruleset().Allow) != 1 {
		t.Errorf("allow count = %d, want 1", len(s.Ruleset().Allow))
	}
}

func TestLoadProjectOnly(t *testing.T) {
	t.Parallel()

	missingGlobal := filepath.Join(t.TempDir(), "nope.yaml")
	root := writeProject(t, "version: 1\nallow:\n  - prefix: [make]\n")

	s, err := Load(LoadOptions{GlobalPath: missingGlobal, ProjectRoot: root})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if s.Global.Present {
		t.Errorf("global layer should be absent")
	}
	if !s.Project.Present {
		t.Errorf("project layer should be present")
	}
	if len(s.Ruleset().Allow) != 1 {
		t.Errorf("allow count = %d, want 1", len(s.Ruleset().Allow))
	}
}

func TestLoadProjectFilesFirstExistingWins(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, ".cg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating .cg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "approve.yaml"), []byte("version: 1\nallow:\n  - prefix: [make]\n"), 0o600); err != nil {
		t.Fatalf("writing legacy project file: %v", err)
	}

	// The preferred .cg.yaml is absent, so the legacy path is loaded instead.
	s, err := Load(LoadOptions{
		GlobalPath:   filepath.Join(t.TempDir(), "nope.yaml"),
		ProjectRoot:  root,
		ProjectFiles: []string{".cg.yaml", ".cg/approve.yaml"},
	})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !s.Project.Present {
		t.Errorf("project layer should be present")
	}
	if want := filepath.Join(root, ".cg", "approve.yaml"); s.Project.Path != want {
		t.Errorf("Project.Path = %q, want %q", s.Project.Path, want)
	}
}

func TestLoadDefaultFilesFallBackToLegacy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, ".cg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating .cg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "approve.yaml"), []byte("version: 1\nallow:\n  - prefix: [make]\n"), 0o600); err != nil {
		t.Fatalf("writing legacy project file: %v", err)
	}

	// No ProjectFiles configured, so the default list finds the legacy file when
	// the preferred .cg.yaml is absent.
	s, err := Load(LoadOptions{
		GlobalPath:  filepath.Join(t.TempDir(), "nope.yaml"),
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !s.Project.Present {
		t.Errorf("project layer should be present")
	}
	if want := filepath.Join(root, ".cg", "approve.yaml"); s.Project.Path != want {
		t.Errorf("Project.Path = %q, want %q", s.Project.Path, want)
	}
}

func TestLoadProjectFilesNoneExistWritesFirst(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s, err := Load(LoadOptions{
		GlobalPath:   filepath.Join(t.TempDir(), "nope.yaml"),
		ProjectRoot:  root,
		ProjectFiles: []string{".cg.yaml", ".cg/approve.yaml"},
	})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if s.Project.Present {
		t.Errorf("project layer should be absent when no candidate exists")
	}
	// With nothing on disk, the first listed path is the write target.
	if want := filepath.Join(root, ".cg.yaml"); s.Project.Path != want {
		t.Errorf("Project.Path = %q, want first listed %q", s.Project.Path, want)
	}
}

func TestLoadProjectModeOverridesGlobal(t *testing.T) {
	t.Parallel()

	global := writeGlobal(t, "version: 1\nmode: enforce\n")
	root := writeProject(t, "version: 1\nmode: allow-all\n")

	s, err := Load(LoadOptions{GlobalPath: global, ProjectRoot: root})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if s.Ruleset().Mode != ModeAllowAll {
		t.Errorf("mode = %q, want allow-all (project overrides global)", s.Ruleset().Mode)
	}
}

func TestLoadUnionsLayers(t *testing.T) {
	t.Parallel()

	global := writeGlobal(t, "version: 1\ndeny:\n  - prefix: [git, push, --force]\nallow:\n  - prefix: [go, test]\nrestrict:\n  - prefix: [git]\n")
	root := writeProject(t, "version: 1\ndeny:\n  - prefix: [terraform, destroy]\nallow:\n  - prefix: [make]\nrestrict:\n  - prefix: [terraform]\n")

	s, err := Load(LoadOptions{GlobalPath: global, ProjectRoot: root})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	rs := s.Ruleset()
	wantDeny := len(builtinDenyRules()) + 2
	if len(rs.Deny) != wantDeny {
		t.Errorf("deny count = %d, want %d", len(rs.Deny), wantDeny)
	}
	if len(rs.Allow) != 2 {
		t.Errorf("allow count = %d, want 2", len(rs.Allow))
	}
	if len(rs.Restrict) != 2 {
		t.Errorf("restrict count = %d, want 2", len(rs.Restrict))
	}
}

type attributionTest struct {
	Name        string
	Global      string
	Project     string
	Argv        []string
	WantMessage string
	WantPrefix  []string
}

var attributionTests = []attributionTest{
	{
		Name:        "global deny over builtin",
		Global:      "version: 1\ndeny:\n  - prefix: [bash, -c]\n    message: global\n",
		Project:     "version: 1\n",
		Argv:        []string{"bash", "-c", "true"},
		WantMessage: "global",
		WantPrefix:  []string{"bash", "-c"},
	},
	{
		Name:        "project deny over global and builtin",
		Global:      "version: 1\ndeny:\n  - prefix: [bash]\n    message: global\n",
		Project:     "version: 1\ndeny:\n  - prefix: [bash, -c]\n    message: project\n",
		Argv:        []string{"bash", "-c", "true"},
		WantMessage: "project",
		WantPrefix:  []string{"bash", "-c"},
	},
	{
		Name:        "project restrict over global",
		Global:      "version: 1\nrestrict:\n  - prefix: [git]\n    message: global\n",
		Project:     "version: 1\nrestrict:\n  - prefix: [git, push]\n    message: project\n",
		Argv:        []string{"git", "push"},
		WantMessage: "project",
		WantPrefix:  []string{"git", "push"},
	},
	{
		Name:        "global deny over builtin at equal specificity",
		Global:      "version: 1\ndeny:\n  - prefix: [bash]\n    message: global\n",
		Project:     "version: 1\n",
		Argv:        []string{"bash", "x"},
		WantMessage: "global",
		WantPrefix:  []string{"bash"},
	},
	{
		Name:       "project allow over global",
		Global:     "version: 1\nallow:\n  - prefix: [make]\n",
		Project:    "version: 1\nallow:\n  - prefix: [make, test]\n",
		Argv:       []string{"make", "test"},
		WantPrefix: []string{"make", "test"},
	},
}

// TestLoadAttributesProjectThenGlobalThenBuiltin covers which rule a verdict names when rules from
// several layers match one command. The decision does not depend on the order, but the attributed
// rule's message is what the agent sees, so a builtin or global rule must not shadow a more
// specific layer's explanation.
func TestLoadAttributesProjectThenGlobalThenBuiltin(t *testing.T) {
	t.Parallel()

	for _, test := range attributionTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()

			global := writeGlobal(t, test.Global)
			root := writeProject(t, test.Project)

			s, err := Load(LoadOptions{GlobalPath: global, ProjectRoot: root})
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}

			got := s.Ruleset().Match(identitySubject(test.Argv))
			if got.Rule == nil {
				t.Fatalf("Match(%v) rule = nil, want prefix %v", test.Argv, test.WantPrefix)
			}
			if !slices.Equal(got.Rule.Prefix, test.WantPrefix) || got.Rule.Message != test.WantMessage {
				t.Errorf("Match(%v) rule = prefix %v message %q, want prefix %v message %q",
					test.Argv, got.Rule.Prefix, got.Rule.Message, test.WantPrefix, test.WantMessage)
			}
		})
	}
}

// TestLoadRestrictCrossLayer covers the settled cross-layer precedence: a project
// allow carves out of a global restrict because allow (tier 3) beats restrict
// (tier 4) regardless of which layer each rule came from.
func TestLoadRestrictCrossLayer(t *testing.T) {
	t.Parallel()

	global := writeGlobal(t, "version: 1\nrestrict:\n  - prefix: [git]\n")
	root := writeProject(t, "version: 1\nallow:\n  - prefix: [git, show]\n")

	s, err := Load(LoadOptions{GlobalPath: global, ProjectRoot: root})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	rs := s.Ruleset()
	if got := rs.Match(identitySubject([]string{"git", "show", "HEAD"})); got.Decision != DecisionRun {
		t.Errorf("git show decision = %v, want run (project allow carves out of global restrict)", got.Decision)
	}
	if got := rs.Match(identitySubject([]string{"git", "push"})); got.Decision != DecisionRefuse || !got.Restricted {
		t.Errorf("git push = {%v, restricted=%v}, want refuse via restrict", got.Decision, got.Restricted)
	}
}

// TestLoadEnvPermitCrossLayer covers a project allow that permits an override
// shadowed by an earlier global allow that matches the same command without one.
func TestLoadEnvPermitCrossLayer(t *testing.T) {
	t.Parallel()

	global := writeGlobal(t, "version: 1\nallow:\n  - prefix: [make]\n")
	root := writeProject(t, "version: 1\nallow:\n  - exact: [make, test]\n    permit_unsafe_envs: [PATH]\n")

	s, err := Load(LoadOptions{GlobalPath: global, ProjectRoot: root})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	got := s.Ruleset().Evaluate(identitySubject([]string{"make", "test"}), map[string]string{"PATH": "/custom"}, nil)
	if got.Decision != DecisionRun {
		t.Errorf("decision = %v (bad envs %v), want run via the project permit", got.Decision, got.BadEnvs)
	}
}

// TestLoadRecordsEachRuleLayer covers the layer a rule carries after the layers are concatenated.
// The ranking breaks equal rules by layer, so a builtin deny must rank as builtin and not as the
// project or global rule it sits beside in the list.
func TestLoadRecordsEachRuleLayer(t *testing.T) {
	t.Parallel()

	global := writeGlobal(t, "version: 1\nallow:\n  - prefix: [g]\ndeny:\n  - prefix: [gd]\nrestrict:\n  - prefix: [gr]\n")
	root := writeProject(t, "version: 1\nallow:\n  - prefix: [p]\ndeny:\n  - prefix: [pd]\nrestrict:\n  - prefix: [pr]\n")

	s, err := Load(LoadOptions{GlobalPath: global, ProjectRoot: root})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	rs := s.Ruleset()
	want := map[string]ruleLayer{"g": layerGlobal, "gd": layerGlobal, "gr": layerGlobal, "p": layerProject, "pd": layerProject, "pr": layerProject, "sh": layerBuiltin}

	seen := make(map[string]bool)
	for _, tier := range [][]Rule{rs.Allow, rs.Deny, rs.Restrict} {
		for _, r := range tier {
			layer, ok := want[r.Prefix[0]]
			if !ok {
				continue
			}

			seen[r.Prefix[0]] = true
			if r.layer != layer {
				t.Errorf("rule %s layer = %d, want %d", describeRule(&r), r.layer, layer)
			}
		}
	}

	for name := range want {
		if !seen[name] {
			t.Errorf("no rule for %q found in the ruleset", name)
		}
	}
}

// TestLoadGlobalRuleMoreSpecificThanProjectDecides covers the ranking across layers: specificity
// comes before layer, so a global exact rule decides over a project prefix rule that permits an
// override, and a project rule decides over an otherwise equal global one.
func TestLoadGlobalRuleMoreSpecificThanProjectDecides(t *testing.T) {
	t.Parallel()

	env := map[string]string{"PATH": "/custom"}
	subj := identitySubject([]string{"make", "test"})

	t.Run("more specific global rule", func(t *testing.T) {
		t.Parallel()

		global := writeGlobal(t, "version: 1\nallow:\n  - exact: [make, test]\n")
		root := writeProject(t, "version: 1\nallow:\n  - prefix: [make]\n    permit_unsafe_envs: [PATH]\n")

		s, err := Load(LoadOptions{GlobalPath: global, ProjectRoot: root})
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		got := s.Ruleset().Evaluate(subj, env, nil)
		if got.Decision != DecisionRefuse || got.Rule == nil || got.Rule.kind != KindExact {
			t.Errorf("verdict = %v rule %v, want a refusal naming the global exact rule", got.Decision, got.Rule)
		}
	})

	t.Run("equal rules break to the project", func(t *testing.T) {
		t.Parallel()

		global := writeGlobal(t, "version: 1\nallow:\n  - prefix: [make, test]\n")
		root := writeProject(t, "version: 1\nallow:\n  - prefix: [make, test]\n    permit_unsafe_envs: [PATH]\n")

		s, err := Load(LoadOptions{GlobalPath: global, ProjectRoot: root})
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		got := s.Ruleset().Evaluate(subj, env, nil)
		if got.Decision != DecisionRun || len(got.Deciding) != 1 || got.Deciding[0].layer != layerProject {
			t.Errorf("verdict = %v deciding %v, want a run decided by the project rule alone", got.Decision, got.Deciding)
		}
	})
}

func TestLoadCapturesSnapshot(t *testing.T) {
	t.Parallel()

	content := "version: 1\nallow:\n  - prefix: [make]\n"
	global := writeGlobal(t, content)
	emptyRoot := t.TempDir()

	s, err := Load(LoadOptions{GlobalPath: global, ProjectRoot: emptyRoot})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !bytes.Equal(s.Global.Snapshot, []byte(content)) {
		t.Errorf("global snapshot = %q, want %q", s.Global.Snapshot, content)
	}
	if s.Project.Snapshot != nil {
		t.Errorf("absent project snapshot = %q, want nil", s.Project.Snapshot)
	}
}

func TestLoadInvalidRuleFailsLoad(t *testing.T) {
	t.Parallel()

	global := writeGlobal(t, "version: 1\nallow:\n  - message: not allowed on allow\n")
	emptyRoot := t.TempDir()

	if _, err := Load(LoadOptions{GlobalPath: global, ProjectRoot: emptyRoot}); err == nil {
		t.Fatalf("Load() expected an error for an invalid rule, got nil")
	}
}

func TestDiagnoseBothClean(t *testing.T) {
	t.Parallel()

	global := writeGlobal(t, "version: 1\nallow:\n  - prefix: [git]\n")
	root := writeProject(t, "version: 1\nallow:\n  - prefix: [make]\n")

	g, p, err := Diagnose(LoadOptions{GlobalPath: global, ProjectRoot: root})
	if err != nil {
		t.Fatalf("Diagnose() error: %v", err)
	}
	if !g.Present || g.Err != nil {
		t.Errorf("global = %+v, want present with no error", g)
	}
	if !p.Present || p.Err != nil {
		t.Errorf("project = %+v, want present with no error", p)
	}
}

func TestDiagnoseBothMissing(t *testing.T) {
	t.Parallel()

	missingGlobal := filepath.Join(t.TempDir(), "nope.yaml")
	emptyRoot := t.TempDir()

	g, p, err := Diagnose(LoadOptions{GlobalPath: missingGlobal, ProjectRoot: emptyRoot})
	if err != nil {
		t.Fatalf("Diagnose() error: %v", err)
	}
	if g.Present || g.Err != nil {
		t.Errorf("global = %+v, want absent with no error", g)
	}
	if p.Present || p.Err != nil {
		t.Errorf("project = %+v, want absent with no error", p)
	}
}

// TestDiagnoseBothBrokenReportsBoth is the reason Diagnose exists rather than
// reusing Load: a broken global layer must not prevent the project layer's
// problems from being reported in the same run.
func TestDiagnoseBothBrokenReportsBoth(t *testing.T) {
	t.Parallel()

	global := writeGlobal(t, "version: 1\ndeny:\n  - message: no kind here\n")
	root := writeProject(t, "version: 1\nallow:\n  - prefix: [make]\n    message: not allowed here\n")

	g, p, err := Diagnose(LoadOptions{GlobalPath: global, ProjectRoot: root})
	if err != nil {
		t.Fatalf("Diagnose() error: %v", err)
	}
	if g.Err == nil || !errors.Is(g.Err, ErrNoRuleKind) {
		t.Errorf("global.Err = %v, want it to match ErrNoRuleKind", g.Err)
	}
	if p.Err == nil || !errors.Is(p.Err, ErrMessageOnAllow) {
		t.Errorf("project.Err = %v, want it to match ErrMessageOnAllow", p.Err)
	}
}

func TestDiagnoseOnlyProjectBroken(t *testing.T) {
	t.Parallel()

	global := writeGlobal(t, "version: 1\nallow:\n  - prefix: [git]\n")
	root := writeProject(t, "version: 1\nallow:\n  - exact: [git]\n    prefix: [git]\n")

	g, p, err := Diagnose(LoadOptions{GlobalPath: global, ProjectRoot: root})
	if err != nil {
		t.Fatalf("Diagnose() error: %v", err)
	}
	if g.Err != nil {
		t.Errorf("global.Err = %v, want nil", g.Err)
	}
	if p.Err == nil || !errors.Is(p.Err, ErrMultipleRuleKinds) {
		t.Errorf("project.Err = %v, want it to match ErrMultipleRuleKinds", p.Err)
	}
}

func TestDefaultProjectRoot(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "/some/project")
	got, err := DefaultProjectRoot()
	if err != nil {
		t.Fatalf("DefaultProjectRoot() error: %v", err)
	}
	if got != "/some/project" {
		t.Errorf("DefaultProjectRoot() = %q, want /some/project", got)
	}

	t.Setenv("CLAUDE_PROJECT_DIR", "")
	cwd, _ := os.Getwd()
	got, err = DefaultProjectRoot()
	if err != nil {
		t.Fatalf("DefaultProjectRoot() error: %v", err)
	}
	if got != cwd {
		t.Errorf("DefaultProjectRoot() = %q, want cwd %q", got, cwd)
	}
}
