package model

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"
)

// Pool filter values for a stats query. Any is the default and selects every run.
const (
	StatsPoolAny  = "any"
	StatsPoolNone = "none"
	StatsPoolOnly = "only"
)

// Bounds for StatsQuery.Samples.
const (
	DefaultStatsSamples = 20
	MaxStatsSamples     = 1000
)

// Match types in a stats result.
const (
	StatsMatchExact  = "exact"
	StatsMatchPrefix = "prefix"
)

// ErrInvalidStatsQuery marks a query that a caller got wrong, as opposed to a log that could not
// be read. The CLI maps it to a usage error.
var ErrInvalidStatsQuery = errors.New("invalid stats query")

// historyMaxLine bounds one log line. A longer line is corrupt and is skipped whole.
const historyMaxLine = 1 << 20

// StatsQuery selects the runs a stats lookup covers. Build one with NewStatsQuery so the
// filters start at their defaults: exit code 0, the 20 most recent runs, any pool membership.
type StatsQuery struct {
	Argv []string

	// Cwd is canonicalized before hashing. Empty means the process working directory.
	Cwd string

	// Since keeps runs started at or after it. Nil keeps everything.
	Since *time.Time

	ExitCode ExitCodeFilter
	Samples  int
	Pool     string
}

// NewStatsQuery returns a query for argv with the default filters.
func NewStatsQuery(argv []string) StatsQuery {
	return StatsQuery{
		Argv:    argv,
		Samples: DefaultStatsSamples,
		Pool:    StatsPoolAny,
	}
}

// Validate reports every problem with the query, so a caller can fix them in one pass.
func (q StatsQuery) Validate() error {
	var errs []error

	if len(q.Argv) == 0 {
		errs = append(errs, fmt.Errorf("%w: command is required", ErrInvalidStatsQuery))
	}
	if q.Samples < 1 || q.Samples > MaxStatsSamples {
		errs = append(errs, fmt.Errorf("%w: samples %d out of range, want 1 to %d",
			ErrInvalidStatsQuery, q.Samples, MaxStatsSamples))
	}

	switch q.Pool {
	case StatsPoolAny, StatsPoolNone, StatsPoolOnly:
	default:
		errs = append(errs, fmt.Errorf("%w: pool %q, want any, none, or only", ErrInvalidStatsQuery, q.Pool))
	}

	return errors.Join(errs...)
}

// DurationStats summarizes the durations of the sampled runs. Percentiles use the nearest-rank
// method, so each is a duration that a sampled run actually took. With no samples every field is
// zero and LastAt is null.
type DurationStats struct {
	Count  int        `json:"count"`
	MinMs  int64      `json:"min_ms"`
	MaxMs  int64      `json:"max_ms"`
	AvgMs  int64      `json:"avg_ms"`
	P50Ms  int64      `json:"p50_ms"`
	P90Ms  int64      `json:"p90_ms"`
	LastMs int64      `json:"last_ms"`
	LastAt *time.Time `json:"last_at"`
}

// StatsMatch is one entry of a stats result. An exact match carries Command, a prefix match
// carries CommandPrefix. Parallelism is set only for pooled entries.
type StatsMatch struct {
	MatchType     string        `json:"match_type"`
	Command       []string      `json:"command,omitempty"`
	CommandPrefix []string      `json:"command_prefix,omitempty"`
	Pool          bool          `json:"pool"`
	Parallelism   int           `json:"parallelism,omitempty"`
	Stats         DurationStats `json:"stats"`
}

// StatsResult is the stats lookup answer, and the exact JSON that `cg stats -o json` prints.
type StatsResult struct {
	Cwd     string       `json:"cwd"`
	Matches []StatsMatch `json:"matches"`
}

// ExactStandalone returns the exact-match entry for standalone runs. It is nil when the query
// used the pool filter only.
func (r *StatsResult) ExactStandalone() *StatsMatch {
	for i := range r.Matches {
		m := &r.Matches[i]
		if m.MatchType == StatsMatchExact && !m.Pool {
			return m
		}
	}

	return nil
}

// ExactPooled returns the exact-match entry for pooled runs at parallelism, or nil when no
// pooled run at that parallelism matched.
func (r *StatsResult) ExactPooled(parallelism int) *StatsMatch {
	for i := range r.Matches {
		m := &r.Matches[i]
		if m.MatchType == StatsMatchExact && m.Pool && m.Parallelism == parallelism {
			return m
		}
	}

	return nil
}

// CanonicalCwd resolves dir the way `cg ls --cwd` does: absolute, then symlinks resolved. An
// empty dir means the process working directory.
func CanonicalCwd(dir string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolving working directory: %w", err)
		}
		dir = wd
	}

	f, err := NewCwdFilter(dir)
	if err != nil {
		return "", err
	}

	return f.resolved, nil
}

// statsLevel is one argv prefix a query reports. idx is the position in the MAC chain.
type statsLevel struct {
	idx       int
	prefixLen int
	exact     bool
}

