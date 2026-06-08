// Package mcp implements `cg mcp`, a stdio MCP server that exposes cg's
// capture-run model as native MCP tools. The server is a thin wrapper over the
// same on-disk capture model the shell subcommands use, so a run started via
// `cg --capture -- cmd` is resolvable by the MCP tools and vice versa.
package mcp

import (
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/version"
)

// serverOptions holds the flags for `cg mcp`.
type serverOptions struct {
	blindlyAllow  bool
	projectConfig string
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
	c.Flags().StringVar(&opts.projectConfig, "project-config", "",
		"project-specific approval rules file, relative to the project root (default "+approve.DefaultProjectFile+")")
	return c
}

func runServer(cmd *cobra.Command, opts *serverOptions) error {
	v := version.GetString()
	if v == "" {
		v = "unknown"
	}

	store, err := approve.Load(approve.LoadOptions{ProjectFile: opts.projectConfig})
	if err != nil {
		return fmt.Errorf("loading approval rules: %w", err)
	}
	g := &gate{store: store, blindlyAllow: opts.blindlyAllow}

	s := newServer(v, g)
	return s.Run(cmd.Context(), &mcpsdk.StdioTransport{})
}

// newServer constructs a fully-registered MCP server. Pulled out so tests can
// drive it without going through stdio.
func newServer(v string, g *gate) *mcpsdk.Server {
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "cg", Version: v}, nil)
	reg := newRunRegistry()
	registerRun(s, reg, g)
	registerList(s)
	registerMeta(s)
	registerWait(s, reg)
	registerCancel(s, reg)
	registerPaths(s)
	registerStreams(s)
	registerGrep(s)
	registerPrune(s)
	registerElicitTest(s)
	return s
}
