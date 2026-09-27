package model

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// ErrNotRegularFile is returned when a run file is a symlink, FIFO, device, or
// anything else that is not a plain file.
var ErrNotRegularFile = errors.New("not a regular file")

// ErrUntrustedDir is returned when the capture root or notes root is not a real
// directory owned by the current user.
var ErrUntrustedDir = errors.New("not a directory owned by the current user")

// OpenRunFile opens a file inside the capture root for reading. It refuses a
// symlink in the final component and anything that is not a regular file.
//
// The capture root is writable by every process running as this user,
// including sandboxed ones that the MCP server is not. Following a planted
// symlink would let such a process read files through the server that its
// sandbox denies it, and opening a planted FIFO would hang the reader.
// O_NONBLOCK keeps the FIFO open from blocking; it has no effect on reads of
// regular files.
func OpenRunFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegularFile)
	}
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegularFile)
	}
	return f, nil
}

// readRunFile is os.ReadFile with OpenRunFile's refusals.
func readRunFile(path string) ([]byte, error) {
	f, err := OpenRunFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
}

// writeRunFile is os.WriteFile that refuses to follow a symlink in the final
// component. Without it, a link planted in a run directory would redirect an
// unsandboxed supervisor's write to any file the user owns.
func writeRunFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644)
	if errors.Is(err, syscall.ELOOP) {
		return fmt.Errorf("%s: %w", path, ErrNotRegularFile)
	}
	if err != nil {
		return err
	}

	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// createRunFile creates a file that must not already exist. The run directory
// is freshly allocated, so any existing entry, including a symlink, was planted.
func createRunFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
}

// checkOwnedDir reports whether path is a real directory, not a symlink to one,
// owned by the current user. A missing path surfaces as fs.ErrNotExist so
// callers can treat an absent root as empty.
//
// On a shared TMPDIR such as /tmp, another user could create the root first,
// or plant a symlink there, and then read or steer every run.
func checkOwnedDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: %w", path, ErrUntrustedDir)
	}

	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s: %w", path, ErrUntrustedDir)
	}
	return nil
}

// ensureOwnedDir creates path if needed and then applies checkOwnedDir.
func ensureOwnedDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	return checkOwnedDir(path)
}

// ReadCaptureRoot lists the capture root after checking that it is trusted. A
// missing root surfaces as fs.ErrNotExist.
func ReadCaptureRoot() ([]os.DirEntry, error) {
	root := CaptureRoot()
	if err := checkOwnedDir(root); err != nil {
		return nil, err
	}
	return os.ReadDir(root)
}
