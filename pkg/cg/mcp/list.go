package mcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/model"
)

const (
	defaultListLimit = 20
	maxListLimit     = 1000

	listStateAll = "all"

	listPoolNone = "none"
	listPoolAny  = "any"

	listKindPool = "pool"
)

// listInput is the argument shape for `cg_list`.
type listInput struct {
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum number of runs to return; default 20, max 1000"`
	State    string `json:"state,omitempty" jsonschema:"which runs to surface: all|finished|running|failed|abandoned|unknown; default all"`
	Pool     string `json:"pool,omitempty" jsonschema:"pool handling: empty collapses members behind one row per pool, a pool ID lists that pool's members, \"none\" lists only standalone runs, \"any\" lists everything uncollapsed"`
	ExitCode string `json:"exit_code,omitempty" jsonschema:"filter finished runs by exit code: N (equals), !=N, >=N, >N, <N, or <=N; runs with no exit code never match and pool summary rows always pass through regardless"`
	Since    string `json:"since,omitempty" jsonschema:"only include runs started at or after TIME: a relative duration meaning ago (e.g. 4h, 7d), an RFC3339 timestamp, or a bare YYYY-MM-DD date at local midnight"`
	Before   string `json:"before,omitempty" jsonschema:"only include runs started strictly before TIME, same grammar as since"`
}

// listOutput is the result shape for `cg_list`.
type listOutput struct {
	Runs []listRun `json:"runs"`
}

