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
)

// listInput is the argument shape for `cg_list`.
type listInput struct {
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of runs to return; default 20, max 1000"`
	State string `json:"state,omitempty" jsonschema:"which runs to surface: all|finished|running|failed|abandoned; default finished"`
}

// listOutput is the result shape for `cg_list`.
type listOutput struct {
	Runs []listRun `json:"runs"`
}

// listRun is a single row in the cg_list response. Only `id` and `state` are
// guaranteed; the remaining meta-derived fields are populated for finished runs
// only. In-flight and abandoned rows carry `command` and `started_at` from
// start.json, or just `started_at` synthesized from the run dir's mtime when
// start.json is absent. Failed rows carry `start_error` and `command`.
type listRun struct {
	ID          string     `json:"id"`
	State       string     `json:"state"`
	Command     []string   `json:"command,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	DurationMs  *int64     `json:"duration_ms,omitempty"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	Signal      *int       `json:"signal,omitempty"`
	StdoutLines *int64     `json:"stdout_lines,omitempty"`
	StderrLines *int64     `json:"stderr_lines,omitempty"`
	StartError  string     `json:"start_error,omitempty"`
}

func registerList(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_list",
		Description: "List recent capture runs, most-recent-first by directory mtime. The `state` input filters to finished (default), running, failed, abandoned, or all runs. Failed rows include state: \"failed\" and start_error. Running and abandoned rows carry id, state, command, and started_at from start.json, falling back to the run dir's mtime when start.json is absent. An abandoned run is one whose supervisor died before recording the run's exit.",
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

	state := in.State
	if state == "" {
		state = defaultListState
	}
	switch state {
	case listStateAll, stateFinished, stateRunning, stateFailed, stateAbandoned:
	default:
		return nil, listOutput{}, fmt.Errorf("invalid state %q: want all|finished|running|failed|abandoned", in.State)
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
			// No meta.json: distinguish a failed run (has debug.json) from one
			// still in flight (neither file present yet).
			if dbg, dbgErr := cg.ReadStartDebug(dir); dbgErr == nil {
				if state != listStateAll && state != stateFailed {
					continue
				}
				started := mtime
				rows = append(rows, row{
					mtime: mtime,
					run: listRun{
						ID:         name,
						State:      stateFailed,
						Command:    dbg.Command,
						StartedAt:  &started,
						StartError: dbg.StartError,
					},
				})
				continue
			}
			// A released run lock with no meta.json means the supervisor died
			// before recording the run's exit: the run is abandoned, not running.
			rowState := stateRunning
			if cg.RunLockReleased(dir) {
				rowState = stateAbandoned
			}
			if state != listStateAll && state != rowState {
				continue
			}

			// A running capture writes start.json with its command and precise
			// start time; fall back to the run dir's mtime when it is absent.
			r := listRun{ID: name, State: rowState}
			if si, siErr := cg.ReadStartInfo(dir); siErr == nil {
				started := si.StartedAt
				r.StartedAt = &started
				r.Command = si.Command
			} else {
				started := mtime
				r.StartedAt = &started
			}
			rows = append(rows, row{mtime: mtime, run: r})
			continue
		}

		if state != listStateAll && state != stateFinished {
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
