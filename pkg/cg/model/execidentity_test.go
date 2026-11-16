package model

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// copyTestBinary copies the running test binary into a temp dir and returns the copy's path. The
// copy answers `supervise-run` and `supervise-pool` through TestMain, so it can stand in for the cg
// executable, and a test can change it without touching the real binary.
func copyTestBinary(t *testing.T, name string) string {
	t.Helper()

	src, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatalf("opening test binary: %v", err)
	}
	defer src.Close()

	path := filepath.Join(t.TempDir(), name)
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatalf("copying test binary: %v", err)
	}
	if err := dst.Close(); err != nil {
		t.Fatalf("closing %s: %v", path, err)
	}
	return path
}

// replaceFile swaps path for a file with different content by rename, the way an installer or an
// attacker with write access to the directory would.
func replaceFile(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	tmp := path + ".new"
	if err := os.WriteFile(tmp, append(data, []byte("changed")...), 0o755); err != nil {
		t.Fatalf("writing %s: %v", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("renaming over %s: %v", path, err)
	}
}

func newTestIdentity(t *testing.T, path string) *ExecIdentity {
	t.Helper()

	id, err := NewExecIdentity(path)
	if err != nil {
		t.Fatalf("NewExecIdentity: %v", err)
	}
	return id
}

func wantChanged(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, ErrExecutableChanged) {
		t.Fatalf("err = %v, want ErrExecutableChanged", err)
	}
	if !strings.Contains(err.Error(), "restart cg") {
		t.Errorf("err = %q, want it to tell the user to restart cg", err)
	}
}

