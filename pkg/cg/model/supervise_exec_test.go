package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plantScript writes an executable shebang script that prints the path it was exec'd
// through. The kernel hands a script its exec path as $0, which is how a program such
// as a Python venv interpreter learns where it lives.
func plantScript(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' \"$0\"\n"), 0o755); err != nil {
		t.Fatalf("planting %s: %v", path, err)
	}

	return path
}

// TestSuperviseExecsThroughTheResolvedPath covers a command reached through a symlink
// that must keep its own path, as a venv interpreter linked to its base interpreter
// does. Execing the symlink-evaluated path hands the child the base install's path,
// and the venv is lost. It cannot show that the canonical file is the one that runs.
// TestSuperviseRefusesARetargetedSymlink covers that.
func TestSuperviseExecsThroughTheResolvedPath(t *testing.T) {
	cap := newSuperviseDir(t)
	dir := t.TempDir()

	plantScript(t, dir, "base")
	link := filepath.Join(dir, "venv-python")
	if err := os.Symlink("base", link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	spec := superviseSpec(t, []string{link}, "", nil)
	if ack, _, err := runSupervise(t, cap.Dir, spec); err != nil || !ack.Started {
		t.Fatalf("superviseRun: ack = %+v, err = %v", ack, err)
	}

	stdout, err := os.ReadFile(filepath.Join(cap.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if got := strings.TrimSpace(string(stdout)); got != link {
		t.Errorf("child exec path = %q, want the symlink %q", got, link)
	}
}

// TestSuperviseRefusesARetargetedSymlink covers a symlink retargeted after the gate
// approved the file it pointed at. The supervisor must not exec the new target. It
// cannot show that the approved file itself was left unchanged.
func TestSuperviseRefusesARetargetedSymlink(t *testing.T) {
	cap := newSuperviseDir(t)
	dir := t.TempDir()

	plantScript(t, dir, "approved")
	plantScript(t, dir, "swapped")
	link := filepath.Join(dir, "tool")
	if err := os.Symlink("approved", link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	spec := superviseSpec(t, []string{link}, "", nil)

	if err := os.Remove(link); err != nil {
		t.Fatalf("removing symlink: %v", err)
	}
	if err := os.Symlink("swapped", link); err != nil {
		t.Fatalf("retargeting symlink: %v", err)
	}

	ack, _, err := runSupervise(t, cap.Dir, spec)
	if err == nil || ack.Started || !strings.Contains(ack.StartError, "changed since it was approved") {
		t.Errorf("ack = %+v, err = %v, want a start error naming the changed executable", ack, err)
	}

	stdout, readErr := os.ReadFile(filepath.Join(cap.Dir, "stdout"))
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	if len(stdout) != 0 {
		t.Errorf("stdout = %q, want nothing, since the retargeted file must not run", stdout)
	}
}
