//go:build darwin

package model

import "golang.org/x/sys/unix"

func readProcess(pid int) (processInfo, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return processInfo{}, err
	}

	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return processInfo{}, err
	}

	argv, err := parseProcArgs2(raw)
	if err != nil {
		return processInfo{}, err
	}
	return processInfo{Ppid: int(kp.Eproc.Ppid), Argv: argv}, nil
}
