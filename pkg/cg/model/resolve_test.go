package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type isValidRunIDTest struct {
	name string
	id   string
	want bool
}

var isValidRunIDTests = []isValidRunIDTest{
	{name: "valid uppercase", id: "Q3F9K2", want: true},
	{name: "valid all digits", id: "012345", want: true},
	{name: "empty", id: "", want: false},
	{name: "too short", id: "ABC12", want: false},
	{name: "too long", id: "ABC1234", want: false},
	{name: "lowercase", id: "q3f9k2", want: false},
	{name: "contains I", id: "ABCDIE", want: false},
	{name: "contains L", id: "ABCDLE", want: false},
	{name: "contains O", id: "ABCDOE", want: false},
	{name: "contains U", id: "ABCDUE", want: false},
	{name: "contains space", id: "ABC DE", want: false},
}

func TestIsValidRunID(t *testing.T) {
	t.Parallel()

	for _, tt := range isValidRunIDTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidRunID(tt.id); got != tt.want {
				t.Errorf("IsValidRunID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

// runCgSplit invokes the cg cobra command with separate buffers for stdout and
// stderr so resolution-error tests can distinguish them.
func runCgSplit(args ...string) (stdout, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	cmd := newTestRoot()
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

// seedRunDir creates a fake capture run dir at $TMPDIR/cg/<id> with stdout,
// stderr, and (when meta is non-nil) meta.json.
func seedRunDir(t *testing.T, id string, meta *Meta) string {
	t.Helper()
	dir := filepath.Join(CaptureRoot(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	for _, name := range []string{"stdout", "stderr"} {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		f.Close()
	}
	if meta != nil {
		if err := WriteMeta(dir, meta); err != nil {
			t.Fatalf("WriteMeta: %v", err)
		}
	}
	return dir
}

func assertExitCode1(t *testing.T, err error) {
	t.Helper()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.Code)
	}
}

func TestOutCommandUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	stdout, stderr, err := runCgSplit("out", "ABCDEF")
	assertExitCode1(t, err)
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if stderr != "unknown run id: ABCDEF\n" {
		t.Errorf("stderr = %q, want %q", stderr, "unknown run id: ABCDEF\n")
	}
}

func TestOutCommandInvalidIDFormat(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, stderr, err := runCgSplit("out", "lowercase")
	assertExitCode1(t, err)
	if !strings.Contains(stderr, "unknown run id: lowercase") {
		t.Errorf("stderr = %q, want to contain unknown run id message", stderr)
	}
}

func TestOutCommandIncompleteRun(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	seedRunDir(t, "ABCDEF", nil)

	stdout, stderr, err := runCgSplit("out", "ABCDEF")
	assertExitCode1(t, err)
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "incomplete run: ABCDEF") {
		t.Errorf("stderr = %q, want to contain incomplete run message", stderr)
	}
}

func TestOutCommand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir := seedRunDir(t, "ABCDEF", &Meta{RunInfo: RunInfo{ID: "ABCDEF", Command: []string{"echo", "hi"}}})

	stdout, stderr, err := runCgSplit("out", "ABCDEF")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
	}
	want := filepath.Join(dir, "stdout") + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestErrCommand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir := seedRunDir(t, "ABCDEF", &Meta{RunInfo: RunInfo{ID: "ABCDEF", Command: []string{"echo", "hi"}}})

	stdout, stderr, err := runCgSplit("err", "ABCDEF")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
	}
	want := filepath.Join(dir, "stderr") + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestPathsCommand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir := seedRunDir(t, "ABCDEF", &Meta{RunInfo: RunInfo{ID: "ABCDEF", Command: []string{"echo", "hi"}}})

	stdout, stderr, err := runCgSplit("paths", "ABCDEF")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
	}
	want := filepath.Join(dir, "stdout") + "\n" + filepath.Join(dir, "stderr") + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestLsCommandNoCaptureRoot(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	stdout, stderr, err := runCgSplit("ls")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestLsCommand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	// Newer entry with valid meta.
	dirNew := seedRunDir(t, "AAAAAA", &Meta{
		RunInfo:    RunInfo{ID: "AAAAAA", Command: []string{"echo", "new"}},
		ExitCode:   0,
		DurationMs: 12,
	})
	// Older entry with valid meta and non-zero exit.
	dirOld := seedRunDir(t, "BBBBBB", &Meta{
		RunInfo:    RunInfo{ID: "BBBBBB", Command: []string{"sh", "-c", "exit 2"}},
		ExitCode:   2,
		DurationMs: 1234,
	})
	// Incomplete entry: directory only, no meta.json.
	dirIncomplete := seedRunDir(t, "CCCCCC", nil)
	// Non-Crockford name; must be skipped.
	if err := os.MkdirAll(filepath.Join(root, "lowercase"), 0o755); err != nil {
		t.Fatalf("mkdir junk: %v", err)
	}

	now := time.Now()
	if err := os.Chtimes(dirNew, now, now); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}
	if err := os.Chtimes(dirIncomplete, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("chtimes incomplete: %v", err)
	}
	if err := os.Chtimes(dirOld, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}

	stdout, stderr, err := runCgSplit("ls")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header + 3 lines, got %d: %q", len(lines), stdout)
	}
	// Columns are space-aligned by a tabwriter; each column's width is set by its
	// widest cell, so the other rows pad out to match, which is why assertions
	// below tokenize with strings.Fields rather than compare exact spacing.
	// CCCCCC has no meta.json, no lock file, and no start.json, so it carries no
	// liveness signal at all and falls back to an approximate, mtime-derived
	// duration.
	if got := strings.Fields(lines[0]); !reflect.DeepEqual(got, []string{"CG", "ID", "EXIT", "RUNTIME", "COMMAND"}) {
		t.Errorf("header = %q, want CG ID / EXIT / RUNTIME / COMMAND (STATE is wide-only)", lines[0])
	}
	if f := strings.Fields(lines[1]); f[0] != "AAAAAA" || f[1] != "0" || f[2] != "12ms" || strings.Join(f[3:], " ") != "echo new" {
		t.Errorf("line 1 = %q", lines[1])
	}
	if f := strings.Fields(lines[2]); f[0] != "CCCCCC" || f[1] != "?" || f[2] != "~1h0m0s" || f[3] != "?" {
		t.Errorf("line 2 = %q", lines[2])
	}
	if f := strings.Fields(lines[3]); f[0] != "BBBBBB" || f[1] != "2" || f[2] != "1.23s" || strings.Join(f[3:], " ") != "sh -c 'exit 2'" {
		t.Errorf("line 3 = %q", lines[3])
	}
}

