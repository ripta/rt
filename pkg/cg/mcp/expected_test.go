package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ripta/rt/pkg/cg/model"
)

func canonicalTempDir(t *testing.T) string {
	t.Helper()

	dir, err := model.CanonicalCwd(t.TempDir())
	if err != nil {
		t.Fatalf("CanonicalCwd: %v", err)
	}

	return dir
}

// poolTestSeq keeps appended pool IDs and start times distinct, because the log keeps only the
// first record per ID.
var poolTestSeq int

// appendPoolTestRecord appends a finished, all-succeeded pool record of the given shape.
func appendPoolTestRecord(t *testing.T, h *model.History, cwd string, commands [][]string, repeat, parallelism int, durationMs int64) {
	t.Helper()

	poolTestSeq++
	started := statsTestBase.Add(time.Duration(poolTestSeq) * time.Hour)
	finished := started.Add(time.Duration(durationMs) * time.Millisecond)
	exit := 0

	m := &model.PoolManifest{
		ID:          fmt.Sprintf("POOL%02d", poolTestSeq),
		Commands:    commands,
		Repeat:      repeat,
		Parallelism: parallelism,
		Cwd:         cwd,
		StartedAt:   started,
		FinishedAt:  &finished,
		Runs:        []model.PoolRunRecord{{Command: 0, RunID: "AAAAAA", Status: model.PoolRunFinished, ExitCode: &exit}},
	}

	rec, ok := h.NewPoolRecord(m)
	if !ok {
		t.Fatalf("NewPoolRecord: manifest not finished")
	}
	if err := h.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

func waitForRunFinished(t *testing.T, dir string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := model.ReadMeta(dir); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("meta.json never appeared in %s", dir)
}

type runAutoTimeoutTest struct {
	Name     string
	Argv     []string
	SeedArgv []string
	Seed     []int64
	Explicit int
	Want     int
}

var runAutoTimeoutTests = []runAutoTimeoutTest{
	{
		Name:     "explicit wins over auto",
		Argv:     []string{"echo", "auto"},
		SeedArgv: []string{"echo", "auto"},
		Seed:     []int64{500000},
		Explicit: 90000,
		Want:     90000,
	},
	{
		Name: "no history falls back to the default",
		Argv: []string{"echo", "auto"},
		Want: 60000,
	},
	{
		Name:     "prefix-only history does not count",
		Argv:     []string{"echo", "auto"},
		SeedArgv: []string{"echo", "auto", "more"},
		Seed:     []int64{500000},
		Want:     60000,
	},
	{
		Name:     "small max stays at the floor",
		Argv:     []string{"echo", "auto"},
		SeedArgv: []string{"echo", "auto"},
		Seed:     []int64{10000, 20000},
		Want:     60000,
	},
	{
		Name:     "large max gets headroom",
		Argv:     []string{"echo", "auto"},
		SeedArgv: []string{"echo", "auto"},
		Seed:     []int64{100000, 200000},
		Want:     300000,
	},
}

func TestHandleRunAutoTimeout(t *testing.T) {
	for _, test := range runAutoTimeoutTests {
		t.Run(test.Name, func(t *testing.T) {
			h := newStatsTestHistory(t)
			dir := canonicalTempDir(t)

			var runs []statsTestRun
			for i, d := range test.Seed {
				runs = append(runs, statsTestRun{
					ID:         "AAAAA" + string(rune('1'+i)),
					Argv:       test.SeedArgv,
					Cwd:        dir,
					Day:        i,
					DurationMs: d,
				})
			}
			appendStatsTestRuns(t, h, runs)

			_, out, err := handleRun(context.Background(), nil, nil, nil, "", runInput{
				Command:         test.Argv,
				Cwd:             dir,
				WaitTimeoutMs:   test.Explicit,
				WaitTimeoutAuto: true,
			})
			if err != nil {
				t.Fatalf("handleRun: %v", err)
			}
			if out.WaitTimeoutMsUsed != test.Want {
				t.Errorf("WaitTimeoutMsUsed = %d, want %d", out.WaitTimeoutMsUsed, test.Want)
			}
		})
	}
}

func TestHandleRunWithoutAutoKeepsDefault(t *testing.T) {
	h := newStatsTestHistory(t)
	dir := canonicalTempDir(t)
	appendStatsTestRuns(t, h, []statsTestRun{
		{ID: "AAAAA1", Argv: []string{"echo", "plain"}, Cwd: dir, DurationMs: 500000},
	})

	_, out, err := handleRun(context.Background(), nil, nil, nil, "", runInput{
		Command: []string{"echo", "plain"},
		Cwd:     dir,
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.WaitTimeoutMsUsed != defaultWaitTimeoutMs {
		t.Errorf("WaitTimeoutMsUsed = %d, want %d", out.WaitTimeoutMsUsed, defaultWaitTimeoutMs)
	}
}

func TestHandleRunTimeoutCarriesHistory(t *testing.T) {
	h := newStatsTestHistory(t)
	dir := canonicalTempDir(t)
	appendStatsTestRuns(t, h, []statsTestRun{
		{ID: "AAAAA1", Argv: []string{"sleep", "1"}, Cwd: dir, Day: 0, DurationMs: 1000},
		{ID: "AAAAA2", Argv: []string{"sleep", "1"}, Cwd: dir, Day: 1, DurationMs: 3000},
	})

	_, out, err := handleRun(context.Background(), nil, nil, nil, "", runInput{
		Command:       []string{"sleep", "1"},
		Cwd:           dir,
		WaitTimeoutMs: 150,
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !out.TimedOut {
		t.Fatalf("TimedOut = false, want true")
	}
	if out.ElapsedMs == nil || *out.ElapsedMs < 100 {
		t.Errorf("ElapsedMs = %v, want at least the wait", out.ElapsedMs)
	}
	if out.History == nil || out.History.Source != historySourceRun || out.History.Count != 2 || out.History.MaxMs != 3000 {
		t.Fatalf("History = %+v, want run source, count 2, max 3000", out.History)
	}
	if out.History.P50Ms == nil || out.History.P90Ms == nil {
		t.Errorf("History = %+v, want p50 and p90", out.History)
	}

	waitForRunFinished(t, filepath.Join(model.CaptureRoot(), out.ID))
}

func TestHandleRunTimeoutWithoutHistoryOmitsIt(t *testing.T) {
	newStatsTestHistory(t)

	_, out, err := handleRun(context.Background(), nil, nil, nil, "", runInput{
		Command:       []string{"sleep", "1"},
		Cwd:           canonicalTempDir(t),
		WaitTimeoutMs: 150,
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !out.TimedOut {
		t.Fatalf("TimedOut = false, want true")
	}
	if out.ElapsedMs == nil {
		t.Errorf("ElapsedMs = nil, want a value")
	}
	if out.History != nil {
		t.Errorf("History = %+v, want nil", out.History)
	}

	waitForRunFinished(t, filepath.Join(model.CaptureRoot(), out.ID))
}

var poolTestCommands = [][]string{{"echo", "a"}, {"echo", "b"}}

type poolAutoTimeoutTest struct {
	Name     string
	Explicit int
	PoolWall []int64
	Members  map[string]int64
	Want     int
}

var poolAutoTimeoutTests = []poolAutoTimeoutTest{
	{
		Name:     "explicit wins over auto",
		Explicit: 90000,
		PoolWall: []int64{400000},
		Want:     90000,
	},
	{
		Name: "no history falls back to the default",
		Want: 60000,
	},
	{
		Name:     "pool record wins over the estimate",
		PoolWall: []int64{100000, 200000},
		Members:  map[string]int64{"a": 90000, "b": 90000},
		Want:     300000,
	},
	{
		// 2 commands x repeat 3 / parallelism 2 is 3 batches. 3 x 50000 is 150000, and the
		// headroom makes it 225000.
		Name:    "estimate is batches times the longest member",
		Members: map[string]int64{"a": 40000, "b": 50000},
		Want:    225000,
	},
	{
		Name:    "estimate missing a command falls back to the default",
		Members: map[string]int64{"a": 400000},
		Want:    60000,
	},
	{
		Name:     "small pool record stays at the floor",
		PoolWall: []int64{5000},
		Want:     60000,
	},
}

func seedPoolAutoHistory(t *testing.T, h *model.History, dir string, test poolAutoTimeoutTest) {
	t.Helper()

	for _, d := range test.PoolWall {
		appendPoolTestRecord(t, h, dir, poolTestCommands, 3, 2, d)
	}

	var runs []statsTestRun
	i := 0
	for name, d := range test.Members {
		i++
		runs = append(runs, statsTestRun{
			ID:          "MEMBR" + string(rune('0'+i)),
			Argv:        []string{"echo", name},
			Cwd:         dir,
			Day:         i,
			DurationMs:  d,
			Parallelism: 2,
		})
	}
	appendStatsTestRuns(t, h, runs)
}

func TestRunManyAutoTimeout(t *testing.T) {
	for _, test := range poolAutoTimeoutTests {
		t.Run(test.Name, func(t *testing.T) {
			h := newStatsTestHistory(t)
			dir := canonicalTempDir(t)
			seedPoolAutoHistory(t, h, dir, test)

			_, out, err := handleRunMany(context.Background(), nil, nil, nil, "", runManyInput{
				Commands:        poolTestCommands,
				Repeat:          3,
				Parallelism:     2,
				Cwd:             dir,
				WaitTimeoutMs:   test.Explicit,
				WaitTimeoutAuto: true,
			})
			if err != nil {
				t.Fatalf("handleRunMany: %v", err)
			}
			if out.WaitTimeoutMsUsed != test.Want {
				t.Errorf("WaitTimeoutMsUsed = %d, want %d", out.WaitTimeoutMsUsed, test.Want)
			}
			if out.TimedOut {
				t.Errorf("TimedOut = true, want a finished pool")
			}
		})
	}
}

func TestRunManyAutoTimeoutUsesClampedParallelism(t *testing.T) {
	h := newStatsTestHistory(t)
	dir := canonicalTempDir(t)

	// The pool has 2 runs, so a requested parallelism of 8 is recorded as 2.
	appendPoolTestRecord(t, h, dir, poolTestCommands, 1, 2, 100000)

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, "", runManyInput{
		Commands:        poolTestCommands,
		Parallelism:     8,
		Cwd:             dir,
		WaitTimeoutAuto: true,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if out.WaitTimeoutMsUsed != 150000 {
		t.Errorf("WaitTimeoutMsUsed = %d, want 150000", out.WaitTimeoutMsUsed)
	}
}

func TestRunManyTimeoutCarriesHistory(t *testing.T) {
	h := newStatsTestHistory(t)
	dir := canonicalTempDir(t)

	sleepCommands := [][]string{{"sleep", "1"}}
	appendPoolTestRecord(t, h, dir, sleepCommands, 1, 1, 7000)

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, "", runManyInput{
		Commands:      sleepCommands,
		Cwd:           dir,
		WaitTimeoutMs: 150,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if !out.TimedOut {
		t.Fatalf("TimedOut = false, want true")
	}
	if out.ElapsedMs == nil || *out.ElapsedMs < 100 {
		t.Errorf("ElapsedMs = %v, want at least the wait", out.ElapsedMs)
	}
	if out.History == nil || out.History.Source != historySourcePool || out.History.Count != 1 || out.History.MaxMs != 7000 {
		t.Errorf("History = %+v, want pool source, count 1, max 7000", out.History)
	}

	waitForPoolFinished(t, filepath.Join(model.CaptureRoot(), out.ID))
}

func TestRunManyTimeoutEstimateCarriesOnlyMax(t *testing.T) {
	h := newStatsTestHistory(t)
	dir := canonicalTempDir(t)

	appendStatsTestRuns(t, h, []statsTestRun{
		{ID: "MEMBR1", Argv: []string{"sleep", "1"}, Cwd: dir, DurationMs: 4000, Parallelism: 1},
	})

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, "", runManyInput{
		Commands:      [][]string{{"sleep", "1"}},
		Repeat:        2,
		Cwd:           dir,
		WaitTimeoutMs: 150,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if !out.TimedOut {
		t.Fatalf("TimedOut = false, want true")
	}

	want := historyInfo{Source: historySourceEstimate, MaxMs: 8000}
	if out.History == nil || *out.History != want {
		t.Errorf("History = %+v, want %+v", out.History, want)
	}

	waitForPoolFinished(t, filepath.Join(model.CaptureRoot(), out.ID))
}

func TestRunManyTimeoutWithoutHistoryOmitsIt(t *testing.T) {
	newStatsTestHistory(t)

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, "", runManyInput{
		Commands:      [][]string{{"sleep", "1"}},
		Cwd:           canonicalTempDir(t),
		WaitTimeoutMs: 150,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if out.ElapsedMs == nil {
		t.Errorf("ElapsedMs = nil, want a value")
	}
	if out.History != nil {
		t.Errorf("History = %+v, want nil", out.History)
	}

	waitForPoolFinished(t, filepath.Join(model.CaptureRoot(), out.ID))
}

func TestRunManyFinishedCarriesNoTimedOutFields(t *testing.T) {
	newStatsTestHistory(t)

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, "", runManyInput{
		Commands: [][]string{{"echo", "done"}},
		Cwd:      canonicalTempDir(t),
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if out.ElapsedMs != nil || out.History != nil {
		t.Errorf("elapsed/history = %v/%+v, want none on a finished pool", out.ElapsedMs, out.History)
	}
}

func TestHandleWaitRunTimeoutCarriesHistory(t *testing.T) {
	h := newStatsTestHistory(t)
	dir := canonicalTempDir(t)
	appendStatsTestRuns(t, h, []statsTestRun{
		{ID: "AAAAA1", Argv: []string{"make", "test"}, Cwd: dir, DurationMs: 9000},
	})

	runDir := seedRunDir(t, "BBBBBB", nil)
	info := &model.StartInfo{RunInfo: model.RunInfo{
		ID:        "BBBBBB",
		Command:   []string{"make", "test"},
		Cwd:       dir,
		StartedAt: time.Now().Add(-10 * time.Second),
	}}
	if err := model.WriteStartInfo(runDir, info); err != nil {
		t.Fatalf("WriteStartInfo: %v", err)
	}

	_, out, err := handleWait(context.Background(), nil, newRunRegistry(), waitInput{ID: "BBBBBB", TimeoutMs: 100})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if out.Finished {
		t.Fatalf("Finished = true, want false")
	}
	if out.ElapsedMs == nil || *out.ElapsedMs < 10000 {
		t.Errorf("ElapsedMs = %v, want at least 10000 from the recorded start", out.ElapsedMs)
	}
	if out.History == nil || out.History.Source != historySourceRun || out.History.MaxMs != 9000 {
		t.Errorf("History = %+v, want run source with max 9000", out.History)
	}
}

func TestHandleWaitRunTimeoutWithoutStartInfo(t *testing.T) {
	newStatsTestHistory(t)
	seedRunDir(t, "BBBBBB", nil)

	_, out, err := handleWait(context.Background(), nil, newRunRegistry(), waitInput{ID: "BBBBBB", TimeoutMs: 100})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if out.ElapsedMs == nil {
		t.Errorf("ElapsedMs = nil, want the wait's own elapsed time")
	}
	if out.History != nil {
		t.Errorf("History = %+v, want nil", out.History)
	}
}

func TestHandleWaitPoolTimeoutCarriesHistory(t *testing.T) {
	h := newStatsTestHistory(t)
	dir := canonicalTempDir(t)
	appendPoolTestRecord(t, h, dir, poolTestCommands, 3, 2, 12000)

	m := runningPoolManifest("BBBBBB")
	m.Commands = poolTestCommands
	m.Repeat = 3
	m.Parallelism = 2
	m.Cwd = dir
	m.StartedAt = time.Now().Add(-20 * time.Second)
	seedPoolDir(t, "BBBBBB", m)

	_, out, err := handleWait(context.Background(), nil, newRunRegistry(), waitInput{ID: "BBBBBB", TimeoutMs: 100})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if out.Finished {
		t.Fatalf("Finished = true, want false")
	}
	if out.ElapsedMs == nil || *out.ElapsedMs < 20000 {
		t.Errorf("ElapsedMs = %v, want at least 20000 from the recorded start", out.ElapsedMs)
	}
	if out.History == nil || out.History.Source != historySourcePool || out.History.MaxMs != 12000 {
		t.Errorf("History = %+v, want pool source with max 12000", out.History)
	}
}

func TestHandleWaitPoolTimeoutWithoutHistoryOmitsIt(t *testing.T) {
	newStatsTestHistory(t)

	m := runningPoolManifest("BBBBBB")
	m.Cwd = canonicalTempDir(t)
	m.Repeat = 1
	m.Parallelism = 1
	m.StartedAt = time.Now().Add(-5 * time.Second)
	seedPoolDir(t, "BBBBBB", m)

	_, out, err := handleWait(context.Background(), nil, newRunRegistry(), waitInput{ID: "BBBBBB", TimeoutMs: 100})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if out.ElapsedMs == nil || *out.ElapsedMs < 5000 {
		t.Errorf("ElapsedMs = %v, want at least 5000", out.ElapsedMs)
	}
	if out.History != nil {
		t.Errorf("History = %+v, want nil", out.History)
	}
}
