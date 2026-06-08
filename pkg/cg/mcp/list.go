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
)

// listInput is the argument shape for `cg_list`.
type listInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"maximum number of runs to return; default 20, max 1000"`
}

// listOutput is the result shape for `cg_list`.
type listOutput struct {
	Runs []listRun `json:"runs"`
}

// listRun is a single row in the cg_list response. Incomplete runs are
// skipped, so every field on this struct is reliable.
type listRun struct {
	ID          string    `json:"id"`
	Command     []string  `json:"command"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	DurationMs  int64     `json:"duration_ms"`
	ExitCode    int       `json:"exit_code"`
	Signal      *int      `json:"signal,omitempty"`
	StdoutLines int64     `json:"stdout_lines"`
	StderrLines int64     `json:"stderr_lines"`
}

func registerList(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_list",
		Description: "List recent capture runs, most-recent-first by directory mtime. Incomplete runs (started, no meta.json yet) are skipped.",
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
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		meta, err := cg.ReadMeta(dir)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		r := listRun{
			ID:          meta.ID,
			Command:     meta.Command,
			StartedAt:   meta.StartedAt,
			FinishedAt:  meta.FinishedAt,
			DurationMs:  meta.DurationMs,
			ExitCode:    meta.ExitCode,
			StdoutLines: meta.StdoutLines,
			StderrLines: meta.StderrLines,
		}
		if meta.Signal != nil {
			sig := *meta.Signal
			r.Signal = &sig
		}
		rows = append(rows, row{mtime: info.ModTime(), run: r})
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