// seedPoolDir creates a pool directory holding only the manifest. Lock state
// is layered on by the caller.
func seedPoolDir(t *testing.T, id string, m *PoolManifest) string {
	t.Helper()
	dir := filepath.Join(CaptureRoot(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := WritePoolManifest(dir, m); err != nil {
		t.Fatalf("WritePoolManifest: %v", err)
	}
	return dir
}

func TestLsCommandCollapsesPools(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	finished := time.Now().UTC()
	seedPoolDir(t, "PPPPPP", &PoolManifest{
		ID:         "PPPPPP",
		Commands:   [][]string{{"echo", "hi"}},
		StartedAt:  finished.Add(-2 * time.Second),
		FinishedAt: &finished,
		Runs: []PoolRunRecord{
			{Command: 0, RunID: "AAAAAA", Status: PoolRunFinished, ExitCode: intp(0)},
			{Command: 0, RunID: "BBBBBB", Status: PoolRunFinished, ExitCode: intp(1)},
		},
	})
	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, Pool: "PPPPPP"}})
	seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "hi"}, Pool: "PPPPPP"}, ExitCode: 1})
	seedRunDir(t, "SSSSSS", &Meta{RunInfo: RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})

	stdout, stderr, err := runCgSplit("ls")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + pool row + standalone row, got %d: %q", len(lines), stdout)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "PPPPPP") {
		t.Errorf("no pool row in %q", stdout)
	}
	if !strings.Contains(joined, "2 runs: 1 ok, 1 failed") {
		t.Errorf("no counts summary in %q", stdout)
	}
	if !strings.Contains(joined, "SSSSSS") {
		t.Errorf("standalone row missing from %q", stdout)
	}

	// The pool's STATE ("pool:finished") is wide-only.
	stdout, _, err = runCgSplit("ls", "-o", "wide")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "pool:finished") {
		t.Errorf("no pool:finished state in wide output: %q", stdout)
	}

	stdout, stderr, err = runCgSplit("ls", "--pool", "PPPPPP")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
	}
	lines = strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 member rows, got %d: %q", len(lines), stdout)
	}
	if strings.Contains(stdout, "SSSSSS") || strings.Contains(stdout, "pool:") {
		t.Errorf("member listing leaked non-members: %q", stdout)
	}

	stdout, _, err = runCgSplit("ls", "--pool", "none")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines = strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "SSSSSS") {
		t.Errorf("--pool none = %q, want header + just SSSSSS", stdout)
	}

	stdout, _, err = runCgSplit("ls", "--pool", "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines = strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 5 {
		t.Errorf("--pool any listed %d lines, want header + 4 rows: %q", len(lines), stdout)
	}
}

func TestLsCommandPoolFlagValidation(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, stderr, err := runCgSplit("ls", "--pool", "not-an-id")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("expected exit code 2, got %v", err)
	}
	if !strings.Contains(stderr, "invalid --pool") {
		t.Errorf("stderr = %q, want invalid --pool message", stderr)
	}
}

func TestLsCommandUnknownPool(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	_, _, err := runCgSplit("ls", "--pool", "ZZZZZZ")
	if err == nil {
		t.Fatal("expected unknown pool error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown pool id") {
		t.Errorf("err = %v, want unknown pool id", err)
	}
}

func TestLsCommandOrphanMemberVisible(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, Pool: "PPPPPP"}})

	stdout, _, err := runCgSplit("ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "AAAAAA") {
		t.Errorf("orphan member missing from default listing: %q", stdout)
	}
}

