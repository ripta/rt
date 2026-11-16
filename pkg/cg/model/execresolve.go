package model

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Resolution is the executable identity cg derives from a command's argv[0]
// before it is approved and execed. The approval gate matches against the
// canonical form. The supervisor execs the resolved path and checks that it still
// evaluates to that canonical path, so the policy decision and the execution agree
// on which file runs.
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

// CanonicalArgv is the argv the approval matcher evaluates against canonical
// rules: the symlink-evaluated executable path followed by the original argument
// tail. It is nil when canonicalization did not succeed, so a command with an
// unknown executable identity cannot be allowed by canonical policy.
func (r *Resolution) CanonicalArgv() []string {
	if r == nil || r.Canonical == "" || len(r.Argv) == 0 {
		return nil
	}

	out := make([]string, len(r.Argv))
	out[0] = r.Canonical
	copy(out[1:], r.Argv[1:])
	return out
}

// ResolvedArgv is the pre-symlink counterpart of CanonicalArgv: the resolved
// absolute path followed by the original argument tail. It is nil when
// resolution did not succeed. The two differ when the executable is reached
// through a symlink that renames it, as with a multiplexer shim like rustup's
// cargo, where the resolved path is the only absolute form that still names the
// invoked tool.
func (r *Resolution) ResolvedArgv() []string {
	if r == nil || r.Resolved == "" || len(r.Argv) == 0 {
		return nil
	}

	out := make([]string, len(r.Argv))
	out[0] = r.Resolved
	copy(out[1:], r.Argv[1:])
	return out
}

// RulePath is the executable path a suggested approval rule pins. It is the
// canonical path, except when symlink evaluation changes the executable's
// basename: that marks a multiplexer shim whose argv[0] is the command
// identity, so the resolved path is used and the rule keeps naming the tool
// that was invoked. When resolution is incomplete it falls back like ExecPath.
func (r *Resolution) RulePath() string {
	if r == nil {
		return ""
	}
	if r.Canonical != "" && r.Resolved != "" && filepath.Base(r.Canonical) != filepath.Base(r.Resolved) {
		return r.Resolved
	}

	return r.ExecPath()
}

// ExecPath is the canonical path when available, then the resolved path, falling
// back to the original argv[0] so a command that could not be resolved still
// surfaces its start failure through exec.
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

// SpawnPath is the path the supervisor execs: the resolved path when there is one, and
// ExecPath otherwise. A program that finds its own files from the path it was exec'd
// through, such as a Python venv interpreter linked to its base interpreter, only
// works when that path is the one the user typed. Execing the symlink-evaluated path
// would hand it the base install.
func (r *Resolution) SpawnPath() string {
	if r != nil && r.Resolved != "" {
		return r.Resolved
	}

	return r.ExecPath()
}

// VerifyCanonical checks that the resolved path still evaluates to the canonical path the
// approval gate matched. Execing the resolved path gives up the pin that execing the
// canonical path had, so a symlink retargeted after approval would otherwise run a file
// nobody judged. It is a check made just before the spawn, so a retarget in the window
// between the check and the exec is not caught. A resolution without both paths has
// nothing to compare.
func (r *Resolution) VerifyCanonical() error {
	if r == nil || r.Resolved == "" || r.Canonical == "" {
		return nil
	}

	now, err := filepath.EvalSymlinks(r.Resolved)
	if err != nil {
		return fmt.Errorf("re-checking %s before starting it: %w", r.Resolved, err)
	}
	if now != r.Canonical {
		return fmt.Errorf("%s changed since it was approved: it resolved to %s then and resolves to %s now",
			r.Resolved, r.Canonical, now)
	}

	return nil
}
