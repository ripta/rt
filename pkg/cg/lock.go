package cg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockFilename is the file within a run directory the supervisor holds an exclusive
// flock on for the run's lifetime. The kernel releases the lock on any supervisor
// death, so an acquirable lock with no meta.json marks an abandoned run.
const LockFilename = "lock"

// errRunLockHeld reports that another supervisor already owns the run directory.
var errRunLockHeld = errors.New("run lock held by another supervisor")

// RunLockReleased reports whether dir's run lock exists and is not held. A released
// lock with no meta.json marks an abandoned run: the supervisor died before writing
// the run's bookkeeping. A missing lock file returns false, so shell-path runs and
// pre-supervisor run dirs keep their existing behavior. The probe takes a shared
// lock so concurrent probes do not conflict with each other; it still conflicts with
// the supervisor's exclusive lock.
func RunLockReleased(dir string) bool {
	f, err := os.Open(filepath.Join(dir, LockFilename))
	if err != nil {
		return false
	}
	defer f.Close()

	return syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) == nil
}

// acquireRunLock creates dir/lock if needed and takes a non-blocking exclusive flock
// on it. The caller must keep the returned file open for as long as the lock must be
// held; closing it releases the lock. A held lock means another supervisor owns the
// run directory, which is fatal rather than something to wait out.
func acquireRunLock(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, LockFilename), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening run lock: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errRunLockHeld
		}
		return nil, fmt.Errorf("locking run dir: %w", err)
	}

	return f, nil
}
