package model

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// ErrUnknownRunID is returned by LookupRunDir when id is malformed or the run
// directory does not exist under CaptureRoot.
var ErrUnknownRunID = errors.New("unknown run id")

// ErrIncompleteRun is returned by LookupRunDir when the run directory exists
// but neither meta.json nor debug.json is present, i.e. the capture is still
// in flight.
var ErrIncompleteRun = errors.New("incomplete run")

// ErrFailedRun is returned by LookupRunDir when the run directory has a
// debug.json but no meta.json, indicating the child process failed to start.
var ErrFailedRun = errors.New("failed run")

// IsValidRunID reports whether id has the right shape for a capture run ID:
// the Crockford base-32 alphabet, exactly runIDLen characters.
func IsValidRunID(id string) bool {
	if len(id) != runIDLen {
		return false
	}
	for _, r := range id {
		if !strings.ContainsRune(runIDAlphabet, r) {
			return false
		}
	}
	return true
}

// LookupRunDir resolves the per-run capture directory for id under
// CaptureRoot. It returns ErrUnknownRunID for malformed IDs and absent
// directories, and ErrIncompleteRun when the directory exists but meta.json
// is not present. The returned directory is always the joined path, even on
// error, so callers can decide whether to surface it (for example, when a
// caller wants to peek at an in-flight run's stdout).
func LookupRunDir(id string) (string, error) {
	dir := filepath.Join(CaptureRoot(), id)

	if !IsValidRunID(id) {
		return dir, ErrUnknownRunID
	}

	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return dir, ErrUnknownRunID
	}
	if err != nil {
		return dir, fmt.Errorf("stat run dir: %w", err)
	}

	if _, err := os.Stat(filepath.Join(dir, MetaFilename)); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return dir, fmt.Errorf("stat meta.json: %w", err)
		}
		if _, err := os.Stat(filepath.Join(dir, DebugFilename)); err == nil {
			return dir, ErrFailedRun
		}
		return dir, ErrIncompleteRun
	}

	return dir, nil
}

// resolveRunDir is the shell-side wrapper around LookupRunDir. It prints a
// single-line diagnostic to stderr for known sentinel errors and surfaces an
// ExitError{Code:1} so the cobra root maps it onto exit status 1.
func resolveRunDir(cmd *cobra.Command, id string) (string, error) {
	dir, err := LookupRunDir(id)
	if err == nil {
		return dir, nil
	}
	if errors.Is(err, ErrUnknownRunID) {
		fmt.Fprintf(cmd.ErrOrStderr(), "unknown run id: %s\n", id)
		return "", &ExitError{Code: 1}
	}
	if errors.Is(err, ErrFailedRun) {
		fmt.Fprintf(cmd.ErrOrStderr(), "failed run: %s (start failed; see debug.json)\n", id)
		return "", &ExitError{Code: 1}
	}
	if errors.Is(err, ErrIncompleteRun) {
		fmt.Fprintf(cmd.ErrOrStderr(), "incomplete run: %s (missing meta.json)\n", id)
		return "", &ExitError{Code: 1}
	}
	return "", err
}

// NewOutCommand returns the `cg out <ID>` subcommand. It prints the absolute
// path of the captured stdout file.
func NewOutCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "out <ID>",
		Short:         "Print the absolute path of a captured run's stdout file",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveRunDir(cmd, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), filepath.Join(dir, "stdout"))
			return nil
		},
	}
}

// NewErrCommand returns the `cg err <ID>` subcommand. It prints the absolute
// path of the captured stderr file.
func NewErrCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "err <ID>",
		Short:         "Print the absolute path of a captured run's stderr file",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveRunDir(cmd, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), filepath.Join(dir, "stderr"))
			return nil
		},
	}
}

// NewPathsCommand returns the `cg paths <ID>` subcommand. It prints the
// absolute paths of the captured stdout and stderr files, one per line,
// stdout first.
func NewPathsCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "paths <ID>",
		Short:         "Print the absolute paths of a captured run's stdout and stderr files",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveRunDir(cmd, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, filepath.Join(dir, "stdout"))
			fmt.Fprintln(out, filepath.Join(dir, "stderr"))
			return nil
		},
	}
}

// Values of the `--pool` flag on `cg ls` that are not pool IDs: `none` lists
// only standalone runs, `any` lists everything uncollapsed.
const (
	lsPoolNone = "none"
	lsPoolAny  = "any"
)

// lsStateAll is the `--state` value that disables state filtering. It is
// also `cg ls`'s default, matching its current behavior of listing every
// state.
const lsStateAll = "all"

