package cg

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
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
	cmd := NewCommand()
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
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), stdout)
	}
	// Columns are space-aligned by a tabwriter; status width is set by the widest
	// cell ("running"), so the finished rows pad out to match.
	if lines[0] != "AAAAAA  exit=0   12ms   echo new" {
		t.Errorf("line 0 = %q", lines[0])
	}
	if lines[1] != "CCCCCC  running  ?      ?" {
		t.Errorf("line 1 = %q", lines[1])
	}
	if lines[2] != "BBBBBB  exit=2   1.23s  sh -c 'exit 2'" {
		t.Errorf("line 2 = %q", lines[2])
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
	if len(lines) != 2 {
		t.Fatalf("expected pool row + standalone row, got %d: %q", len(lines), stdout)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "pool:finished") {
		t.Errorf("no pool row in %q", stdout)
	}
	if !strings.Contains(joined, "2 runs: 1 ok, 1 failed") {
		t.Errorf("no counts summary in %q", stdout)
	}
	if !strings.Contains(joined, "SSSSSS") {
		t.Errorf("standalone row missing from %q", stdout)
	}

	stdout, stderr, err = runCgSplit("ls", "--pool", "PPPPPP")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
	}
	lines = strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 member rows, got %d: %q", len(lines), stdout)
	}
	if strings.Contains(stdout, "SSSSSS") || strings.Contains(stdout, "pool:") {
		t.Errorf("member listing leaked non-members: %q", stdout)
	}

	stdout, _, err = runCgSplit("ls", "--pool", "none")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines = strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "SSSSSS") {
		t.Errorf("--pool none = %q, want just SSSSSS", stdout)
	}

	stdout, _, err = runCgSplit("ls", "--pool", "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines = strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 4 {
		t.Errorf("--pool any listed %d rows, want 4: %q", len(lines), stdout)
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

func TestFormatLsRowPool(t *testing.T) {
	t.Parallel()

	now := time.Now()
	finished := now.Add(-30 * time.Second)
	row := lsRow{
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
	got := formatLsRow(row, now)
	want := "PPPPPP\tpool:finished\t1m30s\t2 runs: 1 ok, 1 skipped"
	if got != want {
		t.Errorf("formatLsRow pool = %q, want %q", got, want)
	}

	row.pool.FinishedAt = nil
	row.poolState = PoolStateRunning
	got = formatLsRow(row, now)
	want = "PPPPPP\tpool:running\t2m0s\t2 runs: 1 ok, 1 skipped"
	if got != want {
		t.Errorf("formatLsRow running pool = %q, want %q", got, want)
	}
}

func TestFormatLsRowRunning(t *testing.T) {
	t.Parallel()

	now := time.Now()
	row := lsRow{
		id:    "DDDDDD",
		start: &StartInfo{RunInfo: RunInfo{Command: []string{"sleep", "30"}, StartedAt: now.Add(-90 * time.Second)}},
	}
	got := formatLsRow(row, now)
	want := "DDDDDD\trunning\t1m30s\tsleep 30"
	if got != want {
		t.Errorf("formatLsRow running = %q, want %q", got, want)
	}
}

func TestFormatLsRowRunningNoStartInfo(t *testing.T) {
	t.Parallel()

	got := formatLsRow(lsRow{id: "EEEEEE"}, time.Now())
	want := "EEEEEE\trunning\t?\t?"
	if got != want {
		t.Errorf("formatLsRow running fallback = %q, want %q", got, want)
	}
}

func TestFormatLsRowAbandoned(t *testing.T) {
	t.Parallel()

	now := time.Now()
	row := lsRow{
		id:        "ABANDN",
		start:     &StartInfo{RunInfo: RunInfo{Command: []string{"sleep", "600"}, StartedAt: now.Add(-90 * time.Second)}},
		abandoned: true,
	}
	got := formatLsRow(row, now)
	want := "ABANDN\tabandoned\t1m30s\tsleep 600"
	if got != want {
		t.Errorf("formatLsRow abandoned = %q, want %q", got, want)
	}

	got = formatLsRow(lsRow{id: "ABANDN", abandoned: true}, now)
	want = "ABANDN\tabandoned\t?\t?"
	if got != want {
		t.Errorf("formatLsRow abandoned fallback = %q, want %q", got, want)
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

	stdout, _, err := runCgSplit("ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), stdout)
	}
	if !strings.HasPrefix(lines[0], "ABANDN") || !strings.Contains(lines[0], "abandoned") {
		t.Errorf("line 0 = %q, want ABANDN abandoned", lines[0])
	}
	if !strings.HasPrefix(lines[1], "DDDDDD") || !strings.Contains(lines[1], "running") {
		t.Errorf("line 1 = %q, want DDDDDD running", lines[1])
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
	if len(lines) != 1 {
		t.Fatalf("expected 1 line with -n 1, got %d: %q", len(lines), stdout)
	}
	if !strings.HasPrefix(lines[0], "AAAAAA  ") {
		t.Errorf("line 0 = %q, want most-recent (AAAAAA) first", lines[0])
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

	stdout, _, err := runCgSplit("ls")
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
	want := "AAAAAA  signal=15  5ms  sleep 10\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}
