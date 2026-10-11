package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// statsFixtureCwd exists on every machine and resolves to itself, so golden output is stable.
const statsFixtureCwd = "/"

var statsFixtureBase = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

var statsFixtureKey = bytes.Repeat([]byte{0x42}, historyKeyBytes)

// newFixtureHistory points the state and capture directories at fresh temp dirs and opens a
// history under a fixed key. The same key makes MACs, and so the log, reproducible.
func newFixtureHistory(t *testing.T) *History {
	t.Helper()

	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("TMPDIR", t.TempDir())

	dir := filepath.Join(state, "cg")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating history dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, HistoryKeyFilename), statsFixtureKey, 0o600); err != nil {
		t.Fatalf("writing key: %v", err)
	}

	h, err := OpenHistory()
	if err != nil {
		t.Fatalf("OpenHistory: %v", err)
	}

	return h
}

type fixtureRun struct {
	ID          string
	Argv        string
	Cwd         string
	Day         int
	DurationMs  int64
	ExitCode    int
	Signal      *int
	Parallelism int
}

func (r fixtureRun) append(t *testing.T, h *History) {
	t.Helper()

	cwd := r.Cwd
	if cwd == "" {
		cwd = statsFixtureCwd
	}

	meta := &Meta{
		RunInfo: RunInfo{
			ID:        r.ID,
			Command:   strings.Fields(r.Argv),
			Cwd:       cwd,
			StartedAt: statsFixtureBase.AddDate(0, 0, r.Day),
		},
		DurationMs: r.DurationMs,
		ExitCode:   r.ExitCode,
		Signal:     r.Signal,
	}
	if r.Parallelism > 0 {
		meta.Pool = "POOLID"
	}

	if err := h.Append(h.NewRunRecord(meta, r.Parallelism)); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

func appendFixtureRuns(t *testing.T, h *History, runs []fixtureRun) {
	t.Helper()

	for _, r := range runs {
		r.append(t, h)
	}
}

// statsFixtureRuns is the log behind the golden output. The queried command is
// `make test JOBS=10 FOO=file`.
var statsFixtureRuns = []fixtureRun{
	{ID: "AAAAA1", Argv: "make test JOBS=10 FOO=file", Day: 0, DurationMs: 100000},
	{ID: "AAAAA2", Argv: "make test JOBS=10 FOO=file", Day: 1, DurationMs: 200000},
	{ID: "AAAAA3", Argv: "make test JOBS=10 FOO=file", Day: 2, DurationMs: 300000},
	{ID: "AAAAA4", Argv: "make test JOBS=10 FOO=file EXTRA=1", Day: 3, DurationMs: 400000},
	{ID: "AAAAA5", Argv: "make test JOBS=10 FOO=file", Day: 2, DurationMs: 500000, Parallelism: 4},
	{ID: "AAAAA6", Argv: "make test JOBS=10 FOO=file", Day: 3, DurationMs: 600000, Parallelism: 4},
	{ID: "AAAAA7", Argv: "make test JOBS=10", Day: 0, DurationMs: 50000},
	{ID: "AAAAA8", Argv: "make test JOBS=10", Day: 1, DurationMs: 60000},
	{ID: "AAAAA9", Argv: "make test", Day: 0, DurationMs: 20000},
	{ID: "AAAAB1", Argv: "make test", Day: 1, DurationMs: 30000, Parallelism: 2},
	{ID: "AAAAB2", Argv: "make test", Day: 2, DurationMs: 35000, Parallelism: 4},
	{ID: "AAAAB3", Argv: "make", Day: 0, DurationMs: 5000},
	{ID: "AAAAB4", Argv: "go test", Day: 0, DurationMs: 7000},

	// None of these count under the default filters.
	{ID: "AAAAB5", Argv: "make test JOBS=10 FOO=file", Day: 4, DurationMs: 1000, ExitCode: 1},
	{ID: "AAAAB6", Argv: "make test JOBS=10 FOO=file", Day: 4, DurationMs: 2000, Signal: intp(9)},
	{ID: "AAAAB7", Argv: "make test JOBS=10 FOO=file", Day: 4, DurationMs: 3000, Cwd: "/tmp"},
}

func fixtureQuery(argv string) StatsQuery {
	q := NewStatsQuery(strings.Fields(argv))
	q.Cwd = statsFixtureCwd

	return q
}

func queryStats(t *testing.T, h *History, q StatsQuery) *StatsResult {
	t.Helper()

	result, err := h.QueryStats(q)
	if err != nil {
		t.Fatalf("QueryStats: %v", err)
	}

	return result
}

type matchShape struct {
	Type        string
	Prefix      string
	Pool        bool
	Parallelism int
	Count       int
}

func shapesOf(result *StatsResult) []matchShape {
	shapes := make([]matchShape, len(result.Matches))
	for i, m := range result.Matches {
		command := m.Command
		if m.MatchType == StatsMatchPrefix {
			command = m.CommandPrefix
		}
		shapes[i] = matchShape{m.MatchType, strings.Join(command, " "), m.Pool, m.Parallelism, m.Stats.Count}
	}

	return shapes
}

type queryShapeTest struct {
	Name string
	Pool string
	Want []matchShape
}

var queryShapeTests = []queryShapeTest{
	{
		Name: "any splits each level and orders standalone first",
		Pool: StatsPoolAny,
		Want: []matchShape{
			{StatsMatchExact, "make test JOBS=10 FOO=file", false, 0, 3},
			{StatsMatchExact, "make test JOBS=10 FOO=file", true, 4, 2},
			{StatsMatchPrefix, "make test JOBS=10", false, 0, 6},
			{StatsMatchPrefix, "make test JOBS=10", true, 4, 2},
			{StatsMatchPrefix, "make test", false, 0, 7},
			{StatsMatchPrefix, "make test", true, 2, 1},
			{StatsMatchPrefix, "make test", true, 4, 3},
			{StatsMatchPrefix, "make", false, 0, 8},
			{StatsMatchPrefix, "make", true, 2, 1},
			{StatsMatchPrefix, "make", true, 4, 3},
		},
	},
	{
		Name: "none keeps standalone runs",
		Pool: StatsPoolNone,
		Want: []matchShape{
			{StatsMatchExact, "make test JOBS=10 FOO=file", false, 0, 3},
			{StatsMatchPrefix, "make test JOBS=10", false, 0, 6},
			{StatsMatchPrefix, "make test", false, 0, 7},
			{StatsMatchPrefix, "make", false, 0, 8},
		},
	},
	{
		Name: "only keeps pooled runs",
		Pool: StatsPoolOnly,
		Want: []matchShape{
			{StatsMatchExact, "make test JOBS=10 FOO=file", true, 4, 2},
			{StatsMatchPrefix, "make test JOBS=10", true, 4, 2},
			{StatsMatchPrefix, "make test", true, 2, 1},
			{StatsMatchPrefix, "make test", true, 4, 3},
			{StatsMatchPrefix, "make", true, 2, 1},
			{StatsMatchPrefix, "make", true, 4, 3},
		},
	},
}

func TestQueryStatsShapes(t *testing.T) {
	h := newFixtureHistory(t)
	appendFixtureRuns(t, h, statsFixtureRuns)

	for _, test := range queryShapeTests {
		t.Run(test.Name, func(t *testing.T) {
			q := fixtureQuery("make test JOBS=10 FOO=file")
			q.Pool = test.Pool

			got := shapesOf(queryStats(t, h, q))
			if !slices.Equal(got, test.Want) {
				t.Errorf("matches differ\n got: %+v\nwant: %+v", got, test.Want)
			}
		})
	}
}

func TestQueryStatsValues(t *testing.T) {
	h := newFixtureHistory(t)
	appendFixtureRuns(t, h, statsFixtureRuns)

	q := fixtureQuery("make test JOBS=10 FOO=file")
	q.Pool = StatsPoolNone

	exact := queryStats(t, h, q).ExactStandalone()
	if exact == nil {
		t.Fatal("no exact standalone entry")
	}

	wantLastAt := statsFixtureBase.AddDate(0, 0, 2)
	want := DurationStats{
		Count: 3, MinMs: 100000, MaxMs: 300000, AvgMs: 200000, P50Ms: 200000, P90Ms: 300000,
		LastMs: 300000, LastAt: &wantLastAt,
	}
	if exact.Stats.LastAt == nil || !exact.Stats.LastAt.Equal(wantLastAt) {
		t.Fatalf("last_at = %v, want %v", exact.Stats.LastAt, wantLastAt)
	}
	got := exact.Stats
	got.LastAt, want.LastAt = nil, nil
	if got != want {
		t.Errorf("stats = %+v, want %+v", got, want)
	}
}

func TestQueryStatsExactEntryWithNoSamples(t *testing.T) {
	h := newFixtureHistory(t)
	appendFixtureRuns(t, h, statsFixtureRuns)

	t.Run("standalone shaped by default", func(t *testing.T) {
		result := queryStats(t, h, fixtureQuery("cargo build"))

		want := []matchShape{{StatsMatchExact, "cargo build", false, 0, 0}}
		if got := shapesOf(result); !slices.Equal(got, want) {
			t.Errorf("matches = %+v, want %+v", got, want)
		}
		if result.Matches[0].Stats.LastAt != nil {
			t.Errorf("last_at = %v, want null", result.Matches[0].Stats.LastAt)
		}
	})

	t.Run("pooled shaped under only", func(t *testing.T) {
		q := fixtureQuery("cargo build")
		q.Pool = StatsPoolOnly

		want := []matchShape{{StatsMatchExact, "cargo build", true, 0, 0}}
		if got := shapesOf(queryStats(t, h, q)); !slices.Equal(got, want) {
			t.Errorf("matches = %+v, want %+v", got, want)
		}
	})

	t.Run("exact stays when only prefixes have samples", func(t *testing.T) {
		got := shapesOf(queryStats(t, h, fixtureQuery("go test ./...")))
		if len(got) < 2 || got[0] != (matchShape{StatsMatchExact, "go test ./...", false, 0, 0}) {
			t.Fatalf("matches = %+v, want a count-0 exact entry first", got)
		}
		if got[1] != (matchShape{StatsMatchPrefix, "go test", false, 0, 1}) {
			t.Errorf("second entry = %+v, want the go test prefix", got[1])
		}
	})
}

func TestQueryStatsFilters(t *testing.T) {
	h := newFixtureHistory(t)
	appendFixtureRuns(t, h, statsFixtureRuns)

	exact := func(t *testing.T, q StatsQuery) DurationStats {
		t.Helper()

		q.Pool = StatsPoolNone

		m := queryStats(t, h, q).ExactStandalone()
		if m == nil {
			t.Fatal("no exact standalone entry")
		}

		return m.Stats
	}

	t.Run("signaled runs are excluded even when the exit code filter matches", func(t *testing.T) {
		q := fixtureQuery("make test JOBS=10 FOO=file")
		q.ExitCode, _ = ParseExitCodeFilter(">=0")

		// The exit 1 run counts and the signaled run, with exit code 0, does not.
		if got := exact(t, q).Count; got != 4 {
			t.Errorf("count = %d, want 4", got)
		}
	})

	t.Run("exit code filter widens", func(t *testing.T) {
		q := fixtureQuery("make test JOBS=10 FOO=file")
		q.ExitCode, _ = ParseExitCodeFilter("1")

		if got := exact(t, q).Count; got != 1 {
			t.Errorf("count = %d, want 1", got)
		}
	})

	t.Run("since drops older runs", func(t *testing.T) {
		q := fixtureQuery("make test JOBS=10 FOO=file")
		since := statsFixtureBase.AddDate(0, 0, 2)
		q.Since = &since

		if got := exact(t, q).Count; got != 1 {
			t.Errorf("count = %d, want 1", got)
		}
	})

	t.Run("cwd selects its own runs", func(t *testing.T) {
		q := fixtureQuery("make test JOBS=10 FOO=file")
		q.Cwd = "/tmp"

		stats := exact(t, q)
		if stats.Count != 1 || stats.MaxMs != 3000 {
			t.Errorf("stats = %+v, want the single /tmp run", stats)
		}
	})

	t.Run("samples keeps the most recent", func(t *testing.T) {
		q := fixtureQuery("make test JOBS=10 FOO=file")
		q.Samples = 2

		stats := exact(t, q)
		if stats.Count != 2 || stats.MinMs != 200000 || stats.MaxMs != 300000 {
			t.Errorf("stats = %+v, want the two most recent runs", stats)
		}
	})
}

func TestQueryStatsPercentiles(t *testing.T) {
	h := newFixtureHistory(t)

	for i := range 25 {
		fixtureRun{
			ID: fmt.Sprintf("PCT%03d", i), Argv: "bench", Day: i, DurationMs: int64(i+1) * 1000,
		}.append(t, h)
	}

	q := fixtureQuery("bench")
	q.Samples = 20

	stats := queryStats(t, h, q).Matches[0].Stats
	if stats.Count != 20 || stats.MinMs != 6000 || stats.MaxMs != 25000 {
		t.Fatalf("stats = %+v, want runs 6 through 25", stats)
	}

	// Nearest rank over 20 values: p50 is rank 10 and p90 is rank 18.
	if stats.P50Ms != 15000 || stats.P90Ms != 23000 {
		t.Errorf("p50 = %d and p90 = %d, want 15000 and 23000", stats.P50Ms, stats.P90Ms)
	}
}

func TestQueryStatsLongArgv(t *testing.T) {
	h := newFixtureHistory(t)

	long := func(n int, tail string) string {
		tokens := make([]string, n)
		for i := range tokens {
			tokens[i] = fmt.Sprintf("a%d", i)
		}
		tokens[n-1] = tail

		return strings.Join(tokens, " ")
	}

	appendFixtureRuns(t, h, []fixtureRun{
		{ID: "LONG01", Argv: long(40, "x"), Day: 0, DurationMs: 1000},
		{ID: "LONG02", Argv: long(40, "x"), Day: 1, DurationMs: 2000},
		{ID: "LONG03", Argv: long(40, "y"), Day: 2, DurationMs: 9000},
	})

	got := queryStats(t, h, fixtureQuery(long(40, "x")))
	if exact := got.Matches[0]; exact.MatchType != StatsMatchExact || exact.Stats.Count != 2 {
		t.Errorf("exact entry = %+v, want 2 samples for the 40-token argv", exact)
	}

	// Prefix levels run from 32 tokens down to 1. The levels between 32 and 40 are not reported.
	if len(got.Matches) != 1+HistoryMaxArgvMACs {
		t.Fatalf("got %d entries, want %d", len(got.Matches), 1+HistoryMaxArgvMACs)
	}
	if first := got.Matches[1]; len(first.CommandPrefix) != HistoryMaxArgvMACs || first.Stats.Count != 3 {
		t.Errorf("first prefix entry = %+v, want 32 tokens with 3 samples", first)
	}
}

func TestQueryStatsExactExcludesLongerRuns(t *testing.T) {
	h := newFixtureHistory(t)
	appendFixtureRuns(t, h, []fixtureRun{
		{ID: "EXA001", Argv: "make", Day: 0, DurationMs: 1000},
		{ID: "EXA002", Argv: "make test", Day: 1, DurationMs: 9000},
	})

	got := shapesOf(queryStats(t, h, fixtureQuery("make")))
	want := []matchShape{{StatsMatchExact, "make", false, 0, 1}}
	if !slices.Equal(got, want) {
		t.Errorf("matches = %+v, want %+v", got, want)
	}

	got = shapesOf(queryStats(t, h, fixtureQuery("make test lint")))
	want = []matchShape{
		{StatsMatchExact, "make test lint", false, 0, 0},
		{StatsMatchPrefix, "make test", false, 0, 1},
		{StatsMatchPrefix, "make", false, 0, 2},
	}
	if !slices.Equal(got, want) {
		t.Errorf("matches = %+v, want %+v", got, want)
	}

	// A longer stored run counts at the prefix level but not at the exact entry.
	appendFixtureRuns(t, h, []fixtureRun{{ID: "EXA003", Argv: "make test lint", Day: 2, DurationMs: 4000}})

	got = shapesOf(queryStats(t, h, fixtureQuery("make test")))
	want = []matchShape{
		{StatsMatchExact, "make test", false, 0, 1},
		{StatsMatchPrefix, "make", false, 0, 3},
	}
	if !slices.Equal(got, want) {
		t.Errorf("matches = %+v, want %+v", got, want)
	}
}

func TestQueryStatsExactAtChainDepth(t *testing.T) {
	h := newFixtureHistory(t)

	tokens := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf("t%d", i)
		}

		return strings.Join(parts, " ")
	}

	appendFixtureRuns(t, h, []fixtureRun{
		{ID: "DEP001", Argv: tokens(32), Day: 0, DurationMs: 1000},
		{ID: "DEP002", Argv: tokens(33), Day: 1, DurationMs: 2000},
		{ID: "DEP003", Argv: tokens(34), Day: 2, DurationMs: 3000},
	})

	for _, test := range []struct {
		Name  string
		Query string
		Count int
	}{
		{"32 tokens excludes the 33 and 34 token runs", tokens(32), 1},
		{"33 tokens matches only the 33 token run", tokens(33), 1},
		{"34 tokens matches only the 34 token run", tokens(34), 1},
		{"35 tokens matches nothing", tokens(35), 0},
	} {
		t.Run(test.Name, func(t *testing.T) {
			exact := queryStats(t, h, fixtureQuery(test.Query)).Matches[0]
			if exact.MatchType != StatsMatchExact || exact.Stats.Count != test.Count {
				t.Errorf("exact entry = %+v, want count %d", exact, test.Count)
			}
		})
	}

	// The 32 token prefix level takes all three runs.
	prefix := queryStats(t, h, fixtureQuery(tokens(33))).Matches[1]
	if len(prefix.CommandPrefix) != 32 || prefix.Stats.Count != 3 {
		t.Errorf("prefix entry = %+v, want 32 tokens with 3 samples", prefix)
	}
}

