package approve

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// tildeEnv carries what expanding a leading ~ needs: the home directory, read once per load, and a
// lookup for ~user. Every compile in a session reuses the one a load built, so a change to $HOME
// mid-session cannot change what a rule matches.
//
// A home directory error is held, not returned at load. It fails only a rule that meets a ~ or ~/
// token, so a missing $HOME does not break a rules file that never uses one.
type tildeEnv struct {
	home       string
	homeErr    error
	lookupUser func(name string) (string, error)

	// passwdOnly selects the wording of the unknown-user error. It is a field so a test can cover
	// both wordings in one build.
	passwdOnly bool
}

// newTildeEnv reads the home directory once and fills in the defaults for a nil seam.
func newTildeEnv(home func() (string, error), lookup func(name string) (string, error)) *tildeEnv {
	if home == nil {
		home = os.UserHomeDir
	}
	if lookup == nil {
		lookup = lookupUserHome
	}

	env := &tildeEnv{lookupUser: lookup, passwdOnly: lookupReadsPasswdOnly}

	dir, err := home()
	switch {
	case err != nil:
		env.homeErr = fmt.Errorf("resolving home directory: %w", err)
	case !filepath.IsAbs(dir):
		env.homeErr = fmt.Errorf("resolving home directory: %q is not an absolute path", dir)
	default:
		env.home = dir
	}

	return env
}

// lookupUserHome asks the system user database for a user's home directory.
func lookupUserHome(name string) (string, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return "", err
	}

	return u.HomeDir, nil
}

// isTilde reports whether a first token has the home-relative shape: it starts with ~, with or
// without a slash.
func isTilde(token string) bool {
	return strings.HasPrefix(token, "~")
}

// UnresolvedUserError reports a ~user token whose user cannot be resolved on this machine.
type UnresolvedUserError struct {
	Token string
	User  string

	// PasswdOnly is set when this build reads only /etc/passwd, so a directory-service user is
	// never found.
	PasswdOnly bool

	Err error
}

func (e *UnresolvedUserError) Error() string {
	if e.PasswdOnly {
		return fmt.Sprintf("cannot expand %q: user %q is not in /etc/passwd, the only source this build "+
			"reads, so a user kept in a directory service cannot be resolved. Spell the path absolutely.",
			e.Token, e.User)
	}

	return fmt.Sprintf("cannot expand %q: looking up user %q: %v. Spell the path absolutely.", e.Token, e.User, e.Err)
}

func (e *UnresolvedUserError) Unwrap() error { return e.Err }

// expand turns a token that starts with ~ into an absolute path. A bare ~ or ~/ uses the home
// directory the load read. ~user always goes through the user database, even for the running user.
// The result is cleaned.
func (e *tildeEnv) expand(token string) (string, error) {
	if !isTilde(token) {
		return token, nil
	}

	name, rest := token[1:], ""
	if i := strings.IndexAny(token, "/"+string(filepath.Separator)); i >= 0 {
		name, rest = token[1:i], token[i:]
	}

	if name == "" {
		if e == nil {
			return "", fmt.Errorf("cannot expand %q: no home directory is configured", token)
		}
		if e.homeErr != nil {
			return "", fmt.Errorf("cannot expand %q: %w", token, e.homeErr)
		}

		return filepath.Join(e.home, rest), nil
	}

	failure := &UnresolvedUserError{Token: token, User: name}

	if e == nil || e.lookupUser == nil {
		failure.Err = errors.New("no user lookup is configured")
		return "", failure
	}

	dir, err := e.lookupUser(name)
	switch {
	case err != nil:
		failure.Err = err

		// Only an unknown-user answer proves the name is absent from the one source this build
		// reads. Any other failure must not claim that.
		var unknown user.UnknownUserError
		failure.PasswdOnly = e.passwdOnly && errors.As(err, &unknown)

		return "", failure
	case !filepath.IsAbs(dir):
		failure.Err = fmt.Errorf("home directory %q is not an absolute path", dir)
		return "", failure
	}

	return filepath.Join(dir, rest), nil
}

// expandRule is the one place a rule's first token is expanded. It records the result on the rule
// for compileMatch, and reads the rule's section to decide how an unresolvable ~user fails.
//
// A plain allow rule, exact or prefix, becomes inert. It matches no command, since a literal
// fallback would match <project>/~user/... and fail open for no reason. Any other rule returns the
// error. An inert deny or restrict rule would let a command through, and an inert wrapper rule
// would drop remember's exact-only guard for its program. An unresolvable home directory returns
// an error for every section.
func (e *tildeEnv) expandRule(rule *Rule, section ruleSection) error {
	rule.tilde, rule.inert, rule.tildeHead, rule.inertReason = false, false, "", ""

	tokens := tokensOf(rule)
	if len(tokens) == 0 || !isTilde(tokens[0]) {
		return nil
	}

	rule.tilde = true

	head, err := e.expand(tokens[0])
	if err == nil {
		rule.tildeHead = head
		return nil
	}

	var unresolved *UnresolvedUserError
	if errors.As(err, &unresolved) && section == sectionAllow && (rule.kind == KindExact || rule.kind == KindPrefix) {
		rule.inert = true
		rule.inertReason = err.Error()

		return nil
	}

	return err
}

// projectFile resolves one --project-config value. A value that starts with ~ is expanded. An
// absolute value, or one that is absolute after expansion, names that exact file. A relative value
// joins the project root.
func (e *tildeEnv) projectFile(root, value string) (string, error) {
	path, err := e.expand(value)
	if err != nil {
		return "", fmt.Errorf("project rules file: %w", err)
	}

	return ProjectPath(root, path), nil
}
