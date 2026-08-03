package model

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type exitCodeOp int

const (
	exitCodeEQ exitCodeOp = iota
	exitCodeNE
	exitCodeGE
	exitCodeGT
	exitCodeLE
	exitCodeLT
)

// ExitCodeFilter is a parsed --exit-code / exit_code comparison expression,
// e.g. "!=0" parses to {op: exitCodeNE, value: 0}. Construct via
// ParseExitCodeFilter.
//
// ExitCodeFilter has no representation for "no exit code recorded." That is a
// property of the row, not the expression: a pool summary row, a start-failed
// row, and a running/abandoned/unknown row carry no exit code at all. Callers
// decide per row kind whether to call Match.
type ExitCodeFilter struct {
	op    exitCodeOp
	value int
}

// exitCodeOps lists the operator prefixes in match-order. ">=" and "<="
// must be tried before their single-character counterparts ">" and "<", or
// ">=5" would match the ">" prefix first, leaving "=5" to fail strconv.Atoi.
var exitCodeOps = []struct {
	prefix string
	op     exitCodeOp
}{
	{"!=", exitCodeNE},
	{">=", exitCodeGE},
	{"<=", exitCodeLE},
	{">", exitCodeGT},
	{"<", exitCodeLT},
}

// ParseExitCodeFilter parses an exit-code comparison expression: a bare
// (optionally negative) integer means equality; "!=", ">=", "<=", ">", "<"
// prefixes select the other five operators.
func ParseExitCodeFilter(s string) (ExitCodeFilter, error) {
	s = strings.TrimSpace(s)
	for _, c := range exitCodeOps {
		rest, ok := strings.CutPrefix(s, c.prefix)
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil {
			return ExitCodeFilter{}, fmt.Errorf("invalid exit-code expression %q: %w", s, err)
		}
		return ExitCodeFilter{op: c.op, value: n}, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return ExitCodeFilter{}, fmt.Errorf("invalid exit-code expression %q: %w", s, err)
	}
	return ExitCodeFilter{op: exitCodeEQ, value: n}, nil
}

// Match reports whether code satisfies the filter.
func (f ExitCodeFilter) Match(code int) bool {
	switch f.op {
	case exitCodeEQ:
		return code == f.value
	case exitCodeNE:
		return code != f.value
	case exitCodeGE:
		return code >= f.value
	case exitCodeGT:
		return code > f.value
	case exitCodeLE:
		return code <= f.value
	case exitCodeLT:
		return code < f.value
	}
	return false
}

// ErrEmptyTimeRange is returned by NewTimeRangeFilter when since and before
// are both set and since is not strictly before before, which would exclude
// every row.
var ErrEmptyTimeRange = errors.New("since must be before before")

// ParseFilterTime parses a --since/--before TIME argument into an absolute
// instant, in this order: a full RFC3339 timestamp (RFC3339Nano, so an
// optional fractional-second component is accepted, matching the timestamp
// auto-detection in timestamp.go), a bare "2006-01-02" date interpreted as
// local-timezone midnight, or a relative duration meaning "ago" using
// ParsePruneDuration's grammar, anchored at now. now is threaded in rather
// than read from the wall clock so callers can resolve --since and --before
// against one consistent instant.
func ParseFilterTime(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time expression")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	if d, err := ParsePruneDuration(s); err == nil {
		return now.Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("invalid time expression %q: want a relative duration (e.g. 4h, 7d), an RFC3339 timestamp, or a YYYY-MM-DD date", s)
}

// TimeRangeFilter bundles parsed --since/--before bounds. A nil Since or
// Before is unconstrained on that side. Construct via NewTimeRangeFilter so
// the since-before-before invariant is checked once, at parse time, rather
// than at every row.
type TimeRangeFilter struct {
	Since  *time.Time
	Before *time.Time
}

// NewTimeRangeFilter validates and bundles since/before. since is the
// inclusive lower bound ("at or after"); before is the exclusive upper bound
// ("strictly before"). Returns ErrEmptyTimeRange when both are set and since
// is not strictly before before — an inclusive-lower/exclusive-upper range
// with equal bounds can never match anything either.
func NewTimeRangeFilter(since, before *time.Time) (TimeRangeFilter, error) {
	if since != nil && before != nil && !since.Before(*before) {
		return TimeRangeFilter{}, fmt.Errorf("%w: since (%s) is not before before (%s)",
			ErrEmptyTimeRange, since.Format(time.RFC3339), before.Format(time.RFC3339))
	}
	return TimeRangeFilter{Since: since, Before: before}, nil
}

// Match reports whether t falls in [Since, Before).
func (f TimeRangeFilter) Match(t time.Time) bool {
	if f.Since != nil && t.Before(*f.Since) {
		return false
	}
	if f.Before != nil && !t.Before(*f.Before) {
		return false
	}
	return true
}

// CwdFilter is a parsed --cwd DIR filter for cg ls. Construct via
// NewCwdFilter, which resolves dir once so every row comparison is a plain
// string equality.
type CwdFilter struct {
	resolved string
}

// NewCwdFilter resolves dir to an absolute, symlink-resolved path. Relative
// paths resolve against the process's own working directory, matching how a
// user would type --cwd on the command line; symlink resolution means a path
// like a symlinked $TMPDIR still matches the absolute cwd recorded on disk.
func NewCwdFilter(dir string) (CwdFilter, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return CwdFilter{}, fmt.Errorf("resolving --cwd %q: %w", dir, err)
	}
	return CwdFilter{resolved: resolveCwdSymlinks(abs)}, nil
}

// Match reports whether cwd, a run's recorded working directory, refers to
// the same directory as the filter. An empty cwd (nothing recorded for the
// row) never matches.
func (f CwdFilter) Match(cwd string) bool {
	if cwd == "" {
		return false
	}
	return resolveCwdSymlinks(cwd) == f.resolved
}

// resolveCwdSymlinks resolves path through any symlinks, falling back to path
// itself when the target no longer exists on disk (for example, a run
// recorded a cwd that was later removed).
func resolveCwdSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}