// Values of the `--output`/`-o` flag on `cg ls`. table is the default: id,
// exit/status, runtime, and command. wide adds SYSTEM and USER cpu-time
// columns before command. json prints a `{"runs": [...]}` envelope instead of
// a table.
const (
	lsOutputTable = "table"
	lsOutputWide  = "wide"
	lsOutputJSON  = "json"
)

// lsOptions holds flags for the `cg ls` subcommand.
type lsOptions struct {
	N        int
	Pool     string
	State    string
	ExitCode string
	Since    string
	Before   string
	Cwd      string
	Output   string
}

// NewLsCommand returns the `cg ls` subcommand. It lists recent capture runs in
// most-recent-first order by directory mtime. Pool members collapse behind one
// row per pool unless --pool expands them.
func NewLsCommand() *cobra.Command {
	opts := &lsOptions{}
	c := &cobra.Command{
		Use:           "ls",
		Short:         "List recent capture runs, most-recent-first",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          opts.run,
	}
	c.Flags().IntVarP(&opts.N, "limit", "n", 20, "maximum number of runs to list; 0 or negative means unlimited")
	c.Flags().StringVar(&opts.Pool, "pool", "", "set to a `pool-id` to list that pool's members, none to list only standalone runs, or any to list everything uncollapsed; unset (the default) collapses each pool behind one summary row")
	c.Flags().StringVar(&opts.State, "state", lsStateAll, "state filter: all|finished|running|failed|abandoned|unknown")
	c.Flags().StringVar(&opts.ExitCode, "exit-code", "", "filter finished runs by exit code: N (equals), !=N, >=N, >N, <N, or <=N; pool summary rows always pass through")
	c.Flags().StringVar(&opts.Since, "since", "", "only list runs started at or after TIME: a duration ago (e.g. 4h, 7d), an RFC3339 timestamp, or a YYYY-MM-DD date")
	c.Flags().StringVar(&opts.Before, "before", "", "only list runs started strictly before TIME, same grammar as --since")
	c.Flags().StringVar(&opts.Cwd, "cwd", "", "filter to runs whose recorded working directory matches `DIR` (relative paths and symlinks are resolved before comparing)")
	c.Flags().StringVarP(&opts.Output, "output", "o", lsOutputTable, "output format: table, wide (adds SYSTEM/USER cpu-time columns), or json")
	return c
}

type lsRow struct {
	id        string
	mtime     time.Time
	meta      *Meta
	debug     *StartDebug
	start     *StartInfo
	abandoned bool
	unknown   bool
	pool      *PoolManifest
	poolState string
	memberOf  string
	cwd       string
}

// state reports the row's canonical state label for --state filtering. A
// pool row reports its own pool state, which already uses the same
// running/finished/abandoned vocabulary. lsRowValues's displayed STATE column
// matches this value, except a pool row's is prefixed with "pool:".
func (r lsRow) state() string {
	switch {
	case r.pool != nil:
		return r.poolState
	case r.debug != nil:
		return RunStateFailed
	case r.meta != nil:
		return RunStateFinished
	case r.unknown:
		return RunStateUnknown
	case r.abandoned:
		return RunStateAbandoned
	default:
		return RunStateRunning
	}
}

// filterStartTime returns the timestamp --since/--before compares this row
// against, and whether one is available. Pool, finished, and start-failed
// rows carry a precise recorded StartedAt. Running and abandoned rows do too
// when start.json survived. Everything else falls back to the directory's
// mtime, the same approximate source formatLsRow already uses for display.
func (r lsRow) filterStartTime() (time.Time, bool) {
	switch {
	case r.pool != nil:
		return r.pool.StartedAt, true
	case r.debug != nil:
		return r.debug.StartedAt, true
	case r.meta != nil:
		return r.meta.StartedAt, true
	case r.start != nil:
		return r.start.StartedAt, true
	case !r.mtime.IsZero():
		return r.mtime, true
	default:
		return time.Time{}, false
	}
}

