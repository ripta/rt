package mcp

import (
	"context"
	"fmt"
	"os"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/version"
)

// infoEnvAllowlist bounds which environment variables cg_info may report. It is
// a curated, non-sensitive diagnostic set; variables outside it are never read,
// so secrets cannot leak through this tool by mistake.
var infoEnvAllowlist = []string{
	"HOME",
	"LANG",
	"PATH",
	"PWD",
	"SHELL",
	"TERM",
	"TMPDIR",
	"USER",
}

// infoInput is the argument shape for `cg_info`. The tool takes no arguments.
type infoInput struct{}

// infoBuild holds the version and build diagnostics for the running server.
type infoBuild struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	Dirty     bool   `json:"dirty"`
	GoVersion string `json:"go_version,omitempty"`
}

// infoOutput is the result shape for `cg_info`. Env contains only the
// allowlisted variables that are actually set in the server's environment.
//
// Cwd is the server's working directory. The server never changes directory
// over its lifetime, so this is fixed at process start; it is the directory a
// cg_run inherits when its own cwd argument is empty.
//
// SessionID names this server process. It is minted at start and stamped on
// every run and pool the server spawns, so a listing can be scoped to one
// server's runs.
type infoOutput struct {
	StartedAt time.Time         `json:"started_at"`
	UptimeMs  int64             `json:"uptime_ms"`
	Cwd       string            `json:"cwd"`
	SessionID string            `json:"session_id"`
	Build     infoBuild         `json:"build"`
	Env       map[string]string `json:"env"`
}

func registerInfo(s *mcpsdk.Server, v string, startedAt time.Time, sessionID string) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_info",
		Description: "Return diagnostics about the running cg MCP server: its start time and uptime, version and build info, its working directory (fixed at start; the directory a cg_run inherits when its cwd is empty), and the set values of a curated allowlist of environment variables.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ infoInput) (*mcpsdk.CallToolResult, infoOutput, error) {
		out, err := collectInfo(v, startedAt, sessionID, time.Now())
		return nil, out, err
	})
}

// collectInfo assembles the cg_info response. The clock is threaded in as now
// so tests can assert a deterministic uptime.
func collectInfo(v string, startedAt time.Time, sessionID string, now time.Time) (infoOutput, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return infoOutput{}, fmt.Errorf("resolving working directory: %w", err)
	}
	return infoOutput{
		StartedAt: startedAt,
		UptimeMs:  now.Sub(startedAt).Milliseconds(),
		Cwd:       cwd,
		SessionID: sessionID,
		Build:     buildInfo(v),
		Env:       allowedEnv(),
	}, nil
}

// buildInfo reports the advertised version string alongside the vcs revision,
// dirty flag, and Go toolchain version pulled from the embedded build info.
// The version string mirrors what the server advertises to the MCP client; the
// remaining fields are best-effort and stay empty when build info is absent.
func buildInfo(v string) infoBuild {
	b := infoBuild{Version: v}
	vi, err := version.Get()
	if err != nil {
		return b
	}
	b.GoVersion = vi.BuildInfo.GoVersion
	for _, s := range vi.BuildInfo.Settings {
		switch s.Key {
		case "vcs.revision":
			b.Commit = s.Value
		case "vcs.modified":
			b.Dirty = s.Value == "true"
		}
	}
	return b
}

// allowedEnv returns the allowlisted environment variables that are set,
// preserving their values verbatim. Unset variables are omitted.
func allowedEnv() map[string]string {
	env := make(map[string]string)
	for _, k := range infoEnvAllowlist {
		if val, ok := os.LookupEnv(k); ok {
			env[k] = val
		}
	}
	return env
}
