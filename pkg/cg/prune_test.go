package cg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type parseDurationTest struct {
	name    string
	in      string
	want    time.Duration
	wantErr bool
}

var parseDurationTests = []parseDurationTest{
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

	for _, tt := range parseDurationTests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePruneDuration(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParsePruneDuration(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePruneDuration(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParsePruneDuration(%q) = %v, want %v", tt.in, got, tt.want)
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
	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "a"}}})
	seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "b"}}})

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
	dirA := seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "a"}}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "b"}}})
	dirC := seedRunDir(t, "CCCCCC", &Meta{RunInfo: RunInfo{ID: "CCCCCC", Command: []string{"echo", "c"}}})

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
	dirA := seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "a"}}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "b"}}})
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
	dirA := seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "a"}}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "b"}}})
	dirC := seedRunDir(t, "CCCCCC", &Meta{RunInfo: RunInfo{ID: "CCCCCC", Command: []string{"echo", "c"}}})

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
	dirA := seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "a"}}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "b"}}})

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
	dirA := seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "a"}}})
	dirB := seedRunDir(t, "BBBBBB", &Meta{RunInfo: RunInfo{ID: "BBBBBB", Command: []string{"echo", "b"}}})
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

func TestPruneEvictsAbandonedRuns(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirFin := seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "a"}}})

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

	// Shell-path run: no lock file at all.
	dirShell := seedRunDir(t, "EEEEEE", nil)

	chtimes(t, dirFin, now)
	chtimes(t, dirAband, now.Add(-1*time.Hour))
	chtimes(t, dirHeld, now.Add(-2*time.Hour))
	chtimes(t, dirShell, now.Add(-3*time.Hour))

	stdout, _, err := runCgSplit("prune", "--keep", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "ABANDN\n" {
		t.Errorf("stdout = %q, want %q", stdout, "ABANDN\n")
	}

	if _, err := os.Stat(dirAband); !os.IsNotExist(err) {
		t.Errorf("ABANDN still exists: %v", err)
	}
	for _, dir := range []string{dirFin, dirHeld, dirShell} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s removed unexpectedly: %v", dir, err)
		}
	}
}

func TestPruneOlderThanEvictsAbandonedRuns(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirAband := seedRunDir(t, "ABANDN", nil)
	lock, err := acquireRunLock(dirAband)
	if err != nil {
		t.Fatalf("acquiring lock: %v", err)
	}
	lock.Close()

	dirHeld := seedRunDir(t, "DDDDDD", nil)
	held, err := acquireRunLock(dirHeld)
	if err != nil {
		t.Fatalf("holding lock: %v", err)
	}
	defer held.Close()

	chtimes(t, dirAband, now.Add(-2*time.Hour))
	chtimes(t, dirHeld, now.Add(-2*time.Hour))

	stdout, _, err := runCgSplit("prune", "--older-than", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "ABANDN\n" {
		t.Errorf("stdout = %q, want %q", stdout, "ABANDN\n")
	}

	if _, err := os.Stat(dirAband); !os.IsNotExist(err) {
		t.Errorf("ABANDN still exists: %v", err)
	}
	if _, err := os.Stat(dirHeld); err != nil {
		t.Errorf("DDDDDD removed unexpectedly: %v", err)
	}
}

// seedPoolWithMembers seeds a finished pool and two finished member run dirs
// whose IDs the manifest names.
func seedPoolWithMembers(t *testing.T, poolID, okID, badID string) string {
	t.Helper()
	finished := time.Now().UTC()
	dir := seedPoolDir(t, poolID, &PoolManifest{
		ID:         poolID,
		Commands:   [][]string{{"echo", "hi"}},
		StartedAt:  finished.Add(-time.Minute),
		FinishedAt: &finished,
		Runs: []PoolRunRecord{
			{Command: 0, RunID: okID, Status: PoolRunFinished, ExitCode: intp(0)},
			{Command: 0, RunID: badID, Status: PoolRunFinished, ExitCode: intp(1)},
		},
	})
	seedRunDir(t, okID, &Meta{RunInfo: RunInfo{ID: okID, Command: []string{"echo", "hi"}, Pool: poolID}})
	seedRunDir(t, badID, &Meta{RunInfo: RunInfo{ID: badID, Command: []string{"echo", "hi"}, Pool: poolID}, ExitCode: 1})
	return dir
}

func TestPruneEvictsPoolAsUnit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirPool := seedPoolWithMembers(t, "PPPPPP", "AAAAAA", "BBBBBB")
	dirSolo := seedRunDir(t, "SSSSSS", &Meta{RunInfo: RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})
	chtimes(t, dirSolo, now)
	chtimes(t, dirPool, now.Add(-1*time.Hour))

	stdout, _, err := runCgSplit("prune", "--keep", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "PPPPPP\nAAAAAA\nBBBBBB\n" {
		t.Errorf("stdout = %q, want pool then members", stdout)
	}

	for _, id := range []string{"PPPPPP", "AAAAAA", "BBBBBB"} {
		if _, err := os.Stat(filepath.Join(root, id)); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", id, err)
		}
	}
	if _, err := os.Stat(dirSolo); err != nil {
		t.Errorf("SSSSSS removed unexpectedly: %v", err)
	}
}