// lsRowValuesTest is one lsRowValues table-test case. row is built lazily so
// it can reference the shared `now` fixture for relative timestamps.
type lsRowValuesTest struct {
	name                                               string
	row                                                func(now time.Time) lsRow
	wantState, wantExit, wantRuntime, wantSys, wantUsr string
	wantCommand                                        string
}

var lsRowValuesTests = []lsRowValuesTest{
	{
		name: "pool finished",
		row: func(now time.Time) lsRow {
			finished := now.Add(-30 * time.Second)
			return lsRow{
				id:        "PPPPPP",
				poolState: PoolStateFinished,
				pool: &PoolManifest{
					StartedAt:  finished.Add(-90 * time.Second),
					FinishedAt: &finished,
					Runs: []PoolRunRecord{
						{Status: PoolRunFinished, ExitCode: intp(0)},
						{Status: PoolRunSkipped},
					},
				},
			}
		},
		wantState: "pool:finished", wantExit: "?", wantRuntime: "1m30s", wantSys: "?", wantUsr: "?",
		wantCommand: "2 runs: 1 ok, 1 skipped",
	},
	{
		name: "pool running",
		row: func(now time.Time) lsRow {
			return lsRow{
				id:        "PPPPPP",
				poolState: PoolStateRunning,
				pool: &PoolManifest{
					StartedAt: now.Add(-2 * time.Minute),
					Runs: []PoolRunRecord{
						{Status: PoolRunFinished, ExitCode: intp(0)},
						{Status: PoolRunSkipped},
					},
				},
			}
		},
		wantState: "pool:running", wantExit: "?", wantRuntime: "2m0s", wantSys: "?", wantUsr: "?",
		wantCommand: "2 runs: 1 ok, 1 skipped",
	},
	{
		name: "running with start info",
		row: func(now time.Time) lsRow {
			return lsRow{
				id:    "DDDDDD",
				start: &StartInfo{RunInfo: RunInfo{Command: []string{"sleep", "30"}, StartedAt: now.Add(-90 * time.Second)}},
			}
		},
		wantState: "running", wantExit: "?", wantRuntime: "1m30s", wantSys: "?", wantUsr: "?",
		wantCommand: "sleep 30",
	},
	{
		// A zero mtime carries no timestamp at all, so it stays the
		// unresolved fallback; real rows always have a directory mtime.
		name:      "running with no start info or mtime",
		row:       func(now time.Time) lsRow { return lsRow{id: "EEEEEE"} },
		wantState: "running", wantExit: "?", wantRuntime: "?", wantSys: "?", wantUsr: "?",
		wantCommand: "?",
	},
	{
		name: "running mtime fallback",
		row: func(now time.Time) lsRow {
			return lsRow{id: "FFFFFF", mtime: now.Add(-90 * time.Second)}
		},
		wantState: "running", wantExit: "?", wantRuntime: "~1m30s", wantSys: "?", wantUsr: "?",
		wantCommand: "?",
	},
	{
		name: "unknown",
		row: func(now time.Time) lsRow {
			return lsRow{id: "UUUUUU", mtime: now.Add(-5 * time.Minute), unknown: true}
		},
		wantState: "unknown", wantExit: "?", wantRuntime: "~5m0s", wantSys: "?", wantUsr: "?",
		wantCommand: "?",
	},
	{
		name: "abandoned with start info",
		row: func(now time.Time) lsRow {
			return lsRow{
				id:        "ABANDN",
				start:     &StartInfo{RunInfo: RunInfo{Command: []string{"sleep", "600"}, StartedAt: now.Add(-90 * time.Second)}},
				abandoned: true,
			}
		},
		wantState: "abandoned", wantExit: "?", wantRuntime: "1m30s", wantSys: "?", wantUsr: "?",
		wantCommand: "sleep 600",
	},
	{
		name: "abandoned mtime fallback",
		row: func(now time.Time) lsRow {
			return lsRow{id: "ABANDN", abandoned: true, mtime: now.Add(-90 * time.Second)}
		},
		wantState: "abandoned", wantExit: "?", wantRuntime: "~1m30s", wantSys: "?", wantUsr: "?",
		wantCommand: "?",
	},
	{
		name: "finished with usage",
		row: func(now time.Time) lsRow {
			return lsRow{
				id: "AAAAAA",
				meta: &Meta{
					RunInfo:    RunInfo{Command: []string{"echo", "hi"}},
					DurationMs: 12,
					Usage:      &Usage{UserUS: 20000, SystemUS: 5000},
				},
			}
		},
		wantState: "finished", wantExit: "0", wantRuntime: "12ms", wantSys: "5ms", wantUsr: "20ms",
		wantCommand: "echo hi",
	},
	{
		name: "finished without usage",
		row: func(now time.Time) lsRow {
			return lsRow{id: "AAAAAA", meta: &Meta{RunInfo: RunInfo{Command: []string{"echo", "hi"}}, DurationMs: 12}}
		},
		wantState: "finished", wantExit: "0", wantRuntime: "12ms", wantSys: "?", wantUsr: "?",
		wantCommand: "echo hi",
	},
	{
		name: "signaled",
		row: func(now time.Time) lsRow {
			sig := 15
			return lsRow{
				id:   "AAAAAA",
				meta: &Meta{RunInfo: RunInfo{Command: []string{"sleep", "10"}}, ExitCode: -1, Signal: &sig, DurationMs: 5},
			}
		},
		wantState: "finished", wantExit: "-15", wantRuntime: "5ms", wantSys: "?", wantUsr: "?",
		wantCommand: "sleep 10",
	},
	{
		name: "failed to start",
		row: func(now time.Time) lsRow {
			return lsRow{id: "FA1LED", debug: &StartDebug{RunInfo: RunInfo{Command: []string{"nope"}}, StartError: "exec: not found"}}
		},
		wantState: RunStateFailed, wantExit: "?", wantRuntime: "?", wantSys: "?", wantUsr: "?",
		wantCommand: "nope",
	},
}