func (opts *lsOptions) run(cmd *cobra.Command, args []string) error {
	switch {
	case opts.Pool == "", opts.Pool == lsPoolNone, opts.Pool == lsPoolAny, IsValidRunID(opts.Pool):
	default:
		fmt.Fprintf(cmd.ErrOrStderr(), "invalid --pool %q: want a pool ID, none, or any\n", opts.Pool)
		return &ExitError{Code: 2}
	}
	memberMode := opts.Pool != "" && opts.Pool != lsPoolNone && opts.Pool != lsPoolAny

	switch opts.State {
	case lsStateAll, RunStateFinished, RunStateRunning, RunStateFailed, RunStateAbandoned, RunStateUnknown:
	default:
		fmt.Fprintf(cmd.ErrOrStderr(), "invalid --state %q: want all|finished|running|failed|abandoned|unknown\n", opts.State)
		return &ExitError{Code: 2}
	}

	switch opts.Output {
	case lsOutputTable, lsOutputWide, lsOutputJSON:
	default:
		fmt.Fprintf(cmd.ErrOrStderr(), "invalid --output %q: want table|wide|json\n", opts.Output)
		return &ExitError{Code: 2}
	}

	var cwdFilter *CwdFilter
	if opts.Cwd != "" {
		f, err := NewCwdFilter(opts.Cwd)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "invalid --cwd: %v\n", err)
			return &ExitError{Code: 2}
		}
		cwdFilter = &f
	}

	var exitFilter *ExitCodeFilter
	if opts.ExitCode != "" {
		f, err := ParseExitCodeFilter(opts.ExitCode)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "invalid --exit-code: %v\n", err)
			return &ExitError{Code: 2}
		}
		exitFilter = &f
	}

	now := time.Now()
	var since, before *time.Time
	if opts.Since != "" {
		t, err := ParseFilterTime(opts.Since, now)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "invalid --since: %v\n", err)
			return &ExitError{Code: 2}
		}
		since = &t
	}
	if opts.Before != "" {
		t, err := ParseFilterTime(opts.Before, now)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "invalid --before: %v\n", err)
			return &ExitError{Code: 2}
		}
		before = &t
	}
	timeFilter, err := NewTimeRangeFilter(since, before)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "%v\n", err)
		return &ExitError{Code: 2}
	}

	root := CaptureRoot()
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading capture root: %w", err)
	}

	rows := make([]lsRow, 0, len(entries))
	poolDirs := make(map[string]bool)
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !IsValidRunID(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		row := lsRow{id: name, mtime: info.ModTime()}
		dir := filepath.Join(root, name)
		if m, err := ReadMeta(dir); err == nil {
			row.meta = m
			row.memberOf = m.Pool
			row.cwd = m.Cwd
		} else if p, err := ReadPoolManifest(dir); err == nil {
			row.pool = p
			row.poolState = PoolState(dir, p)
			row.cwd = p.Cwd
			poolDirs[name] = true
		} else if d, err := ReadStartDebug(dir); err == nil {
			row.debug = d
			row.memberOf = d.Pool
			row.cwd = d.Cwd
		} else {
			hasLock := LockFileExists(dir)
			row.abandoned = RunLockReleased(dir)
			if s, err := ReadStartInfo(dir); err == nil {
				row.start = s
				row.memberOf = s.Pool
				row.cwd = s.Cwd
			}
			row.unknown = !hasLock && row.start == nil
		}
		rows = append(rows, row)
	}

	// Collapse before truncating so a big pool cannot bury the listing. A
	// member whose pool dir is gone is an orphan and counts as standalone.
	kept := rows[:0]
	for _, r := range rows {
		switch {
		case memberMode:
			if r.memberOf != opts.Pool {
				continue
			}
		case opts.Pool == lsPoolAny:
		case opts.Pool == lsPoolNone:
			if r.pool != nil || (r.memberOf != "" && poolDirs[r.memberOf]) {
				continue
			}
		default:
			if r.pool == nil && r.memberOf != "" && poolDirs[r.memberOf] {
				continue
			}
		}
		kept = append(kept, r)
	}
	rows = kept

	if memberMode && len(rows) == 0 && !poolDirs[opts.Pool] {
		return fmt.Errorf("unknown pool id: %s", opts.Pool)
	}

	if opts.State != lsStateAll {
		kept = rows[:0]
		for _, r := range rows {
			if r.state() == opts.State {
				kept = append(kept, r)
			}
		}
		rows = kept
	}

	if exitFilter != nil {
		kept = rows[:0]
		for _, r := range rows {
			switch {
			case r.pool != nil:
				kept = append(kept, r) // pool summary rows always pass through
			case r.meta != nil && exitFilter.Match(r.meta.ExitCode):
				kept = append(kept, r)
			}
		}
		rows = kept
	}

	if since != nil || before != nil {
		kept = rows[:0]
		for _, r := range rows {
			if t, ok := r.filterStartTime(); ok && timeFilter.Match(t) {
				kept = append(kept, r)
			}
		}
		rows = kept
	}

	if cwdFilter != nil {
		kept = rows[:0]
		for _, r := range rows {
			if cwdFilter.Match(r.cwd) {
				kept = append(kept, r)
			}
		}
		rows = kept
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].mtime.After(rows[j].mtime)
	})

	if opts.N > 0 && len(rows) > opts.N {
		rows = rows[:opts.N]
	}

	if opts.Output == lsOutputJSON {
		return writeJSON(cmd.OutOrStdout(), lsJSONOutput{Runs: lsRowsToJSON(rows)})
	}
	return writeLsTable(cmd.OutOrStdout(), rows, now, opts.Output == lsOutputWide)
}

