package model

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
)

// The parsers below read the raw formats of the darwin and linux process lookups. They are
// untagged so both are tested on either platform.

// parseProcArgs2 decodes a kern.procargs2 buffer: a native-endian int32 argc, the executable path,
// NUL padding, then argc NUL-terminated arguments. The environment follows and is ignored.
func parseProcArgs2(raw []byte) ([]string, error) {
	if len(raw) < 4 {
		return nil, errors.New("short procargs2 buffer")
	}

	argc := int(int32(binary.NativeEndian.Uint32(raw)))
	if argc < 0 {
		return nil, fmt.Errorf("negative argc %d in procargs2", argc)
	}
	rest := raw[4:]

	exeEnd := bytes.IndexByte(rest, 0)
	if exeEnd < 0 {
		return nil, errors.New("unterminated executable path in procargs2")
	}
	rest = bytes.TrimLeft(rest[exeEnd:], "\x00")

	// argc comes from the target process's memory, so it only bounds the loop. Each argument needs
	// at least its NUL terminator, which caps the allocation by the buffer's size.
	argv := make([]string, 0, min(argc, len(rest)))
	for i := 0; i < argc; i++ {
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			return nil, fmt.Errorf("procargs2 holds %d of %d arguments", i, argc)
		}
		argv = append(argv, string(rest[:end]))
		rest = rest[end+1:]
	}
	return argv, nil
}

// parseProcStatPpid reads the parent pid from /proc/<pid>/stat. The command name is parenthesized and
// may itself contain parentheses and spaces, so fields are counted from the last closing parenthesis.
func parseProcStatPpid(stat []byte) (int, error) {
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, fmt.Errorf("malformed stat: no command name")
	}

	fields := bytes.Fields(stat[end+1:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("malformed stat: %d fields after the command name", len(fields))
	}

	ppid, err := strconv.Atoi(string(fields[1]))
	if err != nil {
		return 0, fmt.Errorf("malformed stat ppid: %w", err)
	}
	return ppid, nil
}

func splitCmdline(cmdline []byte) []string {
	cmdline = bytes.TrimSuffix(cmdline, []byte{0})
	if len(cmdline) == 0 {
		return nil
	}

	parts := bytes.Split(cmdline, []byte{0})
	argv := make([]string, len(parts))
	for i, p := range parts {
		argv[i] = string(p)
	}
	return argv
}
