// Package mcp implements `cg mcp`, a stdio MCP server that exposes cg's
// capture-run model as native MCP tools. The server is a thin wrapper over the
// same on-disk capture model the shell subcommands use, so a run started via
// `cg --capture -- cmd` is resolvable by the MCP tools and vice versa.
package mcp

import (
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/version"
)

// NewCommand returns the `cg mcp` cobra subcommand.
func NewCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "mcp",
		Short:         "Start an MCP stdio server exposing cg's capture-run tools",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          runServer,
	}
}

func runServer(cmd *cobra.Command, _ []string) error {
	v := version.GetString()
	if v == "" {
		v = "unknown"
	}
	s := newServer(v)
	return s.Run(cmd.Context(), &mcpsdk.StdioTransport{})
}

// newServer constructs a fully-registered MCP server. Pulled out so tests can
// drive it without going through stdio.
func newServer(v string) *mcpsdk.Server {
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "cg", Version: v}, nil)
	reg := newRunRegistry()
	registerRun(s, reg)
	registerList(s)
	registerMeta(s)
	registerWait(s, reg)
	registerCancel(s, reg)
	registerPaths(s)
	registerStreams(s)
	registerPrune(s)
	return s
}