func TestExecIdentityVerifyUnchanged(t *testing.T) {
	id := newTestIdentity(t, copyTestBinary(t, "cg"))
	if err := id.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestExecIdentityVerifyContentChanged(t *testing.T) {
	path := copyTestBinary(t, "cg")
	id := newTestIdentity(t, path)

	replaceFile(t, path)
	wantChanged(t, id.Verify())
}

func TestExecIdentityVerifySymlinkRetargeted(t *testing.T) {
	dir := t.TempDir()
	a := copyTestBinary(t, "cg-a")
	b := copyTestBinary(t, "cg-b")

	link := filepath.Join(dir, "cg")
	if err := os.Symlink(a, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	id := newTestIdentity(t, link)

	// The two copies hold the same bytes, so only the changed target can trip the check.
	if err := os.Remove(link); err != nil {
		t.Fatalf("removing link: %v", err)
	}
	if err := os.Symlink(b, link); err != nil {
		t.Fatalf("retargeting link: %v", err)
	}
	wantChanged(t, id.Verify())
}

func TestExecIdentityVerifyRemoved(t *testing.T) {
	path := copyTestBinary(t, "cg")
	id := newTestIdentity(t, path)

	if err := os.Remove(path); err != nil {
		t.Fatalf("removing: %v", err)
	}
	wantChanged(t, id.Verify())
}

func TestRunSupervisedUnchangedSupervisorRuns(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	id := newTestIdentity(t, copyTestBinary(t, "cg"))

	run, err := RunSupervised([]string{"echo", "hello"}, SuperviseOptions{Supervisor: id})
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
	}
	waitDone(t, run, 5*time.Second)

	out, err := os.ReadFile(filepath.Join(run.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("stdout = %q, want %q", out, "hello\n")
	}
}

func TestRunSupervisedRefusesChangedSupervisor(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	path := copyTestBinary(t, "cg")
	id := newTestIdentity(t, path)
	replaceFile(t, path)

	marker := filepath.Join(t.TempDir(), "ran")
	_, err := RunSupervised([]string{"touch", marker}, SuperviseOptions{Supervisor: id})

	var sf *StartFailure
	if !errors.As(err, &sf) {
		t.Fatalf("err = %v, want *StartFailure", err)
	}
	wantChanged(t, err)

	if _, err := os.Stat(marker); err == nil {
		t.Error("the command ran under a supervisor from a changed binary")
	}
	if _, err := ReadStartDebug(sf.Dir); err != nil {
		t.Errorf("no start debug record kept for the refused run: %v", err)
	}
}

func TestPoolSupervisedRefusesChangedSupervisor(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	path := copyTestBinary(t, "cg")
	spec := poolSpec(t, []string{"echo", "one"})
	spec.Supervisor = newTestIdentity(t, path)
	replaceFile(t, path)

	_, err := PoolSupervised(spec)

	var sf *StartFailure
	if !errors.As(err, &sf) {
		t.Fatalf("err = %v, want *StartFailure", err)
	}
	wantChanged(t, err)
}

// TestSupervisePoolRefusesChangedSupervisor pins that the pool supervisor checks the identity the
// server recorded before each member, not only before itself. The binary is already swapped when
// the first member would spawn.
func TestSupervisePoolRefusesChangedSupervisor(t *testing.T) {
	_, dir := newPoolTestDir(t)

	path := copyTestBinary(t, "cg")
	spec := poolSpec(t, []string{"echo", "one"})
	spec.Supervisor = newTestIdentity(t, path)
	replaceFile(t, path)

	if _, err := runPool(t, dir, spec); err != nil {
		t.Fatalf("supervisePool: %v", err)
	}

	m := readManifest(t, dir)
	rec := m.Runs[0]
	if rec.Status != PoolRunStartError || !strings.Contains(rec.StartError, "restart cg") {
		t.Errorf("member = %+v, want a start error telling the user to restart cg", rec)
	}
}

// TestSupervisePoolRefusesRepeatAfterSwap pins the repeat case: the first run spawns from the
// unchanged binary, its command then replaces the binary, and the next repeat is refused.
func TestSupervisePoolRefusesRepeatAfterSwap(t *testing.T) {
	_, dir := newPoolTestDir(t)

	path := copyTestBinary(t, "cg")
	other := copyTestBinary(t, "other")
	replaceFile(t, other)

	spec := poolSpec(t, []string{"mv", other, path})
	spec.Supervisor = newTestIdentity(t, path)
	spec.Repeat = 2

	if _, err := runPool(t, dir, spec); err != nil {
		t.Fatalf("supervisePool: %v", err)
	}

	m := readManifest(t, dir)
	if len(m.Runs) != 2 {
		t.Fatalf("len(runs) = %d, want 2", len(m.Runs))
	}
	if m.Runs[0].Status != PoolRunFinished {
		t.Errorf("first repeat = %+v, want finished", m.Runs[0])
	}
	if rec := m.Runs[1]; rec.Status != PoolRunStartError || !strings.Contains(rec.StartError, "restart cg") {
		t.Errorf("second repeat = %+v, want a start error telling the user to restart cg", rec)
	}
}

func TestPoolSpecSupervisorAbsentRunsAsBefore(t *testing.T) {
	_, dir := newPoolTestDir(t)

	spec := poolSpec(t, []string{"echo", "one"})
	if spec.Supervisor != nil {
		t.Fatal("poolSpec sets a supervisor")
	}

	if _, err := runPool(t, dir, spec); err != nil {
		t.Fatalf("supervisePool: %v", err)
	}
	if rec := readManifest(t, dir).Runs[0]; rec.Status != PoolRunFinished {
		t.Errorf("member = %+v, want finished", rec)
	}
}

// TestRunSupervisedThroughAMountedCommand covers a server started as `rt cg mcp`. The multi-call
// binary knows the supervisors only under its `cg` subcommand, so the spawn must carry that
// prefix. It cannot show that the real rt root resolves the prefixed argv.
func TestRunSupervisedThroughAMountedCommand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	id := newTestIdentity(t, copyTestBinary(t, "rt"))
	id.Prefix = []string{"cg"}

	run, err := RunSupervised([]string{"echo", "hello"}, SuperviseOptions{Supervisor: id})
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
	}
	waitDone(t, run, 5*time.Second)

	out, err := os.ReadFile(filepath.Join(run.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("stdout = %q, want %q", out, "hello\n")
	}
}

type commandPrefixTest struct {
	Name   string
	Path   []string
	Prefix []string
}

var commandPrefixTests = []commandPrefixTest{
	{Name: "standalone cg", Path: []string{"cg", "mcp"}, Prefix: nil},
	{Name: "mounted under rt", Path: []string{"rt", "cg", "mcp"}, Prefix: []string{"cg"}},
	{Name: "mounted deeper", Path: []string{"tool", "a", "cg", "mcp"}, Prefix: []string{"a", "cg"}},
}

func TestCommandPrefix(t *testing.T) {
	for _, tt := range commandPrefixTests {
		t.Run(tt.Name, func(t *testing.T) {
			var parent *cobra.Command
			for _, name := range tt.Path[:len(tt.Path)-1] {
				c := &cobra.Command{Use: name}
				if parent != nil {
					parent.AddCommand(c)
				}
				parent = c
			}
			leaf := &cobra.Command{Use: tt.Path[len(tt.Path)-1]}
			parent.AddCommand(leaf)

			if got := CommandPrefix(leaf); !slices.Equal(got, tt.Prefix) {
				t.Errorf("CommandPrefix = %q, want %q", got, tt.Prefix)
			}
		})
	}
}
