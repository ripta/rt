package cg

import (
	"os/exec"
	"strings"
	"syscall"
)

// escapeArgs formats a command and its arguments in a shell-readable style.
// Arguments containing spaces, quotes, or shell metacharacters are quoted.
func escapeArgs(args []string) string {
	escaped := make([]string, len(args))
	for i, arg := range args {
		escaped[i] = shellQuote(arg)
	}

	return strings.Join(escaped, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}

	safe := true
	for _, c := range s {
		if !isShellSafe(c) {
			safe = false
			break
		}
	}

	if safe {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func isShellSafe(c rune) bool {
	if c >= 'a' && c <= 'z' {
		return true
	}
	if c >= 'A' && c <= 'Z' {
		return true
	}
	if c >= '0' && c <= '9' {
		return true
	}
	switch c {
	case '-', '_', '.', '/', ':', '@', '+', ',', '=':
		return true
	}
	return false
}

func exitStatus(cmd *exec.Cmd) *syscall.WaitStatus {
	if cmd.ProcessState == nil {
		return nil
	}

	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		return nil
	}

	return &ws
}
