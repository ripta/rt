//go:build !linux

package model

import "os/exec"

// prepareCgroup is a no-op on platforms without cgroup v2. The caller falls
// back to the wait4 baseline via resolveUsage.
func prepareCgroup(*exec.Cmd) cgroupCollector { return nil }
