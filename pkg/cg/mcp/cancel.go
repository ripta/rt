package mcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg"
)

// maxSignalNumber bounds numeric signal inputs. Real signal numbers fit well
// under this; the cap rejects obvious garbage without enumerating every
// platform's signal table.
const maxSignalNumber = 64

// cancelInput is the argument shape for `cg_cancel`.
type cancelInput struct {
	ID              string `json:"id" jsonschema:"capture run ID"`
	Signal          string `json:"signal,omitempty" jsonschema:"signal to send to the run's process group: SIGTERM (default), SIGINT, SIGKILL, or a numeric value"`
	EscalateAfterMs int    `json:"escalate_after_ms,omitempty" jsonschema:"if > 0, wait this long for the child to exit, then send escalate_signal if it is still running; 0 or unset means fire-and-forget"`
	EscalateSignal  string `json:"escalate_signal,omitempty" jsonschema:"signal to send if the child is still running after escalate_after_ms (default SIGKILL); same accepted values as signal"`
}

// cancelOutput is the result shape for `cg_cancel`. EscalateSignal is present
// only when an escalation signal was actually sent.
type cancelOutput struct {
	ID             string `json:"id"`
	Signaled       bool   `json:"signaled"`
	Signal         int    `json:"signal"`
	Escalated      bool   `json:"escalated"`
	EscalateSignal int    `json:"escalate_signal,omitempty"`
	Finished       bool   `json:"finished"`
}

func registerCancel(s *mcpsdk.Server, reg *runRegistry) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_cancel",
		Description: "Signal a capture run's process group. Sends signal (default SIGTERM) to the run started by this server. Already-finished or already-gone runs return {signaled: false, finished: true} without error; unknown IDs are a tool error. With escalate_after_ms > 0, waits up to that long for the child to exit and sends escalate_signal (default SIGKILL) if it is still running.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in cancelInput) (*mcpsdk.CallToolResult, cancelOutput, error) {
		return handleCancel(ctx, reg, in)
	})
}

func handleCancel(ctx context.Context, reg *runRegistry, in cancelInput) (*mcpsdk.CallToolResult, cancelOutput, error) {
	sig, err := parseSignal(in.Signal, syscall.SIGTERM)
	if err != nil {
		return nil, cancelOutput{}, fmt.Errorf("signal: %w", err)
	}
	escSig, err := parseSignal(in.EscalateSignal, syscall.SIGKILL)
	if err != nil {
		return nil, cancelOutput{}, fmt.Errorf("escalate_signal: %w", err)
	}

	out := cancelOutput{ID: in.ID, Signal: int(sig)}

	dir, lerr := cg.LookupRunDir(in.ID)
	switch {
	case errors.Is(lerr, cg.ErrUnknownRunID):
		return nil, cancelOutput{}, fmt.Errorf("unknown run id: %s", in.ID)
	case lerr == nil:
		// Finished run: the thing you wanted dead is already dead.
		out.Finished = true
		return nil, out, nil
	case errors.Is(lerr, cg.ErrFailedRun):
		// Child never started; nothing to cancel.
		out.Finished = true
		return nil, out, nil
	case !errors.Is(lerr, cg.ErrIncompleteRun):
		return nil, cancelOutput{}, lerr
	}

	pid, perr := cg.ReadPidFile(dir)
	if perr != nil {
		return nil, cancelOutput{}, fmt.Errorf("cannot cancel %s: no pid recorded for this run: %w", in.ID, perr)
	}

	if kerr := syscall.Kill(-pid, sig); kerr != nil {
		if errors.Is(kerr, syscall.ESRCH) {
			out.Finished = true
			return nil, out, nil
		}
		return nil, cancelOutput{}, fmt.Errorf("signalling %s: %w", in.ID, kerr)
	}
	out.Signaled = true

	if in.EscalateAfterMs <= 0 {
		return nil, out, nil
	}

	finished, werr := awaitFinish(ctx, reg, in.ID, time.Duration(in.EscalateAfterMs)*time.Millisecond)
	if werr != nil {
		return nil, cancelOutput{}, werr
	}
	if finished {
		out.Finished = true
		return nil, out, nil
	}

	if kerr := syscall.Kill(-pid, escSig); kerr != nil && !errors.Is(kerr, syscall.ESRCH) {
		return nil, cancelOutput{}, fmt.Errorf("escalating %s: %w", in.ID, kerr)
	}
	out.Escalated = true
	out.EscalateSignal = int(escSig)
	return nil, out, nil
}

// parseSignal maps a signal name or numeric string onto a syscall.Signal. An
// empty input returns def. Accepted names are SIGTERM, SIGINT, and SIGKILL;
// numeric values in (0, maxSignalNumber] are accepted directly, which covers
// signals like SIGQUIT without enumerating every name.
func parseSignal(name string, def syscall.Signal) (syscall.Signal, error) {
	s := strings.TrimSpace(name)
	if s == "" {
		return def, nil
	}
	switch strings.ToUpper(s) {
	case "SIGTERM":
		return syscall.SIGTERM, nil
	case "SIGINT":
		return syscall.SIGINT, nil
	case "SIGKILL":
		return syscall.SIGKILL, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 || n > maxSignalNumber {
			return 0, fmt.Errorf("numeric signal out of range: %d", n)
		}
		return syscall.Signal(n), nil
	}
	return 0, fmt.Errorf("unsupported signal: %q (want SIGTERM, SIGINT, SIGKILL, or a number)", name)
}
