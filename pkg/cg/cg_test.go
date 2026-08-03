package cg

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// runCgCommand executes the cg cobra command with the given args and returns
// the captured output and error.
func runCgCommand(args ...string) (string, error) {
	var buf bytes.Buffer
	cmd := NewCommand()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestUnknownSubcommandErrors(t *testing.T) {
	_, err := runCgCommand("prnue")
	if err == nil {
		t.Fatal("expected unknown command error")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "unknown command")
	}
}

func TestBareCgDashDashErrors(t *testing.T) {
	_, err := runCgCommand("--", "echo", "hi")
	if err == nil {
		t.Fatal("expected error; bare cg no longer execs")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != 2 {
		t.Errorf("exit code = %d, want 2", exitErr.Code)
	}
}
