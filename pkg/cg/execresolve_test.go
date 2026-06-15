package cg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plantExec writes an executable file named name under dir and returns its path.
func plantExec(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("planting %s: %v", path, err)
	}
	return path
}

// evalSymlinks resolves any symlinks in path so comparisons survive macOS, where
// the temp root (/var/folders/...) is itself a symlink into /private.
func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", path, err)
	}
	return got
}

func TestResolveCommandBarePath(t *testing.T) {
	dir := t.TempDir()
	planted := plantExec(t, dir, "tool")
	t.Setenv("PATH", dir)

	r, err := ResolveCommand([]string{"tool", "--flag"}, "")
	if err != nil {
		t.Fatalf("ResolveCommand: %v", err)
	}
	if r.Resolved == "" || filepath.Base(r.Resolved) != "tool" {
		t.Errorf("Resolved = %q, want an absolute path ending in tool", r.Resolved)
	}
	if want := evalSymlinks(t, planted); r.Canonical != want {
		t.Errorf("Canonical = %q, want %q", r.Canonical, want)
	}
	if got := r.CanonicalArgv(); len(got) != 2 || got[1] != "--flag" {
		t.Errorf("CanonicalArgv tail = %v, want [.. --flag]", got)
	}
}

func TestResolveCommandBareNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	r, err := ResolveCommand([]string{"definitely-not-on-path-zzz"}, "")
	if err == nil {
		t.Fatalf("expected error for unresolvable command")
	}
	if r.Resolved != "" || r.Canonical != "" {
		t.Errorf("Resolved=%q Canonical=%q, want both empty", r.Resolved, r.Canonical)
	}
	if len(r.Argv) != 1 {
		t.Errorf("Argv = %v, want the original command preserved", r.Argv)
	}
}

func TestResolveCommandAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	planted := plantExec(t, dir, "tool")

	r, err := ResolveCommand([]string{planted, "x"}, "")
	if err != nil {
		t.Fatalf("ResolveCommand: %v", err)
	}
	if r.Resolved != filepath.Clean(planted) {
		t.Errorf("Resolved = %q, want %q", r.Resolved, filepath.Clean(planted))
	}
	if want := evalSymlinks(t, planted); r.Canonical != want {
		t.Errorf("Canonical = %q, want %q", r.Canonical, want)
	}
}

func TestResolveCommandRelativeUsesCwd(t *testing.T) {
	dir := t.TempDir()
	planted := plantExec(t, dir, "tool")

	r, err := ResolveCommand([]string{"./tool"}, dir)
	if err != nil {
		t.Fatalf("ResolveCommand: %v", err)
	}
	if r.Resolved != filepath.Join(dir, "tool") {
		t.Errorf("Resolved = %q, want %q", r.Resolved, filepath.Join(dir, "tool"))
	}
	if want := evalSymlinks(t, planted); r.Canonical != want {
		t.Errorf("Canonical = %q, want %q", r.Canonical, want)
	}
}

func TestResolveExecPathRelativeUsesServerCwd(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	got, err := resolveExecPath("./tool", "")
	if err != nil {
		t.Fatalf("resolveExecPath: %v", err)
	}
	if want := filepath.Join(wd, "tool"); got != want {
		t.Errorf("resolveExecPath(./tool, \"\") = %q, want %q", got, want)
	}
}

func TestResolveCommandSymlinkCanonicalizes(t *testing.T) {
	realDir := t.TempDir()
	linkDir := t.TempDir()
	real := plantExec(t, realDir, "tool")
	link := filepath.Join(linkDir, "tool")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	r, err := ResolveCommand([]string{link}, "")
	if err != nil {
		t.Fatalf("ResolveCommand: %v", err)
	}
	if r.Resolved != link {
		t.Errorf("Resolved = %q, want the invoked link %q", r.Resolved, link)
	}
	if want := evalSymlinks(t, real); r.Canonical != want {
		t.Errorf("Canonical = %q, want the symlink target %q", r.Canonical, want)
	}
}

func TestResolveCommandCanonicalizeFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope", "tool")

	r, err := ResolveCommand([]string{missing}, "")
	if err == nil {
		t.Fatalf("expected canonicalization error for nonexistent path")
	}
	if r.Resolved != filepath.Clean(missing) {
		t.Errorf("Resolved = %q, want %q", r.Resolved, filepath.Clean(missing))
	}
	if r.Canonical != "" {
		t.Errorf("Canonical = %q, want empty on canonicalization failure", r.Canonical)
	}
}

func TestResolveCommandEmptyArgv(t *testing.T) {
	if _, err := ResolveCommand(nil, ""); err == nil {
		t.Fatalf("expected error for empty argv")
	}
}

// TestResolveCommandUsesServerPathOnly plants the same program name in two
// directories and confirms resolution follows the server PATH. ResolveCommand
// takes no env argument, so a caller-supplied env.PATH cannot redirect the
// top-level executable that gets approved and execed.
func TestResolveCommandUsesServerPathOnly(t *testing.T) {
	serverDir := t.TempDir()
	otherDir := t.TempDir()
	serverTool := plantExec(t, serverDir, "tool")
	plantExec(t, otherDir, "tool")
	t.Setenv("PATH", serverDir)

	r, err := ResolveCommand([]string{"tool"}, "")
	if err != nil {
		t.Fatalf("ResolveCommand: %v", err)
	}
	if want := evalSymlinks(t, serverTool); r.Canonical != want {
		t.Errorf("Canonical = %q, want the server-PATH copy %q", r.Canonical, want)
	}
	if strings.HasPrefix(r.Canonical, evalSymlinks(t, otherDir)) {
		t.Errorf("Canonical = %q resolved into the off-PATH dir", r.Canonical)
	}
}

type canonicalArgvTest struct {
	name string
	res  Resolution
	want []string
}

var canonicalArgvTests = []canonicalArgvTest{
	{name: "canonical with tail", res: Resolution{Argv: []string{"foo", "-x"}, Canonical: "/opt/foo"}, want: []string{"/opt/foo", "-x"}},
	{name: "canonical only", res: Resolution{Argv: []string{"foo"}, Canonical: "/opt/foo"}, want: []string{"/opt/foo"}},
	{name: "no canonical is nil", res: Resolution{Argv: []string{"foo"}}, want: nil},
	{name: "no argv is nil", res: Resolution{Canonical: "/opt/foo"}, want: nil},
}

func TestCanonicalArgv(t *testing.T) {
	t.Parallel()

	for _, tt := range canonicalArgvTests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.res.CanonicalArgv()
			if len(got) != len(tt.want) {
				t.Fatalf("CanonicalArgv() = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("CanonicalArgv() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

type execPathTest struct {
	name string
	res  Resolution
	want string
}

var execPathTests = []execPathTest{
	{name: "prefers canonical", res: Resolution{Argv: []string{"foo"}, Resolved: "/r/foo", Canonical: "/c/foo"}, want: "/c/foo"},
	{name: "falls back to resolved", res: Resolution{Argv: []string{"foo"}, Resolved: "/r/foo"}, want: "/r/foo"},
	{name: "falls back to argv0", res: Resolution{Argv: []string{"foo"}}, want: "foo"},
	{name: "empty resolution", res: Resolution{}, want: ""},
}

func TestExecPath(t *testing.T) {
	t.Parallel()

	for _, tt := range execPathTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.res.ExecPath(); got != tt.want {
				t.Errorf("ExecPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
