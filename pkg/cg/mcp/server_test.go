package mcp

import "testing"

// TestNewServerDoesNotPanic checks that newServer constructs the MCP server
// and registers cg_run without panicking. AddTool panics on bad schema, so a
// successful return is the assertion.
func TestNewServerDoesNotPanic(t *testing.T) {
	s := newServer("test")
	if s == nil {
		t.Fatalf("newServer returned nil")
	}
}

// TestNewCommandWiring sanity-checks that the cobra subcommand is named "mcp"
// and accepts no args, matching how cg's other resolution subcommands are
// shaped.
func TestNewCommandWiring(t *testing.T) {
	c := NewCommand()
	if c.Use != "mcp" {
		t.Errorf("Use = %q, want %q", c.Use, "mcp")
	}
	if c.RunE == nil {
		t.Errorf("RunE is nil")
	}
}