func TestQueryStatsCountsRepeatedIDOnce(t *testing.T) {
	h := newFixtureHistory(t)

	dup := fixtureRun{ID: "DUP001", Argv: "make", Day: 0, DurationMs: 1000}
	dup.append(t, h)
	dup.append(t, h)

	if got := queryStats(t, h, fixtureQuery("make")).Matches[0].Stats.Count; got != 1 {
		t.Errorf("count = %d, want 1 for a repeated run ID", got)
	}

	commands := [][]string{{"make"}}
	for range 2 {
		err := h.Append(&PoolHistoryRecord{
			V: HistoryVersion, Kind: HistoryKindPool, ID: "DUPP01", StartedAt: statsFixtureBase,
			DurationMs: 5000, Parallelism: 1, OK: true,
			CwdMAC: h.CwdMAC(statsFixtureCwd), PoolMAC: h.PoolMAC(commands, 1, 1),
		})
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	stats, err := h.PoolWallStats(statsFixtureCwd, commands, 1, 1, DefaultStatsSamples)
	if err != nil {
		t.Fatalf("PoolWallStats: %v", err)
	}
	if stats.Count != 1 {
		t.Errorf("pool count = %d, want 1 for a repeated pool ID", stats.Count)
	}
}

func TestScanLogWaitsForAppender(t *testing.T) {
	h := newFixtureHistory(t)

	unlock, err := h.lock()
	if err != nil {
		t.Fatalf("lock: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- h.scanLog(func(historyHeader, []byte) {})
	}()

	select {
	case <-done:
		t.Fatal("scan finished while an appender held the lock")
	case <-time.After(100 * time.Millisecond):
	}

	unlock()

	if err := <-done; err != nil {
		t.Errorf("scanLog: %v", err)
	}
}

func TestQueryStatsSkipsUnreadableLines(t *testing.T) {
	h := newFixtureHistory(t)
	appendFixtureRuns(t, h, []fixtureRun{{ID: "GOOD01", Argv: "make", Day: 0, DurationMs: 1000}})

	good := mustJSON(t, h.NewRunRecord(&Meta{
		RunInfo:    RunInfo{ID: "GOOD02", Command: []string{"make"}, Cwd: statsFixtureCwd, StartedAt: statsFixtureBase},
		DurationMs: 3000,
	}, 0))

	future := strings.Replace(good, `"v":1`, `"v":2`, 1)
	unknownKind := strings.Replace(good, `"kind":"run"`, `"kind":"other"`, 1)
	tooLong := `{"v":1,"kind":"run","pad":"` + strings.Repeat("x", historyMaxLine) + `"}`

	lines := []string{"not json", future, unknownKind, tooLong, `{"v":1,"kind":"run","id":"TORN`, good}
	logPath := filepath.Join(h.Dir(), HistoryLogFilename)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("opening log: %v", err)
	}
	if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatalf("writing log: %v", err)
	}
	f.Close()

	stats := queryStats(t, h, fixtureQuery("make")).Matches[0].Stats
	if stats.Count != 2 {
		t.Errorf("count = %d, want 2: the good lines only", stats.Count)
	}
}