func TestLsRowValues(t *testing.T) {
	t.Parallel()

	for _, tt := range lsRowValuesTests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			state, exit, runtime, sys, usr, command := lsRowValues(tt.row(now), now)
			if state != tt.wantState || exit != tt.wantExit || runtime != tt.wantRuntime ||
				sys != tt.wantSys || usr != tt.wantUsr || command != tt.wantCommand {
				t.Errorf("lsRowValues = (%q, %q, %q, %q, %q, %q), want (%q, %q, %q, %q, %q, %q)",
					state, exit, runtime, sys, usr, command,
					tt.wantState, tt.wantExit, tt.wantRuntime, tt.wantSys, tt.wantUsr, tt.wantCommand)
			}
		})
	}
}

func TestFormatLsRowEmbeddedNewline(t *testing.T) {
	t.Parallel()

	now := time.Now()
	row := lsRow{
		id:   "WQKSRR",
		meta: &Meta{RunInfo: RunInfo{Command: []string{"echo", "line one\nline two", "col1\tcol2"}}},
	}
	_, _, _, _, _, command := lsRowValues(row, now)
	if strings.Contains(command, "\n") {
		t.Fatalf("command contains a raw newline, which would split the tabwriter row: %q", command)
	}
	want := `echo 'line one\nline two' 'col1\tcol2'`
	if command != want {
		t.Errorf("command with embedded newline/tab = %q, want %q", command, want)
	}
}

func TestLsCommandAbandonedRun(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	// Abandoned: the lock file exists but nothing holds it.
	dirAband := seedRunDir(t, "ABANDN", nil)
	lock, err := acquireRunLock(dirAband)
	if err != nil {
		t.Fatalf("acquiring lock: %v", err)
	}
	lock.Close()

	// Live supervised run: the lock is held.
	dirHeld := seedRunDir(t, "DDDDDD", nil)
	held, err := acquireRunLock(dirHeld)
	if err != nil {
		t.Fatalf("holding lock: %v", err)
	}
	defer held.Close()

	now := time.Now()
	if err := os.Chtimes(dirAband, now, now); err != nil {
		t.Fatalf("chtimes abandoned: %v", err)
	}
	if err := os.Chtimes(dirHeld, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("chtimes held: %v", err)
	}

	// STATE is wide-only.
	stdout, _, err := runCgSplit("ls", "-o", "wide")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 lines, got %d: %q", len(lines), stdout)
	}
	if !strings.HasPrefix(lines[1], "ABANDN") || !strings.Contains(lines[1], "abandoned") {
		t.Errorf("line 1 = %q, want ABANDN abandoned", lines[1])
	}
	if !strings.HasPrefix(lines[2], "DDDDDD") || !strings.Contains(lines[2], "running") {
		t.Errorf("line 2 = %q, want DDDDDD running", lines[2])
	}
}

func TestLsCommandUnknownRun(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	// No lock file, no pid file, no start.json: zero liveness signal.
	dir := seedRunDir(t, "ZZZZZZ", nil)

	now := time.Now()
	if err := os.Chtimes(dir, now.Add(-5*time.Minute), now.Add(-5*time.Minute)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// STATE is wide-only.
	stdout, _, err := runCgSplit("ls", "-o", "wide")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 line, got %d: %q", len(lines), stdout)
	}
	if !strings.HasPrefix(lines[1], "ZZZZZZ") || !strings.Contains(lines[1], "unknown") || !strings.Contains(lines[1], "~5m0s") {
		t.Errorf("line 1 = %q, want ZZZZZZ unknown ~5m0s", lines[1])
	}
}

func TestLsCommandLimit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	dirA := seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "a"}}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "b"}}})

	now := time.Now()
	if err := os.Chtimes(dirA, now, now); err != nil {
		t.Fatalf("chtimes a: %v", err)
	}
	if err := os.Chtimes(dirB, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("chtimes b: %v", err)
	}

	stdout, stderr, err := runCgSplit("ls", "-n", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 line with -n 1, got %d: %q", len(lines), stdout)
	}
	if !strings.HasPrefix(lines[1], "AAAAAA") {
		t.Errorf("line 1 = %q, want most-recent (AAAAAA) first", lines[1])
	}
}