// listRun is a single row in the cg_list response. Only `id` and `state` are
// guaranteed; the remaining meta-derived fields are populated for finished runs
// only. In-flight and abandoned rows carry `command` and `started_at` from
// start.json. When start.json is absent, `started_at` falls back to the run
// dir's mtime and `started_at_approx` is set, so callers can tell an estimate
// from a measurement. A row with no lock file, pid file, or start.json at all
// carries no liveness signal whatsoever and is reported as state: "unknown"
// rather than "running". Failed rows carry `start_error` and `command`.
//
// Pool rows carry kind: "pool" and member counts instead of a command; their
// timestamps come from the manifest. Member rows name their pool in `pool`.
type listRun struct {
	ID              string            `json:"id"`
	State           string            `json:"state"`
	Kind            string            `json:"kind,omitempty"`
	Pool            string            `json:"pool,omitempty"`
	Counts          *model.PoolCounts `json:"counts,omitempty"`
	Command         []string          `json:"command,omitempty"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	StartedAtApprox bool              `json:"started_at_approx,omitempty"`
	FinishedAt      *time.Time        `json:"finished_at,omitempty"`
	DurationMs      *int64            `json:"duration_ms,omitempty"`
	ExitCode        *int              `json:"exit_code,omitempty"`
	Signal          *int              `json:"signal,omitempty"`
	StdoutLines     *int64            `json:"stdout_lines,omitempty"`
	StderrLines     *int64            `json:"stderr_lines,omitempty"`
	StartError      string            `json:"start_error,omitempty"`
}

func registerList(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_list",
		Description: "List recent capture runs, most-recent-first by directory mtime. The `state` input filters to all (default), finished, running, failed, abandoned, or unknown runs. Failed rows include state: \"failed\" and start_error. Running and abandoned rows carry id, state, command, and started_at from start.json, falling back to the run dir's mtime (with started_at_approx: true) when start.json is absent. An abandoned run is one whose supervisor died before recording the run's exit. A run with no lock file, pid file, or start.json at all carries no liveness signal and is reported as state: \"unknown\" rather than \"running\". The `exit_code` input filters finished runs by exit code: N (equals), !=N, >=N, >N, <N, or <=N; runs with no exit code (running, abandoned, unknown, start-failed) never match, and pool summary rows always pass through regardless of exit_code. The `since`/`before` inputs filter by start time: TIME accepts a relative duration meaning ago (e.g. 4h, 7d), a full RFC3339 timestamp, or a bare YYYY-MM-DD date at local midnight; since is inclusive, before is exclusive, and unlike exit_code these bounds do apply to pool rows, using the pool's own precise started_at. Pool members collapse behind one row per pool with kind: \"pool\" and member counts; the `pool` input expands them: a pool ID lists that pool's members, \"none\" lists only standalone runs, \"any\" lists everything uncollapsed.",
	}, handleList)
}

func handleList(_ context.Context, _ *mcpsdk.CallToolRequest, in listInput) (*mcpsdk.CallToolResult, listOutput, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	poolFilter := in.Pool
	switch {
	case poolFilter == "", poolFilter == listPoolNone, poolFilter == listPoolAny, model.IsValidRunID(poolFilter):
	default:
		return nil, listOutput{}, fmt.Errorf("invalid pool %q: want a pool ID, none, or any", in.Pool)
	}
	memberMode := poolFilter != "" && poolFilter != listPoolNone && poolFilter != listPoolAny

	state := in.State
	if state == "" {
		state = listStateAll
	}
	switch state {
	case listStateAll, stateFinished, stateRunning, stateFailed, stateAbandoned, stateUnknown:
	default:
		return nil, listOutput{}, fmt.Errorf("invalid state %q: want all|finished|running|failed|abandoned|unknown", in.State)
	}

	var exitFilter *model.ExitCodeFilter
	if in.ExitCode != "" {
		f, err := model.ParseExitCodeFilter(in.ExitCode)
		if err != nil {
			return nil, listOutput{}, fmt.Errorf("invalid exit_code: %w", err)
		}
		exitFilter = &f
	}

	now := time.Now()
	var sinceT, beforeT *time.Time
	if in.Since != "" {
		t, err := model.ParseFilterTime(in.Since, now)
		if err != nil {
			return nil, listOutput{}, fmt.Errorf("invalid since: %w", err)
		}
		sinceT = &t
	}
	if in.Before != "" {
		t, err := model.ParseFilterTime(in.Before, now)
		if err != nil {
			return nil, listOutput{}, fmt.Errorf("invalid before: %w", err)
		}
		beforeT = &t
	}
	timeFilter, err := model.NewTimeRangeFilter(sinceT, beforeT)
	if err != nil {
		return nil, listOutput{}, err
	}

	root := model.CaptureRoot()
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, listOutput{Runs: []listRun{}}, nil
	}
	if err != nil {
		return nil, listOutput{}, fmt.Errorf("reading capture root: %w", err)
	}

	type row struct {
		mtime       time.Time
		filterStart time.Time
		run         listRun
	}
	rows := make([]row, 0, len(entries))
	poolDirs := make(map[string]bool)
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !model.IsValidRunID(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mtime := info.ModTime()
		dir := filepath.Join(root, name)

		meta, err := model.ReadMeta(dir)
		if err != nil {
			// No meta.json: a pool dir (has pool.json), a failed run (has
			// debug.json), or a run still in flight (none of them present yet).
			if m, perr := model.ReadPoolManifest(dir); perr == nil {
				poolDirs[name] = true
				counts := m.Counts()
				started := m.StartedAt
				rows = append(rows, row{
					mtime:       mtime,
					filterStart: m.StartedAt,
					run: listRun{
						ID:         name,
						State:      model.PoolState(dir, m),
						Kind:       listKindPool,
						Counts:     &counts,
						StartedAt:  &started,
						FinishedAt: m.FinishedAt,
					},
				})
				continue
			}
			if dbg, dbgErr := model.ReadStartDebug(dir); dbgErr == nil {
				started := dbg.StartedAt
				rows = append(rows, row{
					mtime:       mtime,
					filterStart: dbg.StartedAt,
					run: listRun{
						ID:         name,
						State:      stateFailed,
						Pool:       dbg.Pool,
						Command:    dbg.Command,
						StartedAt:  &started,
						StartError: dbg.StartError,
					},
				})
				continue
			}
			// A running capture writes start.json with its command and precise
			// start time; fall back to the run dir's mtime when it is absent.
			hasLock := model.LockFileExists(dir)
			si, siErr := model.ReadStartInfo(dir)
			hasStart := siErr == nil

			// A released run lock with no meta.json means the supervisor died
			// before recording the run's exit: the run is abandoned, not running.
			// No lock file, pid file, or start.json at all means no liveness
			// signal has ever been recorded for this directory: unknown, not
			// running.
			rowState := stateRunning
			switch {
			case !hasLock && !hasStart:
				rowState = stateUnknown
			case model.RunLockReleased(dir):
				rowState = stateAbandoned
			}

			r := listRun{ID: name, State: rowState}
			filterStart := mtime
			if hasStart {
				started := si.StartedAt
				r.StartedAt = &started
				r.Command = si.Command
				r.Pool = si.Pool
				filterStart = si.StartedAt
			} else {
				started := mtime
				r.StartedAt = &started
				r.StartedAtApprox = true
			}
			rows = append(rows, row{mtime: mtime, filterStart: filterStart, run: r})
			continue
		}

		started := meta.StartedAt
		finished := meta.FinishedAt
		duration := meta.DurationMs
		exit := meta.ExitCode
		stdoutLines := meta.StdoutLines
		stderrLines := meta.StderrLines
		r := listRun{
			ID:          meta.ID,
			State:       stateFinished,
			Pool:        meta.Pool,
			Command:     meta.Command,
			StartedAt:   &started,
			FinishedAt:  &finished,
			DurationMs:  &duration,
			ExitCode:    &exit,
			StdoutLines: &stdoutLines,
			StderrLines: &stderrLines,
		}
		if meta.Signal != nil {
			sig := *meta.Signal
			r.Signal = &sig
		}
		rows = append(rows, row{mtime: mtime, filterStart: meta.StartedAt, run: r})
	}

	// The pool filter runs before the state filter so collapsing is independent
	// of which states survive. A member whose pool dir is gone is an orphan and
	// counts as standalone.
	kept := rows[:0]
	for _, r := range rows {
		switch {
		case memberMode:
			if r.run.Pool != poolFilter {
				continue
			}
		case poolFilter == listPoolAny:
		case poolFilter == listPoolNone:
			if r.run.Kind == listKindPool || (r.run.Pool != "" && poolDirs[r.run.Pool]) {
				continue
			}
		default:
			if r.run.Kind != listKindPool && r.run.Pool != "" && poolDirs[r.run.Pool] {
				continue
			}
		}
		kept = append(kept, r)
	}
	rows = kept

	if memberMode && len(rows) == 0 && !poolDirs[poolFilter] {
		return nil, listOutput{}, fmt.Errorf("unknown pool id: %s", poolFilter)
	}

	if state != listStateAll {
		kept = rows[:0]
		for _, r := range rows {
			if r.run.State == state {
				kept = append(kept, r)
			}
		}
		rows = kept
	}

	if exitFilter != nil {
		kept = rows[:0]
		for _, r := range rows {
			switch {
			case r.run.Kind == listKindPool:
				kept = append(kept, r) // pool summary rows always pass through
			case r.run.ExitCode != nil && exitFilter.Match(*r.run.ExitCode):
				kept = append(kept, r)
			}
		}
		rows = kept
	}

	if sinceT != nil || beforeT != nil {
		kept = rows[:0]
		for _, r := range rows {
			if timeFilter.Match(r.filterStart) {
				kept = append(kept, r)
			}
		}
		rows = kept
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].mtime.After(rows[j].mtime)
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}

	out := listOutput{Runs: make([]listRun, len(rows))}
	for i, r := range rows {
		out.Runs[i] = r.run
	}
	return nil, out, nil
}
