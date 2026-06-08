package approve

import (
	"bytes"
	"os"
	"path/filepath"
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

	global := writeGlobal(t, "version: 1\ndeny:\n  - prefix: [git, push, --force]\nallow:\n  - prefix: [go, test]\n")
	root := writeProject(t, "version: 1\ndeny:\n  - prefix: [terraform, destroy]\nallow:\n  - prefix: [make]\n")

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
