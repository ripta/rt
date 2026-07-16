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

// metaFields holds the meta.json-derived fields shared between `cg_meta` and
// `cg_wait`. All fields are pointer-typed with `omitempty` so they collapse
// out of the JSON response when the run is still in flight and the caller
// has no meta to report.
type metaFields struct {
	Command     []string   `json:"command,omitempty"`
	Cwd         string     `json:"cwd,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	DurationMs  *int64     `json:"duration_ms,omitempty"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	Signal      *int       `json:"signal,omitempty"`
	StdoutLines *int64     `json:"stdout_lines,omitempty"`
	StderrLines *int64     `json:"stderr_lines,omitempty"`
	Usage       *cg.Usage  `json:"usage,omitempty"`
}

// metaOutput is the result shape for `cg_meta`. State is always populated.
// Running and finished runs carry the shared start-time fields; finished runs
// add the finish fields; failed runs carry Debug. A pool ID returns the pool
// state and the manifest verbatim, which never holds excerpts.
type metaOutput struct {
	ID       string           `json:"id"`
	State    string           `json:"state"`
	Debug    *cg.StartDebug   `json:"debug,omitempty"`
	Manifest *cg.PoolManifest `json:"manifest,omitempty"`
	metaFields
}

func registerMeta(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_meta",
		Description: "Return the run state and meta.json fields for a capture run. Finished runs return state: \"finished\" with all meta fields. In-flight runs return state: \"running\" with command, cwd, and started_at from start.json. Failed-to-start runs return state: \"failed\" with a debug field carrying cwd and the resolution diagnostics. A pool ID returns the pool state with the manifest in a manifest field. Unknown ID is a tool error.",
	}, handleMeta)
}

func handleMeta(_ context.Context, _ *mcpsdk.CallToolRequest, in metaInput) (*mcpsdk.CallToolResult, metaOutput, error) {
	dir, err := cg.LookupRunDir(in.ID)
	switch {
	case errors.Is(err, cg.ErrUnknownRunID):
		return nil, metaOutput{}, fmt.Errorf("unknown run id: %s", in.ID)
	case errors.Is(err, cg.ErrIncompleteRun):
		// A directory without meta.json is either an in-flight run or a pool;
		// the manifest's presence is what distinguishes the two.
		if m, perr := cg.ReadPoolManifest(dir); perr == nil {
			return nil, metaOutput{ID: in.ID, State: cg.PoolState(dir, m), Manifest: m}, nil
		}
		out := metaOutput{ID: in.ID, State: stateRunning}
		if si, siErr := cg.ReadStartInfo(dir); siErr == nil {
			out.metaFields = metaFieldsFromStart(si)
		}
		return nil, out, nil
	case errors.Is(err, cg.ErrFailedRun):
		dbg, _ := cg.ReadStartDebug(dir)
		return nil, metaOutput{ID: in.ID, State: stateFailed, Debug: dbg}, nil
	case err != nil:
		return nil, metaOutput{}, err
	}

	m, err := cg.ReadMeta(dir)
	if err != nil {
		return nil, metaOutput{}, fmt.Errorf("reading meta.json for %s: %w", in.ID, err)
	}

	return nil, metaOutput{
		ID:         m.ID,
		State:      stateFinished,
		metaFields: metaFieldsFrom(m),
	}, nil
}

// mapLookupError converts the cg sentinel errors into wire-friendly MCP tool
// errors. Non-sentinel errors are surfaced verbatim. Used by tools (paths,
// stream) that genuinely cannot operate on in-flight runs and still treat the
// missing meta.json as an error.
func mapLookupError(id string, err error) error {
	switch {
	case errors.Is(err, cg.ErrUnknownRunID):
		return fmt.Errorf("unknown run id: %s", id)
	case errors.Is(err, cg.ErrIncompleteRun):
		return fmt.Errorf("incomplete run: %s (missing meta.json)", id)
	case errors.Is(err, cg.ErrFailedRun):
		return fmt.Errorf("failed run: %s (start failed; use cg_meta for debug info)", id)
	default:
		return err
	}
}

// metaFieldsFrom builds a metaFields populated from m. Returned by value; the
// caller embeds it into the surrounding output struct.
func metaFieldsFrom(m *cg.Meta) metaFields {
	started := m.StartedAt
	finished := m.FinishedAt
	dur := m.DurationMs
	exit := m.ExitCode
	stdoutLines := m.StdoutLines
	stderrLines := m.StderrLines
	f := metaFields{
		Command:     m.Command,
		Cwd:         m.Cwd,
		StartedAt:   &started,
		FinishedAt:  &finished,
		DurationMs:  &dur,
		ExitCode:    &exit,
		StdoutLines: &stdoutLines,
		StderrLines: &stderrLines,
		Usage:       m.Usage,
	}
	if m.Signal != nil {
		sig := *m.Signal
		f.Signal = &sig
	}
	return f
}

// metaFieldsFromStart builds the shared start-time fields from an in-flight run.
func metaFieldsFromStart(si *cg.StartInfo) metaFields {
	started := si.StartedAt
	return metaFields{
		Command:   si.Command,
		Cwd:       si.Cwd,
		StartedAt: &started,
	}
}
