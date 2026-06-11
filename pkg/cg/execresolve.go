package cg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Resolution is the executable identity cg derives from a command's argv[0]
// before it is approved and execed. The approval gate matches against the
// canonical form and RunCapture execs the same path, so the policy decision and
// the execution agree on which file runs.
type Resolution struct {
	// Argv is the original command, unchanged.
	Argv []string
	// Resolved is the absolute path after PATH lookup and cwd joining, before
	// symlink evaluation. Empty when argv[0] could not be located.
	Resolved string
	// Canonical is EvalSymlinks(Resolved). Empty when symlink evaluation fails;
	// the caller decides how to treat an uncanonicalizable command.
	Canonical string
}

// ResolveCommand computes the resolved and canonical executable path for argv.
//
// A bare program name is looked up against the server process PATH via
// exec.LookPath, before any caller-supplied env override is applied, so env
// cannot redirect the top-level executable. A slash-bearing absolute path is
// used as-is; a relative one is joined onto cwd when set and the server current
// directory otherwise. The resolved path is then canonicalized with
// EvalSymlinks.
//
// It returns a best-effort Resolution populated as far as resolution got,
// alongside an error, so a caller can still build start-failure diagnostics when
// resolution or canonicalization fails.
func ResolveCommand(argv []string, cwd string) (*Resolution, error) {
	r := &Resolution{Argv: argv}
	if len(argv) == 0 {
		return r, fmt.Errorf("command is empty")
	}

	resolved, err := resolveExecPath(argv[0], cwd)
	if err != nil {
		return r, err
	}
	r.Resolved = resolved

	canonical, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return r, fmt.Errorf("canonicalizing %s: %w", resolved, err)
	}
	r.Canonical = canonical
	return r, nil
}

// resolveExecPath turns argv0 into an absolute path. A bare name is resolved
// through the server PATH; a slash-bearing relative path is joined onto cwd (or
// the server cwd when cwd is empty).
func resolveExecPath(argv0, cwd string) (string, error) {
	if !strings.ContainsRune(argv0, filepath.Separator) && !strings.ContainsRune(argv0, '/') {
		found, err := exec.LookPath(argv0)
		if err != nil {
			return "", fmt.Errorf("resolving %s: %w", argv0, err)
		}
		return filepath.Abs(found)
	}

	if filepath.IsAbs(argv0) {
		return filepath.Clean(argv0), nil
	}

	base := cwd
	if base == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolving cwd for %s: %w", argv0, err)
		}
		base = wd
	}
	return filepath.Join(base, argv0), nil
}

// ExecPath is the path RunCapture execs: the canonical path when available, then
// the resolved path, falling back to the original argv[0] so a command that
// could not be resolved still surfaces its start failure through exec.
func (r *Resolution) ExecPath() string {
	if r == nil {
		return ""
	}
	switch {
	case r.Canonical != "":
		return r.Canonical
	case r.Resolved != "":
		return r.Resolved
	case len(r.Argv) > 0:
		return r.Argv[0]
	default:
		return ""
	}
}