func TestPruneSkipsLivePoolAndMembers(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()

	// A live pool: manifest without finished_at, lock held. Its finished member
	// must not be evicted from under it, however old.
	dirPool := seedPoolDir(t, "PPPPPP", &PoolManifest{
		ID:        "PPPPPP",
		Commands:  [][]string{{"echo", "hi"}},
		StartedAt: now.UTC(),
		Runs: []PoolRunRecord{
			{Command: 0, RunID: "AAAAAA", Status: PoolRunFinished, ExitCode: intp(0)},
			{Command: 0, RunID: "BBBBBB", Status: PoolRunRunning},
		},
	})
	lock, err := acquireRunLock(dirPool)
	if err != nil {
		t.Fatalf("holding pool lock: %v", err)
	}
	defer lock.Close()

	dirMember := seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, Pool: "PPPPPP"}})
	dirSolo := seedRunDir(t, "SSSSSS", &Meta{RunInfo: RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})
	dirOld := seedRunDir(t, "CCCCCC", &Meta{RunInfo: RunInfo{ID: "CCCCCC", Command: []string{"echo", "old"}}})

	chtimes(t, dirSolo, now)
	chtimes(t, dirPool, now.Add(-1*time.Hour))
	chtimes(t, dirMember, now.Add(-2*time.Hour))
	chtimes(t, dirOld, now.Add(-3*time.Hour))

	stdout, _, err := runCgSplit("prune", "--keep", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "CCCCCC\n" {
		t.Errorf("stdout = %q, want just CCCCCC", stdout)
	}

	for _, dir := range []string{dirPool, dirMember, dirSolo} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s removed unexpectedly: %v", dir, err)
		}
	}
}

func TestPruneEvictsAbandonedPool(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()

	// Abandoned: no finished_at, lock file released. The manifest still names a
	// running member; a held member lock keeps that member alive as an orphan.
	dirPool := seedPoolDir(t, "PPPPPP", &PoolManifest{
		ID:        "PPPPPP",
		Commands:  [][]string{{"echo", "hi"}},
		StartedAt: now.UTC(),
		Runs: []PoolRunRecord{
			{Command: 0, RunID: "AAAAAA", Status: PoolRunFinished, ExitCode: intp(0)},
			{Command: 0, RunID: "BBBBBB", Status: PoolRunRunning},
		},
	})
	poolLock, err := acquireRunLock(dirPool)
	if err != nil {
		t.Fatalf("acquiring pool lock: %v", err)
	}
	poolLock.Close()

	dirDone := seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, Pool: "PPPPPP"}})
	dirLive := seedRunDir(t, "BBBBBB", nil)
	memberLock, err := acquireRunLock(dirLive)
	if err != nil {
		t.Fatalf("holding member lock: %v", err)
	}
	defer memberLock.Close()

	dirSolo := seedRunDir(t, "SSSSSS", &Meta{RunInfo: RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})
	chtimes(t, dirSolo, now)
	chtimes(t, dirPool, now.Add(-1*time.Hour))
	chtimes(t, dirDone, now.Add(-1*time.Hour))
	chtimes(t, dirLive, now.Add(-1*time.Hour))

	stdout, _, err := runCgSplit("prune", "--keep", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "PPPPPP\nAAAAAA\n" {
		t.Errorf("stdout = %q, want pool and finished member only", stdout)
	}

	if _, err := os.Stat(dirLive); err != nil {
		t.Errorf("live member BBBBBB removed from under abandoned pool: %v", err)
	}
	for _, dir := range []string{dirPool, dirDone} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", dir, err)
		}
	}
}