func TestQueryStatsReadsBothGenerations(t *testing.T) {
	h := newFixtureHistory(t)
	h.maxBytes = 1

	appendFixtureRuns(t, h, []fixtureRun{
		{ID: "ROT001", Argv: "make", Day: 0, DurationMs: 1000},
		{ID: "ROT002", Argv: "make", Day: 1, DurationMs: 2000},
	})

	if _, err := os.Stat(filepath.Join(h.Dir(), HistoryRotatedFilename)); err != nil {
		t.Fatalf("log did not rotate: %v", err)
	}

	if got := queryStats(t, h, fixtureQuery("make")).Matches[0].Stats.Count; got != 2 {
		t.Errorf("count = %d, want 2 across both generations", got)
	}
}

type invalidQueryTest struct {
	Name   string
	Mutate func(q *StatsQuery)
}

var invalidQueryTests = []invalidQueryTest{
	{"empty command", func(q *StatsQuery) { q.Argv = nil }},
	{"zero samples", func(q *StatsQuery) { q.Samples = 0 }},
	{"too many samples", func(q *StatsQuery) { q.Samples = MaxStatsSamples + 1 }},
	{"unknown pool filter", func(q *StatsQuery) { q.Pool = "some" }},
}

func TestQueryStatsRejectsInvalidQueries(t *testing.T) {
	h := newFixtureHistory(t)

	for _, test := range invalidQueryTests {
		t.Run(test.Name, func(t *testing.T) {
			q := fixtureQuery("make")
			test.Mutate(&q)

			if _, err := h.QueryStats(q); !errors.Is(err, ErrInvalidStatsQuery) {
				t.Errorf("err = %v, want ErrInvalidStatsQuery", err)
			}
		})
	}
}

