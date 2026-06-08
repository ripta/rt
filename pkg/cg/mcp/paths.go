package mcp

import (
	"context"
	"errors"
	"path/filepath"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg"
)

// pathsInput is the argument shape for `cg_paths`.
type pathsInput struct {
	ID string `json:"id" jsonschema:"capture run ID"`
}

// pathsOutput is the result shape for `cg_paths`. The meta path is included
// even when meta.json does not yet exist; callers waiting on an in-flight run
// can poll the same path.
type pathsOutput struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Meta   string `json:"meta"`
}

func registerPaths(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_paths",
		Description: "Return absolute paths for a capture run's stdout, stderr, and meta.json files. Works for in-flight runs (meta.json may not exist yet).",
	}, handlePaths)
}

func handlePaths(_ context.Context, _ *mcpsdk.CallToolRequest, in pathsInput) (*mcpsdk.CallToolResult, pathsOutput, error) {
	dir, err := cg.LookupRunDir(in.ID)
	if err != nil && !errors.Is(err, cg.ErrIncompleteRun) {
		return nil, pathsOutput{}, mapLookupError(in.ID, err)
	}
	return nil, pathsOutput{
		Stdout: filepath.Join(dir, "stdout"),
		Stderr: filepath.Join(dir, "stderr"),
		Meta:   filepath.Join(dir, cg.MetaFilename),
	}, nil
}