func TestPruneOrphanMemberEvictable(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()

	// The member's meta names a pool whose dir no longer exists: an orphan,
	// individually evictable like any run.
	dirOrphan := seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}, Pool: "GGGGGG"}})
	dirSolo := seedRunDir(t, "SSSSSS", &Meta{RunInfo: RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})
	chtimes(t, dirSolo, now)
	chtimes(t, dirOrphan, now.Add(-1*time.Hour))

	stdout, _, err := runCgSplit("prune", "--keep", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "AAAAAA\n" {
		t.Errorf("stdout = %q, want %q", stdout, "AAAAAA\n")
	}
}

func TestPruneToleratesMissingMemberDirs(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	finished := now.UTC()
	dirPool := seedPoolDir(t, "PPPPPP", &PoolManifest{
		ID:         "PPPPPP",
		Commands:   [][]string{{"echo", "hi"}},
		StartedAt:  finished.Add(-time.Minute),
		FinishedAt: &finished,
		Runs:       []PoolRunRecord{{Command: 0, RunID: "AAAAAA", Status: PoolRunFinished, ExitCode: intp(0)}},
	})
	dirSolo := seedRunDir(t, "SSSSSS", &Meta{RunInfo: RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})
	chtimes(t, dirSolo, now)
	chtimes(t, dirPool, now.Add(-1*time.Hour))

	stdout, _, err := runCgSplit("prune", "--keep", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "PPPPPP\n" {
		t.Errorf("stdout = %q, want just the pool", stdout)
	}
	if _, err := os.Stat(dirPool); !os.IsNotExist(err) {
		t.Errorf("PPPPPP still exists: %v", err)
	}
}

func TestPrunePoolCountsOnceAgainstKeep(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirPool := seedPoolWithMembers(t, "PPPPPP", "AAAAAA", "BBBBBB")
	dirSolo := seedRunDir(t, "SSSSSS", &Meta{RunInfo: RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})
	chtimes(t, dirSolo, now)
	chtimes(t, dirPool, now.Add(-1*time.Hour))

	// Four directories, but only two candidates: the standalone run and the
	// pool unit. keep=2 retains both.
	stdout, _, err := runCgSplit("prune", "--keep", "2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestPruneDryRunPool(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirPool := seedPoolWithMembers(t, "PPPPPP", "AAAAAA", "BBBBBB")
	dirSolo := seedRunDir(t, "SSSSSS", &Meta{RunInfo: RunInfo{ID: "SSSSSS", Command: []string{"echo", "solo"}}})
	chtimes(t, dirSolo, now)
	chtimes(t, dirPool, now.Add(-1*time.Hour))

	stdout, _, err := runCgSplit("prune", "--keep", "1", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "PPPPPP\nAAAAAA\nBBBBBB\n" {
		t.Errorf("stdout = %q, want the whole unit previewed", stdout)
	}

	for _, id := range []string{"PPPPPP", "AAAAAA", "BBBBBB"} {
		if _, err := os.Stat(filepath.Join(root, id)); err != nil {
			t.Errorf("%s removed despite --dry-run: %v", id, err)
		}
	}
}

func TestPruneOlderThanEvictsPoolUnit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := CaptureRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	now := time.Now()
	dirPool := seedPoolWithMembers(t, "PPPPPP", "AAAAAA", "BBBBBB")
	chtimes(t, dirPool, now.Add(-2*time.Hour))

	stdout, _, err := runCgSplit("prune", "--older-than", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "PPPPPP\nAAAAAA\nBBBBBB\n" {
		t.Errorf("stdout = %q, want the whole unit", stdout)
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
