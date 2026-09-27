// Package mcp implements `cg mcp`, a stdio MCP server that exposes cg's
// capture-run model as native MCP tools. The server is a thin wrapper over the
// same on-disk capture model the shell subcommands use, so a run started via
// `cg --capture -- cmd` is resolvable by the MCP tools and vice versa.
package mcp

import (
	"fmt"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
	"github.com/ripta/rt/pkg/version"
)

// serverOptions holds the flags for `cg mcp`.
type serverOptions struct {
	blindlyAllow  bool
	projectConfig []string
}

// NewCommand returns the `cg mcp` cobra subcommand.
func NewCommand() *cobra.Command {
	opts := &serverOptions{}
	c := &cobra.Command{
		Use:           "mcp",
		Short:         "Start an MCP stdio server exposing cg's capture-run tools",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServer(cmd, opts)
		},
	}
	c.Flags().BoolVar(&opts.blindlyAllow, "blindly-allow", false,
		"disable the cg_run approval gate for this server process (forces allow-all)")
	c.Flags().StringSliceVar(&opts.projectConfig, "project-config", nil,
		"project-specific approval rules files, relative to the project root, tried in order (first existing wins; default "+strings.Join(approve.DefaultProjectFiles(), ", ")+")")
	return c
}

func runServer(cmd *cobra.Command, opts *serverOptions) error {
	v := version.GetString()
	if v == "" {
		v = "unknown"
	}

	store, err := approve.Load(approve.LoadOptions{ProjectFiles: opts.projectConfig})
	if err != nil {
		return fmt.Errorf("loading approval rules: %w", err)
	}
	g := &gate{store: store, blindlyAllow: opts.blindlyAllow}

	sessionID, err := model.GenerateSessionID()
	if err != nil {
		return fmt.Errorf("generating session id: %w", err)
	}

	s := newServer(v, time.Now(), sessionID, g)
	return s.Run(cmd.Context(), &mcpsdk.StdioTransport{})
}

// serverInstructions steers clients away from polling long runs. Claude Code moves an MCP call that
// outlives its auto-background threshold into a background task and notifies the model when it
// completes, so one blocking call costs a single turn where a cg_wait loop costs one per timeout.
const serverInstructions = `For a long-running command, call cg_run or cg_run_many synchronously and set wait_timeout_ms above the expected duration. The default is 60000, which is shorter than most builds and test suites.

If the client moves the call to the background, keep working or end the turn. Do not poll with cg_wait. The client delivers the result when the run finishes.

Use wait=false only to run other work alongside the command. Use cg_wait to resume a run whose original call was lost, such as after a client restart. Runs are recorded on disk, so the ID stays valid.`

// newServer constructs a fully-registered MCP server. Pulled out so tests can
// drive it without going through stdio. startedAt is the server's start time
// and sessionID names this server process; both are reported by cg_info and
// fixed for the process's lifetime.
func newServer(v string, startedAt time.Time, sessionID string, g *gate) *mcpsdk.Server {
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "cg", Version: v}, &mcpsdk.ServerOptions{Instructions: serverInstructions})
	reg := newRunRegistry()
	registerRun(s, reg, g, sessionID)
	registerRunMany(s, reg, g, sessionID)
	registerList(s)
	registerMeta(s)
	registerWait(s, reg)
	registerCancel(s, reg)
	registerPaths(s)
	registerStreams(s)
	registerGrep(s)
	registerPrune(s)
	registerNotes(s)
	registerElicitTest(s)
	registerInfo(s, v, startedAt, sessionID)
	return s
}
