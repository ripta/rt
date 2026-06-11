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
	State string `json:"state,omitempty" jsonschema:"which runs to surface: all|finished|running|failed; default finished"`
}

// listOutput is the result shape for `cg_list`.
type listOutput struct {
	Runs []listRun `json:"runs"`
}

// listRun is a single row in the cg_list response. Only `id` and `state` are
// guaranteed; the meta-derived fields are populated for finished runs only.
// In-flight rows may carry `started_at` synthesized from the run dir's mtime
// when filesystem stat succeeds. Failed rows carry `start_error` and `command`.
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
		Description: "List recent capture runs, most-recent-first by directory mtime. The `state` input filters to finished (default), running, failed, or all runs. Failed rows include state: \"failed\" and start_error. Running rows are sparse: id, state, and started_at from the run dir's mtime.",
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
	case listStateAll, stateFinished, stateRunning, stateFailed:
	default:
		return nil, listOutput{}, fmt.Errorf("invalid state %q: want all|finished|running|failed", in.State)
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
				if state == stateRunning || state == stateFinished {
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
			if state == stateFinished || state == stateFailed {
				continue
			}
			started := mtime
			rows = append(rows, row{
				mtime: mtime,
				run: listRun{
					ID:        name,
					State:     stateRunning,
					StartedAt: &started,
				},
			})
			continue
		}

		if state == stateRunning || state == stateFailed {
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
