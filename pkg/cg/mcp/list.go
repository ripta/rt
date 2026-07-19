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

	"github.com/ripta/rt/pkg/cg"
)

const (
	defaultListLimit = 20
	maxListLimit     = 1000

	listStateAll = "all"

	defaultListState = stateFinished

	listPoolNone = "none"
	listPoolAny  = "any"

	listKindPool = "pool"
)

// listInput is the argument shape for `cg_list`.
type listInput struct {
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of runs to return; default 20, max 1000"`
	State string `json:"state,omitempty" jsonschema:"which runs to surface: all|finished|running|failed|abandoned|unknown; default finished, or all when pool is a pool ID"`
	Pool  string `json:"pool,omitempty" jsonschema:"pool handling: empty collapses members behind one row per pool, a pool ID lists that pool's members, \"none\" lists only standalone runs, \"any\" lists everything uncollapsed"`
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
	ID              string         `json:"id"`
	State           string         `json:"state"`
	Kind            string         `json:"kind,omitempty"`
	Pool            string         `json:"pool,omitempty"`
	Counts          *cg.PoolCounts `json:"counts,omitempty"`
	Command         []string       `json:"command,omitempty"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	StartedAtApprox bool           `json:"started_at_approx,omitempty"`
	FinishedAt      *time.Time     `json:"finished_at,omitempty"`
	DurationMs      *int64         `json:"duration_ms,omitempty"`
	ExitCode        *int           `json:"exit_code,omitempty"`
	Signal          *int           `json:"signal,omitempty"`
	StdoutLines     *int64         `json:"stdout_lines,omitempty"`
	StderrLines     *int64         `json:"stderr_lines,omitempty"`
	StartError      string         `json:"start_error,omitempty"`
}

func registerList(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_list",
		Description: "List recent capture runs, most-recent-first by directory mtime. The `state` input filters to finished (default), running, failed, abandoned, unknown, or all runs. Failed rows include state: \"failed\" and start_error. Running and abandoned rows carry id, state, command, and started_at from start.json, falling back to the run dir's mtime (with started_at_approx: true) when start.json is absent. An abandoned run is one whose supervisor died before recording the run's exit. A run with no lock file, pid file, or start.json at all carries no liveness signal and is reported as state: \"unknown\" rather than \"running\". Pool members collapse behind one row per pool with kind: \"pool\" and member counts; the `pool` input expands them: a pool ID lists that pool's members (state defaults to all), \"none\" lists only standalone runs, \"any\" lists everything uncollapsed.",
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
	case poolFilter == "", poolFilter == listPoolNone, poolFilter == listPoolAny, cg.IsValidRunID(poolFilter):
	default:
		return nil, listOutput{}, fmt.Errorf("invalid pool %q: want a pool ID, none, or any", in.Pool)
	}
	memberMode := poolFilter != "" && poolFilter != listPoolNone && poolFilter != listPoolAny

	// Asking for a pool's roster implies wanting every member, running and
	// failed included, so member mode defaults the state filter to all.
	state := in.State
	if state == "" {
		state = defaultListState
		if memberMode {
			state = listStateAll
		}
	}
	switch state {
	case listStateAll, stateFinished, stateRunning, stateFailed, stateAbandoned, stateUnknown:
	default:
		return nil, listOutput{}, fmt.Errorf("invalid state %q: want all|finished|running|failed|abandoned|unknown", in.State)
	}

	root := cg.CaptureRoot()
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, listOutput{Runs: []listRun{}}, nil
	}
	if err != nil {
		return nil, listOutput{}, fmt.Errorf("reading capture root: %w", err)
	}

	type row struct {
		mtime time.Time
		run   listRun
	}
	rows := make([]row, 0, len(entries))
	poolDirs := make(map[string]bool)
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !cg.IsValidRunID(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mtime := info.ModTime()
		dir := filepath.Join(root, name)

		meta, err := cg.ReadMeta(dir)
		if err != nil {
			// No meta.json: a pool dir (has pool.json), a failed run (has
			// debug.json), or a run still in flight (none of them present yet).
			if m, perr := cg.ReadPoolManifest(dir); perr == nil {
				poolDirs[name] = true
				counts := m.Counts()
				started := m.StartedAt
				rows = append(rows, row{
					mtime: mtime,
					run: listRun{
						ID:         name,
						State:      cg.PoolState(dir, m),
						Kind:       listKindPool,
						Counts:     &counts,
						StartedAt:  &started,
						FinishedAt: m.FinishedAt,
					},
				})
				continue
			}
			if dbg, dbgErr := cg.ReadStartDebug(dir); dbgErr == nil {
				started := mtime
				rows = append(rows, row{
					mtime: mtime,
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
			hasLock := cg.LockFileExists(dir)
			si, siErr := cg.ReadStartInfo(dir)
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
			case cg.RunLockReleased(dir):
				rowState = stateAbandoned
			}

			r := listRun{ID: name, State: rowState}
			if hasStart {
				started := si.StartedAt
				r.StartedAt = &started
				r.Command = si.Command
				r.Pool = si.Pool
			} else {
				started := mtime
				r.StartedAt = &started
				r.StartedAtApprox = true
			}
			rows = append(rows, row{mtime: mtime, run: r})
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
		rows = append(rows, row{mtime: mtime, run: r})
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
