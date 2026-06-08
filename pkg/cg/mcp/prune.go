package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg"
)

const defaultPruneKeep = 50

// pruneInput is the argument shape for `cg_prune`.
type pruneInput struct {
	Keep      *int   `json:"keep,omitempty" jsonschema:"keep N most recent runs by mtime; default 50; mutually exclusive with older_than"`
	OlderThan string `json:"older_than,omitempty" jsonschema:"evict runs older than DUR (e.g., 7d, 2h, 90m); mutually exclusive with keep"`
	DryRun    bool   `json:"dry_run,omitempty" jsonschema:"report what would be removed without removing"`
}

// pruneOutput is the result shape for `cg_prune`.
type pruneOutput struct {
	Removed []string `json:"removed"`
	DryRun  bool     `json:"dry_run"`
}

func registerPrune(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_prune",
		Description: "Evict capture runs from $TMPDIR/cg/. Either keeps the N most recent runs by mtime or removes runs older than a duration. Use dry_run to preview without removing.",
	}, handlePrune)
}

func handlePrune(_ context.Context, _ *mcpsdk.CallToolRequest, in pruneInput) (*mcpsdk.CallToolResult, pruneOutput, error) {
	if in.Keep != nil && in.OlderThan != "" {
		return nil, pruneOutput{}, fmt.Errorf("keep and older_than are mutually exclusive")
	}

	opts := cg.PruneOptions{DryRun: in.DryRun}
	if in.OlderThan != "" {
		d, err := cg.ParsePruneDuration(in.OlderThan)
		if err != nil {
			return nil, pruneOutput{}, fmt.Errorf("invalid older_than: %w", err)
		}
		opts.UseOlderThan = true
		opts.OlderThan = d
	} else if in.Keep != nil {
		opts.Keep = *in.Keep
	} else {
		opts.Keep = defaultPruneKeep
	}

	removed, err := cg.PruneRuns(opts)
	if err != nil {
		return nil, pruneOutput{}, err
	}
	if removed == nil {
		removed = []string{}
	}
	return nil, pruneOutput{Removed: removed, DryRun: in.DryRun}, nil
}
