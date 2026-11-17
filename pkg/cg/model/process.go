package model

import (
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
)

// ErrProcessGone reports that a pid names no process.
var ErrProcessGone = errors.New("process not found")

// ErrPidNotTied reports that a pid read from a run directory cannot be tied to the run.
var ErrPidNotTied = errors.New("pid cannot be tied to the run")

// errProcessLookupUnsupported reports a platform where cg cannot read another process's parent or
// argv. Cancelling a run it did not start is refused there.
var errProcessLookupUnsupported = errors.New("process lookup is not supported on this platform")

// processInfo is what cg needs to know about another process to decide whether it owns it.
type processInfo struct {
	Ppid int
	Argv []string
}

// lookupProcess reads pid's parent and argv. A pid that names no process surfaces as
// ErrProcessGone.
func lookupProcess(pid int) (processInfo, error) {
	if pid <= 0 {
		return processInfo{}, fmt.Errorf("invalid pid %d", pid)
	}

	info, err := readProcess(pid)
	if err == nil {
		return info, nil
	}
	if errors.Is(err, errProcessLookupUnsupported) {
		return processInfo{}, err
	}
	if kerr := syscall.Kill(pid, 0); errors.Is(kerr, syscall.ESRCH) {
		return processInfo{}, ErrProcessGone
	}
	return processInfo{}, fmt.Errorf("reading process %d: %w", pid, err)
}

// tieRunPid confirms pid is the child of the `cg supervise-run` process for the run in dir. The pid
// file lives in a directory a sandboxed command may write, so it names a candidate and nothing more.
// A supervisor's child is the only process whose parent carries the run's directory in its argv.
func tieRunPid(dir string, pid int) error {
	info, err := lookupProcess(pid)
	if err != nil {
		return err
	}

	parent, err := lookupProcess(info.Ppid)
	if errors.Is(err, ErrProcessGone) {
		return fmt.Errorf("%w: pid %d has no supervisor", ErrPidNotTied, pid)
	}
	if err != nil {
		return err
	}

	if !isSupervisorArgv(parent.Argv, "supervise-run", dir) {
		return fmt.Errorf("%w: the parent of pid %d is not `cg supervise-run %s`", ErrPidNotTied, pid, dir)
	}
	return nil
}

// tiePoolPid confirms pid is the `cg supervise-pool` process for the pool in dir. The pool pid file
// names the supervisor itself, whose own parent may be long gone.
func tiePoolPid(dir string, pid int) error {
	info, err := lookupProcess(pid)
	if err != nil {
		return err
	}

	if !isSupervisorArgv(info.Argv, "supervise-pool", dir) {
		return fmt.Errorf("%w: pid %d is not `cg supervise-pool %s`", ErrPidNotTied, pid, dir)
	}
	return nil
}

// isSupervisorArgv reports whether argv is `<exe> [<mount>...] <sub> <dir>`, comparing dir after
// symlink resolution because the server and the cancelling shell may see the temp root through
// different paths. argv[0] is not compared: the multi-call binary runs under several names. The
// mount words are not compared either, since they depend on where the binary mounts cg.
func isSupervisorArgv(argv []string, sub, dir string) bool {
	if len(argv) < 3 || argv[len(argv)-2] != sub {
		return false
	}
	return sameDir(argv[len(argv)-1], dir)
}

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}

	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	return ra == rb
}