func TestLsCommandUnlimited(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "a"}}})
	seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "b"}}})

	for _, n := range []string{"0", "-1"} {
		stdout, stderr, err := runCgSplit("ls", "-n", n)
		if err != nil {
			t.Fatalf("-n %s: unexpected error: %v (stderr=%q)", n, err, stderr)
		}
		lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
		if len(lines) != 3 {
			t.Fatalf("-n %s: expected header + 2 lines, got %d: %q", n, len(lines), stdout)
		}
	}
}

func TestLsCommandRunningReadsStartInfo(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	dir := seedRunDir(t, "DDDDDD", nil)
	if err := WriteStartInfo(dir, &StartInfo{RunInfo: RunInfo{Command: []string{"sleep", "30"}, StartedAt: time.Now().Add(-5 * time.Second)}}); err != nil {
		t.Fatalf("WriteStartInfo: %v", err)
	}

	// STATE ("running") is wide-only.
	stdout, _, err := runCgSplit("ls", "-o", "wide")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"DDDDDD", "running", "sleep 30"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("ls output missing %q:\n%s", want, stdout)
		}
	}
}

func TestLsCommandSignaledMeta(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	sig := 15
	seedRunDir(t, "AAAAAA", &Meta{
		RunInfo:    RunInfo{ID: "AAAAAA", Command: []string{"sleep", "10"}},
		ExitCode:   -1,
		Signal:     &sig,
		DurationMs: 5,
	})

	stdout, _, err := runCgSplit("ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 line, got %d: %q", len(lines), stdout)
	}
	// A signaled run shows its negated signal number in EXIT, not "signal=N":
	// easier to spot at a glance than an encoded >=128 exit code.
	if f := strings.Fields(lines[1]); f[0] != "AAAAAA" || f[1] != "-15" || f[2] != "5ms" || strings.Join(f[3:], " ") != "sleep 10" {
		t.Errorf("signaled row = %q, want id=AAAAAA exit=-15 runtime=5ms command=%q", lines[1], "sleep 10")
	}
}

func TestLsCommandStateFilter(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}}})

	dirRunning := seedRunDir(t, "RRRRRR", nil)
	held, err := acquireRunLock(dirRunning)
	if err != nil {
		t.Fatalf("holding lock: %v", err)
	}
	defer held.Close()

	dirAband := seedRunDir(t, "BBBBBB", nil)
	lock, err := acquireRunLock(dirAband)
	if err != nil {
		t.Fatalf("acquiring lock: %v", err)
	}
	lock.Close()

	seedRunDir(t, "ZZZZZZ", nil) // no lock, no start.json: unknown

	dirFailed := seedRunDir(t, "FFFFFF", nil)
	if err := WriteStartDebug(dirFailed, &StartDebug{RunInfo: RunInfo{ID: "FFFFFF", Command: []string{"nope"}}, StartError: "exec: not found"}); err != nil {
		t.Fatalf("WriteStartDebug: %v", err)
	}

	tests := []struct {
		state string
		want  string
	}{
		{state: "finished", want: "AAAAAA"},
		{state: "running", want: "RRRRRR"},
		{state: "abandoned", want: "BBBBBB"},
		{state: "unknown", want: "ZZZZZZ"},
		{state: "failed", want: "FFFFFF"},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			stdout, _, err := runCgSplit("ls", "--state", tt.state)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
			if len(lines) != 2 || !strings.HasPrefix(lines[1], tt.want) {
				t.Errorf("--state %s = %q, want header + just %s", tt.state, stdout, tt.want)
			}
		})
	}

	stdout, _, err := runCgSplit("ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Count(stdout, "\n") != 6 {
		t.Errorf("default (all) = %q, want header + 5 rows", stdout)
	}

	_, stderr, err := runCgSplit("ls", "--state", "bogus")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("expected exit code 2, got %v", err)
	}
	if !strings.Contains(stderr, "invalid --state") {
		t.Errorf("stderr = %q, want invalid --state message", stderr)
	}
}

