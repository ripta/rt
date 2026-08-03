//go:build unix

package model

import (
	"os/exec"
	"syscall"
)

// collectUsage reads the reaped child's wait4 rusage that os/exec already
// captured into ProcessState. Reading the per-child rusage rather than
// getrusage(RUSAGE_CHILDREN) keeps the numbers correct in the long-lived MCP
// server, which reaps many children over its lifetime. For the single-child
// foreground run the two are identical.
func collectUsage(cmd *exec.Cmd) Usage {
	u := Usage{Source: UsageSourceRusageChildren}
	if cmd.ProcessState == nil {
		return u
	}

	ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok || ru == nil {
		u.UserUS = cmd.ProcessState.UserTime().Microseconds()
		u.SystemUS = cmd.ProcessState.SystemTime().Microseconds()
		return u
	}

	u.UserUS = timevalMicros(ru.Utime)
	u.SystemUS = timevalMicros(ru.Stime)
	u.MaxRSSBytes = maxrssBytes(int64(ru.Maxrss))
	u.MinorFaults = int64(ru.Minflt)
	u.MajorFaults = int64(ru.Majflt)
	u.VoluntaryCtxSwitches = int64(ru.Nvcsw)
	u.InvoluntaryCtxSwitches = int64(ru.Nivcsw)
	u.BlockInputOps = int64(ru.Inblock)
	u.BlockOutputOps = int64(ru.Oublock)
	return u
}

func timevalMicros(tv syscall.Timeval) int64 {
	return int64(tv.Sec)*1_000_000 + int64(tv.Usec)
}
