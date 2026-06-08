package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg"
)

// metaInput is the argument shape for `cg_meta`.
type metaInput struct {
	ID string `json:"id" jsonschema:"capture run ID"`
}

// metaOutput mirrors cg.Meta on the wire. Kept separate so tags and shape are
// explicit at the MCP boundary.
type metaOutput struct {
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

func registerMeta(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_meta",
		Description: "Return the meta.json blob for a finished capture run. Unknown ID and in-flight runs (no meta.json yet) are tool errors; poll until the run finishes.",
	}, handleMeta)
}

func handleMeta(_ context.Context, _ *mcpsdk.CallToolRequest, in metaInput) (*mcpsdk.CallToolResult, metaOutput, error) {
	dir, err := cg.LookupRunDir(in.ID)
	if err != nil {
		return nil, metaOutput{}, mapLookupError(in.ID, err)
	}

	m, err := cg.ReadMeta(dir)
	if err != nil {
		return nil, metaOutput{}, fmt.Errorf("reading meta.json for %s: %w", in.ID, err)
	}

	out := metaOutput{
		ID:          m.ID,
		Command:     m.Command,
		StartedAt:   m.StartedAt,
		FinishedAt:  m.FinishedAt,
		DurationMs:  m.DurationMs,
		ExitCode:    m.ExitCode,
		StdoutLines: m.StdoutLines,
		StderrLines: m.StderrLines,
	}
	if m.Signal != nil {
		sig := *m.Signal
		out.Signal = &sig
	}
	return nil, out, nil
}

// mapLookupError converts the cg sentinel errors into wire-friendly MCP tool
// errors. Non-sentinel errors are surfaced verbatim.
func mapLookupError(id string, err error) error {
	switch {
	case errors.Is(err, cg.ErrUnknownRunID):
		return fmt.Errorf("unknown run id: %s", id)
	case errors.Is(err, cg.ErrIncompleteRun):
		return fmt.Errorf("incomplete run: %s (missing meta.json)", id)
	default:
		return err
	}
}
