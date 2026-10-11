package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/model"
)

// defaultStatsExitCode keeps failed runs out of a duration estimate unless the caller asks for them.
const defaultStatsExitCode = "0"

// statsInput is the argument shape for `cg_stats`.
type statsInput struct {
	Command  []string `json:"command" jsonschema:"the command and its arguments, exactly as passed to cg_run"`
	Cwd      string   `json:"cwd,omitempty" jsonschema:"working directory the runs started in; inherits the server's cwd when empty"`
	Since    string   `json:"since,omitempty" jsonschema:"only count runs started at or after TIME: a relative duration meaning ago (e.g. 4h, 7d), an RFC3339 timestamp, or a bare YYYY-MM-DD date at local midnight"`
	ExitCode string   `json:"exit_code,omitempty" jsonschema:"only count runs with this exit code: N (equals), !=N, >=N, >N, <N, or <=N; signaled runs never count; default 0"`
	Samples  int      `json:"samples,omitempty" jsonschema:"count at most this many of the most recent runs per entry; default 20, max 1000"`
	Pool     string   `json:"pool,omitempty" jsonschema:"pool membership: any (default), none for standalone runs only, or only for pooled runs only"`
}

func registerStats(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_stats",
		Description: "Report how long earlier runs of a command took, from cg's duration history. Returns the same JSON as `cg stats -o json`. The first match is the exact command. Each shorter prefix of it that has samples follows, longest first. Durations are in milliseconds. Pooled and standalone runs are separate entries. An exact match with no samples has count 0 rather than an error.",
	}, handleStats)
}

func handleStats(_ context.Context, _ *mcpsdk.CallToolRequest, in statsInput) (*mcpsdk.CallToolResult, model.StatsResult, error) {
	filters := model.StatsFilters{
		Cwd:      in.Cwd,
		Since:    in.Since,
		ExitCode: in.ExitCode,
		Samples:  in.Samples,
		Pool:     in.Pool,
	}
	// Omitted inputs arrive as zero values. Fill in the CLI's flag defaults so both surfaces agree.
	if filters.ExitCode == "" {
		filters.ExitCode = defaultStatsExitCode
	}
	if filters.Samples == 0 {
		filters.Samples = model.DefaultStatsSamples
	}
	if filters.Pool == "" {
		filters.Pool = model.StatsPoolAny
	}

	query, err := filters.Query(in.Command, time.Now())
	if err != nil {
		return nil, model.StatsResult{}, err
	}

	history, err := model.OpenHistory()
	if err != nil {
		return nil, model.StatsResult{}, fmt.Errorf("opening history: %w", err)
	}

	result, err := history.QueryStats(query)
	if errors.Is(err, model.ErrInvalidStatsQuery) {
		return nil, model.StatsResult{}, err
	}
	if err != nil {
		return nil, model.StatsResult{}, fmt.Errorf("querying history: %w", err)
	}

	return nil, *result, nil
}