func TestLsCommandExitCodeFilter(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "ok"}}, ExitCode: 0})
	seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"sh", "-c", "exit 1"}}, ExitCode: 1})
	seedRunDir(t, "CCCCCC", &Meta{RunInfo: RunInfo{ID: "CCCCCC", Command: []string{"sh", "-c", "exit 2"}}, ExitCode: 2})
	sig := 9
	seedRunDir(t, "DDDDDD", &Meta{RunInfo: RunInfo{ID: "DDDDDD", Command: []string{"sleep", "10"}}, ExitCode: -1, Signal: &sig})

	tests := []struct {
		expr    string
		include []string
		exclude []string
	}{
		{expr: "0", include: []string{"AAAAAA"}, exclude: []string{"BBBBBB", "CCCCCC", "DDDDDD"}},
		{expr: "!=0", include: []string{"BBBBBB", "CCCCCC", "DDDDDD"}, exclude: []string{"AAAAAA"}},
		{expr: ">=1", include: []string{"BBBBBB", "CCCCCC"}, exclude: []string{"AAAAAA", "DDDDDD"}},
		{expr: ">1", include: []string{"CCCCCC"}, exclude: []string{"AAAAAA", "BBBBBB", "DDDDDD"}},
		{expr: "<1", include: []string{"DDDDDD", "AAAAAA"}, exclude: []string{"BBBBBB", "CCCCCC"}},
		{expr: "<=1", include: []string{"AAAAAA", "BBBBBB", "DDDDDD"}, exclude: []string{"CCCCCC"}},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			stdout, _, err := runCgSplit("ls", "--exit-code", tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, id := range tt.include {
				if !strings.Contains(stdout, id) {
					t.Errorf("--exit-code %s = %q, want %s included", tt.expr, stdout, id)
				}
			}
			for _, id := range tt.exclude {
				if strings.Contains(stdout, id) {
					t.Errorf("--exit-code %s = %q, want %s excluded", tt.expr, stdout, id)
				}
			}
		})
	}

	_, stderr, err := runCgSplit("ls", "--exit-code", "banana")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("expected exit code 2, got %v", err)
	}
	if !strings.Contains(stderr, "invalid --exit-code") {
		t.Errorf("stderr = %q, want invalid --exit-code message", stderr)
	}
}

func TestLsCommandExitCodePoolExemption(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedPoolWithMembers(t, "PPPPPP", "AAAAAA", "BBBBBB") // exit 0 and exit 1
	seedRunDir(t, "SSSSSS", &Meta{RunInfo: RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}, ExitCode: 0})

	// Collapsed: the pool row always passes through --exit-code; the solo
	// exit-0 run does not match "!=0".
	stdout, _, err := runCgSplit("ls", "--exit-code", "!=0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "PPPPPP") {
		t.Errorf("pool row missing under --exit-code filter: %q", stdout)
	}
	if strings.Contains(stdout, "SSSSSS") {
		t.Errorf("solo run with exit 0 leaked under --exit-code '!=0': %q", stdout)
	}

	// Expanded: pool members are filtered normally, with no exemption.
	stdout, _, err = runCgSplit("ls", "--pool", "any", "--exit-code", "!=0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "PPPPPP") {
		t.Errorf("pool row missing under --pool any --exit-code filter: %q", stdout)
	}
	if strings.Contains(stdout, "AAAAAA") {
		t.Errorf("matching-zero member AAAAAA leaked under --exit-code '!=0': %q", stdout)
	}
	if !strings.Contains(stdout, "BBBBBB") {
		t.Errorf("non-matching member BBBBBB missing under --exit-code '!=0': %q", stdout)
	}
}

func TestLsCommandSinceBeforeFilter(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirOld := seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "old"}, StartedAt: now.Add(-3 * time.Hour)}})
	dirNew := seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "new"}, StartedAt: now.Add(-30 * time.Minute)}})
	chtimes(t, dirOld, now.Add(-3*time.Hour))
	chtimes(t, dirNew, now.Add(-30*time.Minute))

	// duration-ago form
	stdout, _, err := runCgSplit("ls", "--since", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stdout, "AAAAAA") || !strings.Contains(stdout, "BBBBBB") {
		t.Errorf("--since 1h = %q, want just BBBBBB", stdout)
	}

	stdout, _, err = runCgSplit("ls", "--before", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "AAAAAA") || strings.Contains(stdout, "BBBBBB") {
		t.Errorf("--before 1h = %q, want just AAAAAA", stdout)
	}

	// RFC3339 form
	sinceRFC := now.Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	stdout, _, err = runCgSplit("ls", "--since", sinceRFC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stdout, "AAAAAA") || !strings.Contains(stdout, "BBBBBB") {
		t.Errorf("--since %s = %q, want just BBBBBB", sinceRFC, stdout)
	}

	// bare date form
	tomorrow := now.Add(24 * time.Hour).Format("2006-01-02")
	stdout, _, err = runCgSplit("ls", "--before", tomorrow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "AAAAAA") || !strings.Contains(stdout, "BBBBBB") {
		t.Errorf("--before %s = %q, want both rows", tomorrow, stdout)
	}

	// combined range: [now-2h, now-1h) excludes both AAAAAA (now-3h) and
	// BBBBBB (now-30m, which is after the exclusive upper bound).
	stdout, _, err = runCgSplit("ls", "--since", "2h", "--before", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stdout, "AAAAAA") || strings.Contains(stdout, "BBBBBB") {
		t.Errorf("--since 2h --before 1h = %q, want neither row", stdout)
	}
}

func TestLsCommandSinceAfterBeforeErrors(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, stderr, err := runCgSplit("ls", "--since", "1h", "--before", "2h")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("expected exit code 2, got %v", err)
	}
	if !strings.Contains(stderr, "since") || !strings.Contains(stderr, "before") {
		t.Errorf("stderr = %q, want a since/before range error", stderr)
	}
}

