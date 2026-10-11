package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ripta/rt/pkg/cg/model"
)

// linkedCwd returns a directory, a symlink to it, and its canonical path. On macOS the directory
// itself is already reached through the /var symlink to /private/var.
func linkedCwd(t *testing.T) (dir, link, canonical string) {
	t.Helper()

	dir = t.TempDir()
	link = filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	canonical, err := model.CanonicalCwd(dir)
	if err != nil {
		t.Fatalf("CanonicalCwd: %v", err)
	}

	return dir, link, canonical
}

// eventually polls until lookup returns a value with the wanted count. The supervisor appends
// its history record after it writes the run's final state.
func eventually(t *testing.T, lookup func() *historyInfo, source string, count int) *historyInfo {
	t.Helper()

	var last *historyInfo
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		last = lookup()
		if last != nil && last.Source == source && last.Count == count {
			return last
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("history never reached source %q count %d, last %+v", source, count, last)

	return nil
}

func TestRunHistoryFoundThroughSymlinkedCwd(t *testing.T) {
	newStatsTestHistory(t)

	dir, link, canonical := linkedCwd(t)
	argv := []string{"echo", "e2e"}

	_, out, err := handleRun(context.Background(), nil, nil, nil, nil, "", runInput{Command: argv, Cwd: link})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0", out.ExitCode)
	}

	for _, cwd := range []string{link, dir, canonical} {
		eventually(t, func() *historyInfo { return expectedRun(argv, cwd) }, historySourceRun, 1)
	}

	h, err := model.OpenHistory()
	if err != nil {
		t.Fatalf("OpenHistory: %v", err)
	}

	query := model.NewStatsQuery(argv)
	query.Cwd = link

	result, err := h.QueryStats(query)
	if err != nil {
		t.Fatalf("QueryStats: %v", err)
	}
	if exact := result.ExactStandalone(); exact == nil || exact.Stats.Count != 1 {
		t.Errorf("exact = %+v, want one sample", exact)
	}

	_, out, err = handleRun(context.Background(), nil, nil, nil, nil, "", runInput{
		Command:         argv,
		Cwd:             link,
		WaitTimeoutAuto: true,
	})
	if err != nil {
		t.Fatalf("handleRun auto: %v", err)
	}
	if out.WaitTimeoutMsUsed != defaultWaitTimeoutMs {
		t.Errorf("WaitTimeoutMsUsed = %d, want the floor %d", out.WaitTimeoutMsUsed, defaultWaitTimeoutMs)
	}
}

func TestRunManyHistoryMatchesManifestShape(t *testing.T) {
	newStatsTestHistory(t)

	_, link, _ := linkedCwd(t)
	in := runManyInput{
		Commands:    [][]string{{"echo", "a"}, {"echo", "b"}},
		Repeat:      2,
		Parallelism: 8,
		Cwd:         link,
	}

	repeat, parallelism, err := validateRunMany(in)
	if err != nil {
		t.Fatalf("validateRunMany: %v", err)
	}
	if parallelism != 4 {
		t.Fatalf("parallelism = %d, want it clamped to the 4 runs", parallelism)
	}

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, nil, "", in)
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if out.TimedOut {
		t.Fatalf("TimedOut = true, want a finished pool")
	}

	m, err := model.ReadPoolManifest(filepath.Join(model.CaptureRoot(), out.ID))
	if err != nil {
		t.Fatalf("ReadPoolManifest: %v", err)
	}
	if m.Repeat != repeat || m.Parallelism != parallelism {
		t.Fatalf("manifest repeat %d parallelism %d, want %d and %d", m.Repeat, m.Parallelism, repeat, parallelism)
	}

	eventually(t, func() *historyInfo {
		return expectedPool(link, in.Commands, repeat, parallelism, time.Time{})
	}, historySourcePool, 1)

	// A different repeat is a new shape. Its members still ran pooled at this parallelism.
	eventually(t, func() *historyInfo {
		return expectedPool(link, in.Commands, repeat+1, parallelism, time.Time{})
	}, historySourceEstimate, 0)
}

func TestExpectedPoolEstimateIgnoresRunsFromTheQueriedPool(t *testing.T) {
	h := newStatsTestHistory(t)
	dir := canonicalTempDir(t)

	appendStatsTestRuns(t, h, []statsTestRun{
		{ID: "MEMBR1", Argv: []string{"echo", "a"}, Cwd: dir, Day: 0, DurationMs: 10, Parallelism: 2},
		{ID: "MEMBR2", Argv: []string{"echo", "b"}, Cwd: dir, Day: 0, DurationMs: 20, Parallelism: 2},
		{ID: "MEMBR3", Argv: []string{"echo", "a"}, Cwd: dir, Day: 5, DurationMs: 900, Parallelism: 2},
	})

	commands := [][]string{{"echo", "a"}, {"echo", "b"}}

	all := expectedPool(dir, commands, 1, 2, time.Time{})
	if all == nil || all.MaxMs != 900 {
		t.Fatalf("without a bound = %+v, want max 900", all)
	}

	earlier := expectedPool(dir, commands, 1, 2, statsTestBase.AddDate(0, 0, 3))
	if earlier == nil || earlier.Source != historySourceEstimate || earlier.MaxMs != 20 {
		t.Errorf("before the pool's start = %+v, want an estimate with max 20", earlier)
	}
}

func TestRunManyTimeoutEstimateIgnoresOwnMembers(t *testing.T) {
	h := newStatsTestHistory(t)
	dir := canonicalTempDir(t)

	fast := []string{"sh", "-c", "sleep 0.3"}
	slow := []string{"sleep", "2"}
	appendStatsTestRuns(t, h, []statsTestRun{
		{ID: "MEMBR1", Argv: fast, Cwd: dir, Day: 0, DurationMs: 10, Parallelism: 2},
		{ID: "MEMBR2", Argv: slow, Cwd: dir, Day: 0, DurationMs: 20, Parallelism: 2},
	})

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, nil, "", runManyInput{
		Commands:      [][]string{fast, slow},
		Parallelism:   2,
		Cwd:           dir,
		WaitTimeoutMs: 1000,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if !out.TimedOut {
		t.Fatalf("TimedOut = false, want the slow member still running")
	}

	want := historyInfo{Source: historySourceEstimate, MaxMs: 20}
	if out.History == nil || *out.History != want {
		t.Errorf("History = %+v, want %+v from earlier pools only", out.History, want)
	}

	waitForPoolFinished(t, filepath.Join(model.CaptureRoot(), out.ID))
}