// writeLsTable renders rows as a tab-separated table with a header row.
// Column order is CG ID, EXIT, RUNTIME, [STATE, SYSTEM, USER,] COMMAND; STATE
// and the cpu-time columns only appear under wide. CG ID, STATE, and COMMAND
// stay tabwriter's natural left-aligned; EXIT, RUNTIME, SYSTEM, and USER are
// right-justified to a shared width computed across the whole listing (a
// tabwriter can only align every column the same direction). EXIT holds only
// a bare exit or negated-signal number, or "?" when a row has none, so a long
// STATE word like "pool:abandoned" never drags EXIT's column wide the way it
// did when the two were combined.
func writeLsTable(w io.Writer, rows []lsRow, now time.Time, wide bool) error {
	type row struct {
		id, state, exit, runtime, system, user, command string
	}

	data := make([]row, len(rows))
	exitWidth, runtimeWidth := len("EXIT"), len("RUNTIME")
	systemWidth, userWidth := len("SYSTEM"), len("USER")
	for i, r := range rows {
		state, exit, runtime, system, user, command := lsRowValues(r, now)
		data[i] = row{r.id, state, exit, runtime, system, user, command}
		exitWidth = max(exitWidth, len(exit))
		runtimeWidth = max(runtimeWidth, len(runtime))
		systemWidth = max(systemWidth, len(system))
		userWidth = max(userWidth, len(user))
	}

	pad := func(s string, width int) string {
		return fmt.Sprintf("%*s", width, s)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)

	header := []string{"CG ID", pad("EXIT", exitWidth), pad("RUNTIME", runtimeWidth)}
	if wide {
		header = append(header, "STATE", pad("SYSTEM", systemWidth), pad("USER", userWidth))
	}
	header = append(header, "COMMAND")
	fmt.Fprintln(tw, strings.Join(header, "\t"))

	for _, d := range data {
		cols := []string{d.id, pad(d.exit, exitWidth), pad(d.runtime, runtimeWidth)}
		if wide {
			cols = append(cols, d.state, pad(d.system, systemWidth), pad(d.user, userWidth))
		}
		cols = append(cols, d.command)
		fmt.Fprintln(tw, strings.Join(cols, "\t"))
	}
	return tw.Flush()
}

// lsRowValues returns one row's STATE, EXIT, RUNTIME, SYSTEM, and USER field
// values plus its rendered COMMAND text. Finished runs read their exit code
// (or, for a signaled run, its negated signal number), duration, and cpu
// usage from meta.json; failed runs read the command from debug.json;
// in-flight and abandoned runs read the command and exact elapsed time from
// start.json. A run with no lock file, pid file, or start.json carries no
// liveness signal at all, so it is reported as unknown rather than running.
// Runs without start.json fall back to the run directory's mtime for an
// approximate elapsed time, prefixed with "~" to mark it as an estimate
// rather than a measurement; the command stays unknown in that case, since
// mtime carries no command information. Pool rows show the pool state and a
// member-count summary in place of a command. EXIT, SYSTEM, and USER are "?"
// for every row kind except a finished run, which alone carries an exit code
// and resource accounting. The caller right-justifies EXIT/RUNTIME/SYSTEM/USER
// and aligns everything with a tabwriter.
func lsRowValues(r lsRow, now time.Time) (state, exit, runtime, system, user, command string) {
	if r.pool != nil {
		dur := formatDuration(now.Sub(r.pool.StartedAt))
		if r.pool.FinishedAt != nil {
			dur = formatDuration(r.pool.FinishedAt.Sub(r.pool.StartedAt))
		}
		return "pool:" + r.poolState, "?", dur, "?", "?", formatPoolCounts(r.pool.Counts())
	}
	if r.debug != nil {
		return RunStateFailed, "?", "?", "?", "?", formatLsCommand(r.debug.Command)
	}
	if r.meta != nil {
		exit := fmt.Sprintf("%d", r.meta.ExitCode)
		if r.meta.Signal != nil {
			exit = fmt.Sprintf("%d", -*r.meta.Signal)
		}
		dur := formatDuration(time.Duration(r.meta.DurationMs) * time.Millisecond)
		sys, usr := "?", "?"
		if r.meta.Usage != nil {
			sys = formatDuration(r.meta.Usage.systemDuration())
			usr = formatDuration(r.meta.Usage.userDuration())
		}
		return RunStateFinished, exit, dur, sys, usr, formatLsCommand(r.meta.Command)
	}

	status := RunStateRunning
	switch {
	case r.unknown:
		status = RunStateUnknown
	case r.abandoned:
		status = RunStateAbandoned
	}
	if r.start != nil {
		elapsed := formatDuration(now.Sub(r.start.StartedAt))
		return status, "?", elapsed, "?", "?", formatLsCommand(r.start.Command)
	}
	if !r.mtime.IsZero() {
		elapsed := "~" + formatDuration(now.Sub(r.mtime))
		return status, "?", elapsed, "?", "?", "?"
	}
	return status, "?", "?", "?", "?", "?"
}