func TestLsCommandSinceBeforeAppliesToPoolRows(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	finished := now.Add(-2 * time.Hour)
	dirPool := seedPoolDir(t, "PPPPPP", &PoolManifest{
		ID:         "PPPPPP",
		Commands:   [][]string{{"echo", "hi"}},
		StartedAt:  now.Add(-3 * time.Hour),
		FinishedAt: &finished,
		Runs:       []PoolRunRecord{{Command: 0, Status: PoolRunFinished, ExitCode: intp(0)}},
	})
	chtimes(t, dirPool, finished)

	// The pool started 3h ago: --since 1h must exclude it, unlike
	// --exit-code, which always lets pool rows through.
	stdout, _, err := runCgSplit("ls", "--since", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stdout, "PPPPPP") {
		t.Errorf("--since 1h = %q, want pool excluded (started 3h ago)", stdout)
	}

	stdout, _, err = runCgSplit("ls", "--before", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "PPPPPP") {
		t.Errorf("--before 1h = %q, want pool included (started 3h ago)", stdout)
	}
}

func TestLsCommandUnknownRowSinceUsesMtime(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	dir := seedRunDir(t, "ZZZZZZ", nil) // no lock, no start.json: unknown
	chtimes(t, dir, time.Now().Add(-2*time.Hour))

	stdout, _, err := runCgSplit("ls", "--since", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stdout, "ZZZZZZ") {
		t.Errorf("--since 1h = %q, want unknown row excluded (mtime 2h ago)", stdout)
	}

	stdout, _, err = runCgSplit("ls", "--before", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "ZZZZZZ") {
		t.Errorf("--before 1h = %q, want unknown row included (mtime 2h ago)", stdout)
	}
}

func TestLsCommandCombinedFilters(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()

	// Matches every filter: finished, exit != 0, started within the last 4h.
	dirMatch := seedRunDir(t, "AAAAAA", &Meta{
		RunInfo:  RunInfo{ID: "AAAAAA", Command: []string{"sh", "-c", "exit 1"}, StartedAt: now.Add(-1 * time.Hour)},
		ExitCode: 1,
	})
	chtimes(t, dirMatch, now.Add(-1*time.Hour))

	// Wrong exit code.
	dirOkExit := seedRunDir(t, "BBBBBB", &Meta{
		RunInfo:  RunInfo{ID: "BBBBBB", Command: []string{"echo", "ok"}, StartedAt: now.Add(-1 * time.Hour)},
		ExitCode: 0,
	})
	chtimes(t, dirOkExit, now.Add(-1*time.Hour))

	// Too old.
	dirOld := seedRunDir(t, "CCCCCC", &Meta{
		RunInfo:  RunInfo{ID: "CCCCCC", Command: []string{"sh", "-c", "exit 1"}, StartedAt: now.Add(-5 * time.Hour)},
		ExitCode: 1,
	})
	chtimes(t, dirOld, now.Add(-5*time.Hour))

	// Not finished at all.
	seedRunDir(t, "DDDDDD", nil)

	stdout, _, err := runCgSplit("ls", "--state", "finished", "--exit-code", "!=0", "--since", "4h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "AAAAAA") {
		t.Errorf("combined filters = %q, want header + just AAAAAA", stdout)
	}
}

func TestLsCommandOutputFlagValidation(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, stderr, err := runCgSplit("ls", "-o", "bogus")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("expected exit code 2, got %v", err)
	}
	if !strings.Contains(stderr, "invalid --output") {
		t.Errorf("stderr = %q, want invalid --output message", stderr)
	}
}

func TestLsCommandWideOutput(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedRunDir(t, "AAAAAA", &Meta{
		RunInfo:    RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, SessionID: "SESS"},
		ExitCode:   0,
		DurationMs: 12,
		Usage:      &Usage{UserUS: 20000, SystemUS: 5000},
	})
	seedRunDir(t, "BBBBBB", nil) // no liveness signal at all: unknown, no usage, no session

	stdout, _, err := runCgSplit("ls", "-o", "wide")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 rows, got %d: %q", len(lines), stdout)
	}

	wantHeader := []string{"CG", "ID", "SESSION", "EXIT", "RUNTIME", "STATE", "SYSTEM", "USER", "COMMAND"}
	if got := strings.Fields(lines[0]); !reflect.DeepEqual(got, wantHeader) {
		t.Errorf("header = %q, want %v", lines[0], wantHeader)
	}

	var finished, unknown string
	for _, l := range lines[1:] {
		switch {
		case strings.HasPrefix(l, "AAAAAA"):
			finished = l
		case strings.HasPrefix(l, "BBBBBB"):
			unknown = l
		}
	}
	// SESSION sits after CG ID, so state/system/user shift one field right.
	if f := strings.Fields(finished); f[1] != "SESS" || f[4] != "finished" || f[5] != "5ms" || f[6] != "20ms" {
		t.Errorf("finished wide row = %q, want session=SESS state=finished system=5ms user=20ms", finished)
	}
	if f := strings.Fields(unknown); f[1] != "-" || f[4] != "unknown" || f[5] != "?" || f[6] != "?" {
		t.Errorf("unknown wide row = %q, want session=- state=unknown ?/? placeholders", unknown)
	}
}

