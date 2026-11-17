//go:build linux

package model

import (
	"os"
	"strconv"
)

func readProcess(pid int) (processInfo, error) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return processInfo{}, err
	}

	ppid, err := parseProcStatPpid(stat)
	if err != nil {
		return processInfo{}, err
	}

	cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return processInfo{}, err
	}
	return processInfo{Ppid: ppid, Argv: splitCmdline(cmdline)}, nil
}