func TestPoolWallStats(t *testing.T) {
	h := newFixtureHistory(t)

	commands := [][]string{{"make", "test"}, {"make", "lint"}}
	addPool := func(id string, day int, durationMs int64, ok bool, repeat, parallelism int) {
		t.Helper()

		err := h.Append(&PoolHistoryRecord{
			V: HistoryVersion, Kind: HistoryKindPool, ID: id, StartedAt: statsFixtureBase.AddDate(0, 0, day),
			DurationMs: durationMs, Parallelism: parallelism, OK: ok,
			CwdMAC: h.CwdMAC(statsFixtureCwd), PoolMAC: h.PoolMAC(commands, repeat, parallelism),
		})
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	addPool("POOL01", 0, 10000, true, 1, 2)
	addPool("POOL02", 1, 30000, true, 1, 2)
	addPool("POOL03", 2, 99000, false, 1, 2)
	addPool("POOL04", 3, 77000, true, 2, 2)
	addPool("POOL05", 3, 66000, true, 1, 4)

	stats, err := h.PoolWallStats(statsFixtureCwd, commands, 1, 2, DefaultStatsSamples)
	if err != nil {
		t.Fatalf("PoolWallStats: %v", err)
	}
	if stats.Count != 2 || stats.MaxMs != 30000 || stats.MinMs != 10000 {
		t.Errorf("stats = %+v, want the two successful pools of this exact shape", stats)
	}

	reordered := [][]string{commands[1], commands[0]}
	stats, err = h.PoolWallStats(statsFixtureCwd, reordered, 1, 2, DefaultStatsSamples)
	if err != nil {
		t.Fatalf("PoolWallStats: %v", err)
	}
	if stats.Count != 0 {
		t.Errorf("reordered pool count = %d, want 0", stats.Count)
	}

	if _, err := h.PoolWallStats(statsFixtureCwd, commands, 1, 2, 0); !errors.Is(err, ErrInvalidStatsQuery) {
		t.Errorf("zero samples err = %v, want ErrInvalidStatsQuery", err)
	}
}

func TestAppendDoesNotImport(t *testing.T) {
	h := newFixtureHistory(t)
	appendFixtureRuns(t, h, []fixtureRun{{ID: "APP001", Argv: "make", Day: 0, DurationMs: 1000}})

	if _, err := os.Lstat(filepath.Join(h.Dir(), HistoryImportedFilename)); err == nil {
		t.Error("append created the import marker")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return string(data)
}