// statsGroup splits a level's samples into standalone and per-parallelism pooled runs.
type statsGroup struct {
	pool        bool
	parallelism int
}

type statsSample struct {
	startedAt  time.Time
	durationMs int64
}

// statsLevels lists the exact level, then every proper prefix longest first. For an argv longer
// than the MAC chain depth, the exact level is the final full-argv entry and prefixes between
// the depth and the full length are not reported.
func statsLevels(argc int) []statsLevel {
	exactIdx := argc - 1
	if argc > HistoryMaxArgvMACs {
		exactIdx = HistoryMaxArgvMACs
	}

	levels := []statsLevel{{idx: exactIdx, prefixLen: argc, exact: true}}
	for n := min(argc-1, HistoryMaxArgvMACs); n >= 1; n-- {
		levels = append(levels, statsLevel{idx: n - 1, prefixLen: n})
	}

	return levels
}

// QueryStats reports how long matching runs took. It returns the exact match first, even with
// no samples, then every shorter prefix of the query that has at least one sample. A stored run
// longer than the query counts at the matching prefix level and never as its own entry. Pool
// records do not feed this lookup.
//
// The first query imports the runs already on disk. See EnsureImported.
func (h *History) QueryStats(q StatsQuery) (*StatsResult, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}

	cwd, err := CanonicalCwd(q.Cwd)
	if err != nil {
		return nil, err
	}

	if err := h.EnsureImported(); err != nil {
		return nil, err
	}

	macs := h.ArgvMACs(q.Argv)
	cwdMAC := h.CwdMAC(cwd)
	levels := statsLevels(len(q.Argv))
	buckets := make([]map[statsGroup][]statsSample, len(levels))
	for i := range buckets {
		buckets[i] = make(map[statsGroup][]statsSample)
	}

	err = h.scanLog(func(header historyHeader, line []byte) {
		if header.Kind != HistoryKindRun {
			return
		}

		var rec RunHistoryRecord
		if json.Unmarshal(line, &rec) != nil {
			return
		}
		if rec.CwdMAC != cwdMAC || rec.Signal != nil || !q.ExitCode.Match(rec.ExitCode) {
			return
		}
		if q.Since != nil && rec.StartedAt.Before(*q.Since) {
			return
		}
		if (q.Pool == StatsPoolNone && rec.Pool) || (q.Pool == StatsPoolOnly && !rec.Pool) {
			return
		}

		group := statsGroup{}
		if rec.Pool {
			group = statsGroup{pool: true, parallelism: rec.Parallelism}
		}

		for i, level := range levels {
			if len(rec.ArgvMACs) <= level.idx || rec.ArgvMACs[level.idx] != macs[level.idx] {
				continue
			}
			if level.exact && len(rec.ArgvMACs) != level.idx+1 {
				continue
			}
			buckets[i][group] = append(buckets[i][group], statsSample{rec.StartedAt, rec.DurationMs})
		}
	})
	if err != nil {
		return nil, err
	}

	result := &StatsResult{Cwd: cwd, Matches: []StatsMatch{}}
	for i, level := range levels {
		result.Matches = append(result.Matches, buildLevel(q, level, buckets[i])...)
	}

	return result, nil
}

// buildLevel turns a level's grouped samples into entries: standalone first, then pooled by
// ascending parallelism. The exact level always yields an entry. Under any and none that is the
// standalone one, which has count 0 when nothing matched. Under only it is a pooled entry with
// no parallelism.
func buildLevel(q StatsQuery, level statsLevel, groups map[statsGroup][]statsSample) []StatsMatch {
	newMatch := func(g statsGroup) StatsMatch {
		m := StatsMatch{Pool: g.pool, Parallelism: g.parallelism}
		if level.exact {
			m.MatchType = StatsMatchExact
			m.Command = slices.Clone(q.Argv)
		} else {
			m.MatchType = StatsMatchPrefix
			m.CommandPrefix = slices.Clone(q.Argv[:level.prefixLen])
		}
		m.Stats = computeStats(groups[g], q.Samples)

		return m
	}

	var entries []StatsMatch

	if q.Pool != StatsPoolOnly {
		standalone := statsGroup{}
		if level.exact || len(groups[standalone]) > 0 {
			entries = append(entries, newMatch(standalone))
		}
	}

	if q.Pool != StatsPoolNone {
		var pooled []statsGroup
		for g := range groups {
			if g.pool {
				pooled = append(pooled, g)
			}
		}
		sort.Slice(pooled, func(a, b int) bool { return pooled[a].parallelism < pooled[b].parallelism })

		for _, g := range pooled {
			entries = append(entries, newMatch(g))
		}
	}

	if len(entries) == 0 && level.exact {
		entries = append(entries, newMatch(statsGroup{pool: true}))
	}

	return entries
}

