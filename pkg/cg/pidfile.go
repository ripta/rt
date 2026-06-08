package cg

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PidFilename is the file name within a run directory that holds the child's
// process-group ID while the run is in flight. The capture paths set Setpgid,
// so the pgid equals the child's pid. cg_cancel reads this to signal the group;
// it is removed once the run finishes so a completed run carries no stale pid.
const PidFilename = "pid"

// WritePidFile records pid in dir/pid as a decimal string. The payload is small
// enough that a single write suffices; no temp-and-rename is needed.
func WritePidFile(dir string, pid int) error {
	path := filepath.Join(dir, PidFilename)
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing pid file: %w", err)
	}
	return nil
}

// ReadPidFile reads and parses the pid recorded in dir/pid. A missing file
// surfaces as fs.ErrNotExist unwrapped, so callers can branch on it.
func ReadPidFile(dir string) (int, error) {
	data, err := os.ReadFile(filepath.Join(dir, PidFilename))
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parsing pid file: %w", err)
	}
	return pid, nil
}

// RemovePidFile deletes dir/pid, ignoring a missing file. It is best-effort:
// the caller is finishing a run and a leftover pid file is harmless because
// cg_cancel checks for meta.json first.
func RemovePidFile(dir string) {
	_ = os.Remove(filepath.Join(dir, PidFilename))
}
