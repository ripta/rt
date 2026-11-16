package model

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"syscall"
	"testing"
)

func TestLookupProcessSelf(t *testing.T) {
	info, err := lookupProcess(os.Getpid())
	if errors.Is(err, errProcessLookupUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatalf("lookupProcess: %v", err)
	}

	if info.Ppid != os.Getppid() {
		t.Errorf("Ppid = %d, want %d", info.Ppid, os.Getppid())
	}
	if !slices.Equal(info.Argv, os.Args) {
		t.Errorf("Argv = %q, want %q", info.Argv, os.Args)
	}
}

func TestLookupProcessGone(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("running true: %v", err)
	}

	_, err := lookupProcess(cmd.Process.Pid)
	if errors.Is(err, errProcessLookupUnsupported) {
		t.Skip(err)
	}
	if !errors.Is(err, ErrProcessGone) {
		t.Errorf("lookupProcess of a reaped pid: err = %v, want ErrProcessGone", err)
	}
}

func TestLookupProcessChildArgv(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	info, err := lookupProcess(cmd.Process.Pid)
	if errors.Is(err, errProcessLookupUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatalf("lookupProcess: %v", err)
	}
	if info.Ppid != os.Getpid() {
		t.Errorf("Ppid = %d, want %d", info.Ppid, os.Getpid())
	}
	if want := []string{"sleep", "30"}; !slices.Equal(info.Argv, want) {
		t.Errorf("Argv = %q, want %q", info.Argv, want)
	}
}

type supervisorArgvTest struct {
	Name string
	Argv []string
	Sub  string
	Want bool
}

var supervisorArgvTests = []supervisorArgvTest{
	{Name: "run", Argv: []string{"/bin/cg", "supervise-run", "/tmp/x"}, Sub: "supervise-run", Want: true},
	{Name: "multi-call name", Argv: []string{"rt", "supervise-run", "/tmp/x"}, Sub: "supervise-run", Want: true},
	{Name: "other dir", Argv: []string{"cg", "supervise-run", "/tmp/y"}, Sub: "supervise-run", Want: false},
	{Name: "other subcommand", Argv: []string{"cg", "supervise-pool", "/tmp/x"}, Sub: "supervise-run", Want: false},
	{Name: "extra argument", Argv: []string{"cg", "supervise-run", "/tmp/x", "/tmp/y"}, Sub: "supervise-run", Want: false},
	{Name: "short", Argv: []string{"cg", "supervise-run"}, Sub: "supervise-run", Want: false},
	{Name: "empty", Argv: nil, Sub: "supervise-run", Want: false},
}

func TestIsSupervisorArgv(t *testing.T) {
	for _, tt := range supervisorArgvTests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := isSupervisorArgv(tt.Argv, tt.Sub, "/tmp/x"); got != tt.Want {
				t.Errorf("isSupervisorArgv(%q, %q) = %v, want %v", tt.Argv, tt.Sub, got, tt.Want)
			}
		})
	}
}

func TestSameDirThroughSymlink(t *testing.T) {
	real := t.TempDir()
	link := t.TempDir() + "/link"
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if !sameDir(link, real) {
		t.Error("sameDir(link, real) = false, want true")
	}
	if sameDir(link, t.TempDir()) {
		t.Error("sameDir of unrelated dirs = true, want false")
	}
}
