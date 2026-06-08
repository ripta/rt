package cg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type parsePruneDurationTest struct {
	name    string
	in      string
	want    time.Duration
	wantErr bool
}

var parsePruneDurationTests = []parsePruneDurationTest{
	{name: "days", in: "7d", want: 7 * 24 * time.Hour},
	{name: "single day", in: "1d", want: 24 * time.Hour},
	{name: "weeks", in: "2w", want: 14 * 24 * time.Hour},
	{name: "single week", in: "1w", want: 7 * 24 * time.Hour},
	{name: "hours", in: "2h", want: 2 * time.Hour},
	{name: "minutes", in: "90m", want: 90 * time.Minute},
	{name: "compound go duration", in: "1h30m", want: 90 * time.Minute},
	{name: "empty", in: "", wantErr: true},
	{name: "mixed days hours", in: "7d12h", wantErr: true},
	{name: "negative days", in: "-1d", wantErr: true},
	{name: "garbage", in: "abc", wantErr: true},
	{name: "bare unit", in: "d", wantErr: true},
}

func TestParsePruneDuration(t *testing.T) {
	t.Parallel()

	for _, tt := range parsePruneDurationTests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePruneDuration(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parsePruneDuration(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePruneDuration(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parsePruneDuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// chtimes is a small helper that fails the test loudly if Chtimes errors out.
func chtimes(t *testing.T, dir string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(dir, when, when); err != nil {
		t.Fatalf("chtimes %s: %v", dir, err)
	}
}

func TestPruneNoCaptureRoot(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	stdout, stderr, err := runCgSplit("prune")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestPruneEmptyCaptureRoot(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	stdout, _, err := runCgSplit("prune")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestPruneKeepDefault(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	// Two runs; default keep is 50, so nothing should be evicted.
	seedRunDir(t, "AAAAAA", &Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	seedRunDir(t, "BBBBBB", &Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})

	stdout, _, err := runCgSplit("prune")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (nothing to prune)", stdout)
	}

	for _, id := range []string{"AAAAAA", "BBBBBB"} {
		if _, err := os.Stat(filepath.Join(CaptureRoot(), id)); err != nil {
			t.Errorf("run %s removed unexpectedly: %v", id, err)
		}
	}
}

func TestPruneKeepEvictsOldest(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirA := seedRunDir(t, "AAAAAA", &Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})
	dirC := seedRunDir(t, "CCCCCC", &Meta{ID: "CCCCCC", Command: []string{"echo", "c"}})

	chtimes(t, dirA, now)
	chtimes(t, dirB, now.Add(-1*time.Hour))
	chtimes(t, dirC, now.Add(-2*time.Hour))

	stdout, _, err := runCgSplit("prune", "--keep", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "BBBBBB\nCCCCCC\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if _, err := os.Stat(dirA); err != nil {
		t.Errorf("AAAAAA removed unexpectedly: %v", err)
	}
	if _, err := os.Stat(dirB); !os.IsNotExist(err) {
		t.Errorf("BBBBBB still exists: %v", err)
	}
	if _, err := os.Stat(dirC); !os.IsNotExist(err) {
		t.Errorf("CCCCCC still exists: %v", err)
	}
}

func TestPruneDryRun(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirA := seedRunDir(t, "AAAAAA", &Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})
	chtimes(t, dirA, now)
	chtimes(t, dirB, now.Add(-1*time.Hour))

	stdout, _, err := runCgSplit("prune", "--keep", "1", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "BBBBBB\n" {
		t.Errorf("stdout = %q, want %q", stdout, "BBBBBB\n")
	}
	if _, err := os.Stat(dirB); err != nil {
		t.Errorf("BBBBBB removed despite --dry-run: %v", err)
	}
}

func TestPruneOlderThan(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirA := seedRunDir(t, "AAAAAA", &Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})
	dirC := seedRunDir(t, "CCCCCC", &Meta{ID: "CCCCCC", Command: []string{"echo", "c"}})

	chtimes(t, dirA, now)
	chtimes(t, dirB, now.Add(-30*time.Minute))
	chtimes(t, dirC, now.Add(-2*time.Hour))

	stdout, _, err := runCgSplit("prune", "--older-than", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "CCCCCC\n" {
		t.Errorf("stdout = %q, want %q", stdout, "CCCCCC\n")
	}
	if _, err := os.Stat(dirA); err != nil {
		t.Errorf("AAAAAA removed unexpectedly: %v", err)
	}
	if _, err := os.Stat(dirB); err != nil {
		t.Errorf("BBBBBB removed unexpectedly: %v", err)
	}
	if _, err := os.Stat(dirC); !os.IsNotExist(err) {
		t.Errorf("CCCCCC still exists: %v", err)
	}
}

func TestPruneOlderThanDaySuffix(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirA := seedRunDir(t, "AAAAAA", &Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})

	chtimes(t, dirA, now)
	chtimes(t, dirB, now.Add(-8*24*time.Hour))

	stdout, _, err := runCgSplit("prune", "--older-than", "7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "BBBBBB\n" {
		t.Errorf("stdout = %q, want %q", stdout, "BBBBBB\n")
	}
	if _, err := os.Stat(dirA); err != nil {
		t.Errorf("AAAAAA removed unexpectedly: %v", err)
	}
}

func TestPruneSkipsNonRunEntries(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	// Stray non-Crockford directory: must survive.
	if err := os.MkdirAll(filepath.Join(root, "lowercase"), 0o755); err != nil {
		t.Fatalf("mkdir junk: %v", err)
	}
	// Stray plain file: must survive.
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	// Crockford-shaped name, but no meta.json (incomplete run): must survive.
	seedRunDir(t, "INCOMP", nil)

	// One valid run plus one valid-but-older run that should be evicted.
	now := time.Now()
	dirA := seedRunDir(t, "AAAAAA", &Meta{ID: "AAAAAA", Command: []string{"echo", "a"}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{ID: "BBBBBB", Command: []string{"echo", "b"}})
	chtimes(t, dirA, now)
	chtimes(t, dirB, now.Add(-1*time.Hour))

	stdout, _, err := runCgSplit("prune", "--keep", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "BBBBBB\n" {
		t.Errorf("stdout = %q, want %q", stdout, "BBBBBB\n")
	}

	for _, name := range []string{"lowercase", "notes.txt", "INCOMP", "AAAAAA"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s removed unexpectedly: %v", name, err)
		}
	}
	if _, err := os.Stat(dirB); !os.IsNotExist(err) {
		t.Errorf("BBBBBB still exists: %v", err)
	}
}

func TestPruneMutuallyExclusive(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	_, stderr, err := runCgSplit("prune", "--keep", "1", "--older-than", "1h")
	var exitErr *ExitError
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != 2 {
		t.Errorf("exit code = %d, want 2", exitErr.Code)
	}
	if !strings.Contains(stderr, "mutually exclusive") {
		t.Errorf("stderr = %q, want to contain 'mutually exclusive'", stderr)
	}
}

func TestPruneInvalidOlderThan(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	_, stderr, err := runCgSplit("prune", "--older-than", "garbage")
	var exitErr *ExitError
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != 2 {
		t.Errorf("exit code = %d, want 2", exitErr.Code)
	}
	if !strings.Contains(stderr, "invalid --older-than") {
		t.Errorf("stderr = %q, want to contain 'invalid --older-than'", stderr)
	}
}
