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
	dir := seedRunDir(t, "ABCDEF", &Meta{ID: "ABCDEF", Command: []string{"echo", "hi"}})

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
	dir := seedRunDir(t, "ABCDEF", &Meta{ID: "ABCDEF", Command: []string{"echo", "hi"}})

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
	dir := seedRunDir(t, "ABCDEF", &Meta{ID: "ABCDEF", Command: []string{"echo", "hi"}})

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
		ID:         "AAAAAA",
		Command:    []string{"echo", "new"},
		ExitCode:   0,
		DurationMs: 12,
	})
	// Older entry with valid meta and non-zero exit.
	dirOld := seedRunDir(t, "BBBBBB", &Meta{
		ID:         "BBBBBB",
		Command:    []string{"sh", "-c", "exit 2"},
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

func TestFormatLsRowRunning(t *testing.T) {
	t.Parallel()

	now := time.Now()
	row := lsRow{
		id:    "DDDDDD",
		start: &StartInfo{Command: []string{"sleep", "30"}, StartedAt: now.Add(-90 * time.Second)},
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

func TestLsCommandLimit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	dirA := seedRunDir(t, "AAAAAA", &Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})

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
	if err := WriteStartInfo(dir, &StartInfo{Command: []string{"sleep", "30"}, StartedAt: time.Now().Add(-5 * time.Second)}); err != nil {
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
		ID:         "AAAAAA",
		Command:    []string{"sleep", "10"},
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