func TestLsCommandJSONOutput(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedRunDir(t, "AAAAAA", &Meta{
		RunInfo:    RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, Cwd: "/work", SessionID: "SESS"},
		ExitCode:   0,
		DurationMs: 12,
	})

	stdout, stderr, err := runCgSplit("ls", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
	}

	var out lsJSONOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("unmarshalling json: %v\n%s", err, stdout)
	}
	if len(out.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d: %s", len(out.Runs), stdout)
	}
	run := out.Runs[0]
	if run.ID != "AAAAAA" || run.State != RunStateFinished {
		t.Errorf("run = %+v, want id=AAAAAA state=finished", run)
	}
	if run.Cwd != "/work" {
		t.Errorf("run.Cwd = %q, want /work", run.Cwd)
	}
	if run.SessionID != "SESS" {
		t.Errorf("run.SessionID = %q, want SESS", run.SessionID)
	}
	if run.ExitCode == nil || *run.ExitCode != 0 {
		t.Errorf("run.ExitCode = %v, want 0", run.ExitCode)
	}
}

func TestLsCommandJSONOutputPoolRow(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	finished := time.Now().UTC()
	seedPoolDir(t, "PPPPPP", &PoolManifest{
		ID:         "PPPPPP",
		SessionID:  "SESS",
		Commands:   [][]string{{"echo", "hi"}},
		Cwd:        "/work",
		StartedAt:  finished.Add(-2 * time.Second),
		FinishedAt: &finished,
		Runs: []PoolRunRecord{
			{Command: 0, RunID: "AAAAAA", Status: PoolRunFinished, ExitCode: intp(0)},
		},
	})
	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, Pool: "PPPPPP", SessionID: "SESS"}})

	stdout, _, err := runCgSplit("ls", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out lsJSONOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("unmarshalling json: %v\n%s", err, stdout)
	}
	if len(out.Runs) != 1 {
		t.Fatalf("expected 1 collapsed pool run, got %d: %s", len(out.Runs), stdout)
	}
	run := out.Runs[0]
	if run.ID != "PPPPPP" || run.State != PoolStateFinished {
		t.Errorf("run = %+v, want id=PPPPPP state=finished", run)
	}
	if run.Manifest == nil {
		t.Fatal("run.Manifest is nil, want the pool manifest")
	}
	if run.Cwd != "/work" {
		t.Errorf("run.Cwd = %q, want /work", run.Cwd)
	}
	if run.SessionID != "SESS" {
		t.Errorf("run.SessionID = %q, want SESS", run.SessionID)
	}
	if run.Manifest.SessionID != "SESS" {
		t.Errorf("run.Manifest.SessionID = %q, want SESS", run.Manifest.SessionID)
	}
}

func TestLsCommandCwdFilter(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	workDir := t.TempDir()
	otherDir := t.TempDir()
	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, Cwd: workDir}})
	seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "hi"}, Cwd: otherDir}})

	stdout, _, err := runCgSplit("ls", "--cwd", workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "AAAAAA") || strings.Contains(stdout, "BBBBBB") {
		t.Errorf("--cwd %s = %q, want just AAAAAA", workDir, stdout)
	}
}

func TestLsCommandSessionIDFilter(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, SessionID: "S1"}})
	seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "hi"}, SessionID: "S2"}})

	stdout, _, err := runCgSplit("ls", "--session-id", "S1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "AAAAAA") || strings.Contains(stdout, "BBBBBB") {
		t.Errorf("--session-id S1 = %q, want just AAAAAA", stdout)
	}

	// An unmatched session lists nothing, and is not an error.
	stdout, _, err = runCgSplit("ls", "--session-id", "NOPE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stdout, "AAAAAA") || strings.Contains(stdout, "BBBBBB") {
		t.Errorf("--session-id NOPE = %q, want no rows", stdout)
	}
}

func TestLsCommandCwdFilterRelativePath(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, Cwd: wd}})

	stdout, _, err := runCgSplit("ls", "--cwd", ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "AAAAAA") {
		t.Errorf("--cwd . = %q, want AAAAAA (relative path resolves against %s)", stdout, wd)
	}
}

func TestLsCommandCwdFilterSymlink(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// The run recorded its cwd as the symlink path, as a shell might report
	// it; the filter is given the resolved real path.
	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, Cwd: link}})

	stdout, _, err := runCgSplit("ls", "--cwd", real)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "AAAAAA") {
		t.Errorf("--cwd %s (real) = %q, want AAAAAA (recorded cwd was the symlink %s)", real, stdout, link)
	}
}

func TestEscapeLsControlChars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain text unchanged", in: "hello world", want: "hello world"},
		{name: "newline", in: "line one\nline two", want: `line one\nline two`},
		{name: "tab", in: "col1\tcol2", want: `col1\tcol2`},
		{name: "carriage return", in: "a\rb", want: `a\rb`},
		{name: "literal backslash", in: `C:\path`, want: `C:\\path`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeLsControlChars(tt.in); got != tt.want {
				t.Errorf("escapeLsControlChars(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
