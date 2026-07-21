//go:build !unix

package model

import "os/exec"

// collectUsage falls back to the portable CPU times exposed by ProcessState on
// platforms without a rusage struct. Memory and counter fields stay zero.
func collectUsage(cmd *exec.Cmd) Usage {
	u := Usage{Source: UsageSourceProcessState}
	if cmd.ProcessState != nil {
		u.UserUS = cmd.ProcessState.UserTime().Microseconds()
		u.SystemUS = cmd.ProcessState.SystemTime().Microseconds()
	}
	return u
}
