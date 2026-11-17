package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// ErrExecutableChanged reports that the cg executable on disk is no longer the one the server
// recorded at start. Errors that wrap it say to restart cg.
var ErrExecutableChanged = errors.New("the cg executable changed")

// ExecIdentity records the cg executable a server re-executes as each run's supervisor. Every run
// starts an unsandboxed supervisor from the file at Path, so a command that can write that file
// could otherwise run its own code outside its sandbox on the next run. Verify catches that before
// the spawn.
//
// Path is what os.Executable returned. Target is Path with symlinks resolved. SHA256 hashes Target.
// The identity rides the pool spec so a pool supervisor checks the same file the server recorded,
// not whatever its own start found.
type ExecIdentity struct {
	Path   string `json:"path"`
	Target string `json:"target"`
	SHA256 string `json:"sha256"`

	// Prefix names the path of the cg command below the executable. It is empty for a standalone
	// cg and `cg` when the multi-call binary mounts cg as `rt cg`.
	Prefix []string `json:"prefix,omitempty"`
}

// CommandPrefix returns the command path from the executable down to the cg command that owns
// cmd, without the executable's own name. The supervisors are subcommands of cg. A multi-call
// binary that mounts cg under another command only knows them behind that mount.
func CommandPrefix(cmd *cobra.Command) []string {
	parent := cmd.Parent()
	if parent == nil {
		return nil
	}

	path := strings.Fields(parent.CommandPath())
	if len(path) <= 1 {
		return nil
	}
	return path[1:]
}

// CaptureExecIdentity records the identity of the running cg executable. prefix is the cg command
// path below the executable. See CommandPrefix.
func CaptureExecIdentity(prefix []string) (*ExecIdentity, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locating cg executable: %w", err)
	}

	id, err := NewExecIdentity(exe)
	if err != nil {
		return nil, err
	}

	id.Prefix = prefix
	return id, nil
}

// NewExecIdentity records the identity of the executable at path. Tests use it to pin a copied
// binary in place of the running one.
func NewExecIdentity(path string) (*ExecIdentity, error) {
	target, sum, err := resolveAndHash(path)
	if err != nil {
		return nil, fmt.Errorf("recording cg executable: %w", err)
	}
	return &ExecIdentity{Path: path, Target: target, SHA256: sum}, nil
}

// Verify resolves and hashes the executable again. It returns ErrExecutableChanged, wrapped with
// what differs, when the target or the content is not the one recorded.
func (id *ExecIdentity) Verify() error {
	target, sum, err := resolveAndHash(id.Path)
	if err != nil {
		return changedError("%s cannot be read: %v", id.Path, err)
	}
	if target != id.Target {
		return changedError("%s now resolves to %s, not %s", id.Path, target, id.Target)
	}
	if sum != id.SHA256 {
		return changedError("the contents of %s differ", target)
	}
	return nil
}

func changedError(format string, args ...any) error {
	return fmt.Errorf("%w since this server started (%s); restart cg", ErrExecutableChanged, fmt.Sprintf(format, args...))
}

// command builds the supervisor command. It execs the verified target and keeps Path as argv[0],
// because the multi-call binary picks its command from the name it was invoked by. Prefix goes
// between argv[0] and the arguments.
func (id *ExecIdentity) command(args ...string) *exec.Cmd {
	cmd := exec.Command(id.Target, append(slices.Clone(id.Prefix), args...)...)
	cmd.Args[0] = id.Path
	return cmd
}

func resolveAndHash(path string) (target, sum string, err error) {
	target, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}

	f, err := os.Open(target)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", "", fmt.Errorf("hashing %s: %w", target, err)
	}
	return target, hex.EncodeToString(h.Sum(nil)), nil
}