// computeStats summarizes the most recent limit samples.
func computeStats(samples []statsSample, limit int) DurationStats {
	if len(samples) == 0 {
		return DurationStats{}
	}

	sorted := slices.Clone(samples)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].startedAt.Before(sorted[b].startedAt) })
	if len(sorted) > limit {
		sorted = sorted[len(sorted)-limit:]
	}

	last := sorted[len(sorted)-1]
	lastAt := last.startedAt.UTC().Truncate(time.Second)

	durations := make([]int64, len(sorted))
	var sum int64
	for i, s := range sorted {
		durations[i] = s.durationMs
		sum += s.durationMs
	}
	slices.Sort(durations)

	return DurationStats{
		Count:  len(durations),
		MinMs:  durations[0],
		MaxMs:  durations[len(durations)-1],
		AvgMs:  sum / int64(len(durations)),
		P50Ms:  nearestRank(durations, 50),
		P90Ms:  nearestRank(durations, 90),
		LastMs: last.durationMs,
		LastAt: &lastAt,
	}
}

// nearestRank returns the pth percentile of ascending-sorted values by the nearest-rank method:
// the value at rank ceil(p/100 × n).
func nearestRank(sorted []int64, p int) int64 {
	rank := (p*len(sorted) + 99) / 100

	return sorted[max(rank, 1)-1]
}

// PoolWallStats summarizes the wall time of earlier pools with the same cwd and pool shape.
// Only pools in which every member succeeded count. It covers the most recent samples pools.
func (h *History) PoolWallStats(cwd string, commands [][]string, repeat, parallelism, samples int) (DurationStats, error) {
	if samples < 1 || samples > MaxStatsSamples {
		return DurationStats{}, fmt.Errorf("%w: samples %d out of range, want 1 to %d",
			ErrInvalidStatsQuery, samples, MaxStatsSamples)
	}

	resolved, err := CanonicalCwd(cwd)
	if err != nil {
		return DurationStats{}, err
	}

	if err := h.EnsureImported(); err != nil {
		return DurationStats{}, err
	}

	cwdMAC := h.CwdMAC(resolved)
	poolMAC := h.PoolMAC(commands, repeat, parallelism)

	var found []statsSample
	err = h.scanLog(func(header historyHeader, line []byte) {
		if header.Kind != HistoryKindPool {
			return
		}

		var rec PoolHistoryRecord
		if json.Unmarshal(line, &rec) != nil {
			return
		}
		if !rec.OK || rec.CwdMAC != cwdMAC || rec.PoolMAC != poolMAC {
			return
		}

		found = append(found, statsSample{rec.StartedAt, rec.DurationMs})
	})
	if err != nil {
		return DurationStats{}, err
	}

	return computeStats(found, samples), nil
}

// historyHeader is the part of a record every version and kind shares. It is decoded first so
// a record of an unknown version or kind is never interpreted as a known shape.
type historyHeader struct {
	V    int    `json:"v"`
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// scanLog calls visit for every well-formed record of a known version, oldest first across both
// generations. Malformed lines, over-long lines, and unknown versions are skipped. A missing
// generation is empty.
//
// Only the first record of a kind and ID is visited. A run can reach the log twice when the
// import reads its meta.json before the run appends its own record.
//
// The scan holds the history lock shared, so a rotation cannot hide records between reading the
// two generations. A caller that already holds the lock uses scanLogLocked.
func (h *History) scanLog(visit func(header historyHeader, line []byte)) error {
	unlock, err := h.lockShared()
	if err != nil {
		return err
	}
	defer unlock()

	return h.scanLogLocked(visit)
}

func (h *History) scanLogLocked(visit func(header historyHeader, line []byte)) error {
	seen := make(map[string]bool)
	dedupe := func(header historyHeader, line []byte) {
		if header.ID != "" {
			key := header.Kind + "\x00" + header.ID
			if seen[key] {
				return
			}
			seen[key] = true
		}

		visit(header, line)
	}

	for _, path := range h.LogPaths() {
		if err := scanLogFile(path, dedupe); err != nil {
			return err
		}
	}

	return nil
}

// scanLogFile reads lines through a fixed buffer. A line longer than historyMaxLine is corrupt,
// and is skipped to its newline without being held in memory.
func scanLogFile(path string, visit func(header historyHeader, line []byte)) error {
	f, err := openHistoryFile(path, os.O_RDONLY)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("opening history log: %w", err)
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 64<<10)
	skipping := false
	var pending []byte

	for {
		chunk, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// The line outgrew the buffer. Keep collecting only while it can still be a record.
			if !skipping {
				pending = append(pending, chunk...)
				skipping = len(pending) > historyMaxLine
			}
			continue
		}

		if len(chunk) > 0 && !skipping {
			line := chunk
			if len(pending) > 0 {
				line = append(pending, chunk...)
			}

			var header historyHeader
			if len(line) <= historyMaxLine && json.Unmarshal(line, &header) == nil && header.V == HistoryVersion {
				visit(header, line)
			}
		}

		pending = pending[:0]
		skipping = false

		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return fmt.Errorf("reading %s: %w", filepath.Base(path), err)
		}
	}
}
