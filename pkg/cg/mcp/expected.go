package mcp

import (
	"time"

	"github.com/ripta/rt/pkg/cg/model"
)

const (
	historySourceRun      = "run"
	historySourcePool     = "pool"
	historySourceEstimate = "estimate"
)

// historyInfo is what earlier runs say about how long a command or pool takes.
type historyInfo struct {
	Source string `json:"source"`
	Count  int    `json:"count,omitempty"`
	P50Ms  *int64 `json:"p50_ms,omitempty"`
	P90Ms  *int64 `json:"p90_ms,omitempty"`
	MaxMs  int64  `json:"max_ms"`
}

// timedOutFields rides on every response that returns while work is still running.
//
// History is nil when there are no samples. An estimate carries only max_ms, which holds the
// member estimate.
type timedOutFields struct {
	ElapsedMs *int64       `json:"elapsed_ms,omitempty"`
	History   *historyInfo `json:"history,omitempty"`
}

// timedOutFieldsFor builds the timed-out fields. A zero start means the recorded start is gone,
// so the elapsed time counts from fallback instead.
func timedOutFieldsFor(start, fallback time.Time, history *historyInfo) timedOutFields {
	if start.IsZero() {
		start = fallback
	}

	elapsed := time.Since(start).Milliseconds()

	return timedOutFields{ElapsedMs: &elapsed, History: history}
}

// expectedRun looks up the exact standalone match for argv in cwd. Prefix matches never count.
// Any history error reads as no history, because history is advisory.
func expectedRun(argv []string, cwd string) *historyInfo {
	history, err := model.OpenHistory()
	if err != nil {
		return nil
	}

	query := model.NewStatsQuery(argv)
	query.Cwd = cwd

	result, err := history.QueryStats(query)
	if err != nil {
		return nil
	}

	exact := result.ExactStandalone()
	if exact == nil || exact.Stats.Count == 0 {
		return nil
	}

	return &historyInfo{
		Source: historySourceRun,
		Count:  exact.Stats.Count,
		P50Ms:  &exact.Stats.P50Ms,
		P90Ms:  &exact.Stats.P90Ms,
		MaxMs:  exact.Stats.MaxMs,
	}
}

// expectedRunDir looks up a run by the argv and cwd its start.json recorded, never by anything the
// caller supplied. A pool member has no standalone expectation.
func expectedRunDir(dir string) *historyInfo {
	info, err := model.ReadStartInfo(dir)
	if err != nil || info.Pool != "" {
		return nil
	}

	return expectedRun(info.Command, info.Cwd)
}

// expectedPool looks up the pool record for the pool's exact shape first. It then falls back to
// the member estimate. parallelism must be the value the pool supervisor records.
func expectedPool(cwd string, commands [][]string, repeat, parallelism int) *historyInfo {
	history, err := model.OpenHistory()
	if err != nil {
		return nil
	}

	wall, err := history.PoolWallStats(cwd, commands, repeat, parallelism, model.DefaultStatsSamples)
	if err == nil && wall.Count > 0 {
		return &historyInfo{
			Source: historySourcePool,
			Count:  wall.Count,
			P50Ms:  &wall.P50Ms,
			P90Ms:  &wall.P90Ms,
			MaxMs:  wall.MaxMs,
		}
	}

	return estimatePool(history, cwd, commands, repeat, parallelism)
}

// estimatePool covers a pool shape seen for the first time. Every distinct command needs a pooled
// exact match at this parallelism. One command without samples voids the estimate.
func estimatePool(history *model.History, cwd string, commands [][]string, repeat, parallelism int) *historyInfo {
	if len(commands) == 0 || parallelism < 1 {
		return nil
	}

	seen := make(map[string]struct{}, len(commands))

	var longest int64
	for _, argv := range commands {
		key := model.ArgvKey(argv)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		query := model.NewStatsQuery(argv)
		query.Cwd = cwd

		result, err := history.QueryStats(query)
		if err != nil {
			return nil
		}

		pooled := result.ExactPooled(parallelism)
		if pooled == nil || pooled.Stats.Count == 0 {
			return nil
		}

		longest = max(longest, pooled.Stats.MaxMs)
	}

	batches := (int64(len(commands)*repeat) + int64(parallelism) - 1) / int64(parallelism)

	return &historyInfo{Source: historySourceEstimate, MaxMs: batches * longest}
}

// expectedPoolManifest looks up a pool by the shape its manifest recorded.
func expectedPoolManifest(m *model.PoolManifest) *historyInfo {
	return expectedPool(m.Cwd, m.Commands, m.Repeat, m.Parallelism)
}

// autoWaitTimeoutMs turns an expectation into a wait timeout: max_ms with 50% headroom, never
// below the default. No expectation yields the default.
func autoWaitTimeoutMs(history *historyInfo) int {
	if history == nil {
		return defaultWaitTimeoutMs
	}

	headroom := (history.MaxMs*3 + 1) / 2

	return int(max(headroom, defaultWaitTimeoutMs))
}