// formatLsCommand renders cmd for the table/wide COMMAND column: shell-quoted
// via EscapeArgs, then with control characters escaped so a raw newline or
// tab embedded in an argument can't split the row across physical lines or
// misalign the tabwriter's columns.
func formatLsCommand(cmd []string) string {
	return escapeLsControlChars(EscapeArgs(cmd))
}

// escapeLsControlChars replaces literal backslash, newline, carriage return,
// and tab bytes in s with their two-character escapes, backslash first so the
// escapes it introduces aren't themselves re-escaped.
func escapeLsControlChars(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// formatPoolCounts renders a pool's member tally, like "6 runs: 4 ok, 1
// failed, 1 skipped". Zero categories are omitted.
func formatPoolCounts(c PoolCounts) string {
	noun := "runs"
	if c.Total == 1 {
		noun = "run"
	}

	parts := make([]string, 0, 5)
	for _, p := range []struct {
		n     int
		label string
	}{
		{c.Succeeded, "ok"},
		{c.Failed, "failed"},
		{c.Skipped, "skipped"},
		{c.Running, "running"},
		{c.Pending, "pending"},
	} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.label))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d %s", c.Total, noun)
	}
	return fmt.Sprintf("%d %s: %s", c.Total, noun, strings.Join(parts, ", "))
}

// lsJSONOutput is the `cg ls -o json` envelope.
type lsJSONOutput struct {
	Runs []lsJSONRun `json:"runs"`
}

// lsJSONRun is one element of the `runs` array. It follows the same shape as
// `cg meta`: metaFields carries the finished/running fields (Cwd included),
// Debug carries a start-failed run's diagnostics, and Manifest is set only
// for a pool summary row.
type lsJSONRun struct {
	ID       string        `json:"id"`
	State    string        `json:"state"`
	Debug    *StartDebug   `json:"debug,omitempty"`
	Manifest *PoolManifest `json:"manifest,omitempty"`
	metaFields
}

// lsRowsToJSON converts rows to their `cg ls -o json` representation.
func lsRowsToJSON(rows []lsRow) []lsJSONRun {
	out := make([]lsJSONRun, len(rows))
	for i, r := range rows {
		out[i] = lsRowToJSON(r)
	}
	return out
}

// lsRowToJSON converts one row to its JSON shape, matching the state and
// field set `cg meta` would report for the same run (or pool) ID.
func lsRowToJSON(r lsRow) lsJSONRun {
	switch {
	case r.pool != nil:
		return lsJSONRun{ID: r.id, State: r.poolState, Manifest: r.pool, metaFields: metaFields{Cwd: r.pool.Cwd}}
	case r.debug != nil:
		return lsJSONRun{
			ID:         r.id,
			State:      RunStateFailed,
			Debug:      r.debug,
			metaFields: metaFields{Command: r.debug.Command, Cwd: r.debug.Cwd},
		}
	case r.meta != nil:
		return lsJSONRun{ID: r.id, State: RunStateFinished, metaFields: metaFieldsFrom(r.meta)}
	case r.start != nil:
		state := RunStateRunning
		if r.abandoned {
			state = RunStateAbandoned
		}
		return lsJSONRun{ID: r.id, State: state, metaFields: metaFieldsFromStart(r.start)}
	default:
		state := RunStateRunning
		switch {
		case r.unknown:
			state = RunStateUnknown
		case r.abandoned:
			state = RunStateAbandoned
		}
		return lsJSONRun{ID: r.id, State: state}
	}
}
