package mcp

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg"
)

// cancelInput is the argument shape for `cg_cancel`.
type cancelInput struct {
	ID              string `json:"id" jsonschema:"capture run ID"`
	Signal          string `json:"signal,omitempty" jsonschema:"signal to send to the run's process group: SIGTERM (default), SIGINT, SIGKILL, or a numeric value"`
	EscalateAfterMs int    `json:"escalate_after_ms,omitempty" jsonschema:"if > 0, wait this long for the child to exit, then send escalate_signal if it is still running; 0 or unset means fire-and-forget"`
	EscalateSignal  string `json:"escalate_signal,omitempty" jsonschema:"signal to send if the child is still running after escalate_after_ms (default SIGKILL); same accepted values as signal"`
}

// cancelOutput is the result shape for `cg_cancel`. EscalateSignal is present
// only when an escalation signal was actually sent. Pool marks that id named a
// pool and the signal went to its supervisor rather than a process group.
type cancelOutput struct {
	ID             string `json:"id"`
	Pool           bool   `json:"pool,omitempty"`
	Signaled       bool   `json:"signaled"`
	Signal         int    `json:"signal"`
	Escalated      bool   `json:"escalated"`
	EscalateSignal int    `json:"escalate_signal,omitempty"`
	Finished       bool   `json:"finished"`
}

func registerCancel(s *mcpsdk.Server, reg *runRegistry) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_cancel",
		Description: "Signal a capture run's process group. Sends signal (default SIGTERM) to the run started by this server. Already-finished or already-gone runs return {signaled: false, finished: true} without error; unknown IDs are a tool error. With escalate_after_ms > 0, waits up to that long for the child to exit and sends escalate_signal (default SIGKILL) if it is still running. A pool ID signals the pool supervisor instead: SIGTERM stops scheduling and lets in-flight runs finish, SIGINT additionally cancels them, and anything else (including the SIGKILL escalation default) abandons the pool.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in cancelInput) (*mcpsdk.CallToolResult, cancelOutput, error) {
		return handleCancel(ctx, reg, in)
	})
}

func handleCancel(ctx context.Context, reg *runRegistry, in cancelInput) (*mcpsdk.CallToolResult, cancelOutput, error) {
	sig, err := cg.ParseSignal(in.Signal, syscall.SIGTERM)
	if err != nil {
		return nil, cancelOutput{}, fmt.Errorf("signal: %w", err)
	}
	escSig, err := cg.ParseSignal(in.EscalateSignal, syscall.SIGKILL)
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

	// A directory without meta.json is either an in-flight run or a pool; the
	// manifest's presence is what distinguishes the two.
	if m, perr := cg.ReadPoolManifest(dir); perr == nil {
		return handlePoolCancel(ctx, reg, in, out, dir, m, sig, escSig)
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

// handlePoolCancel signals the pool supervisor with sig. The pid is positive on
// purpose: the supervisor is its own session leader and members run in their
// own sessions, so a group signal would reach nothing else anyway, and the
// protocol is defined on the supervisor process. Escalation waits for the pool
// to finish and then signals the supervisor again with escSig.
func handlePoolCancel(ctx context.Context, reg *runRegistry, in cancelInput, out cancelOutput, dir string, m *cg.PoolManifest, sig, escSig syscall.Signal) (*mcpsdk.CallToolResult, cancelOutput, error) {
	out.Pool = true

	pid, finished, err := cg.PoolSupervisorPid(dir, m)
	if err != nil {
		return nil, cancelOutput{}, fmt.Errorf("cannot cancel %s: %w", in.ID, err)
	}
	if finished {
		out.Finished = true
		return nil, out, nil
	}

	if kerr := syscall.Kill(pid, sig); kerr != nil {
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

	finished, werr := awaitPoolFinish(ctx, reg, in.ID, dir, time.Duration(in.EscalateAfterMs)*time.Millisecond)
	if werr != nil {
		return nil, cancelOutput{}, werr
	}
	if finished {
		out.Finished = true
		return nil, out, nil
	}

	if kerr := syscall.Kill(pid, escSig); kerr != nil && !errors.Is(kerr, syscall.ESRCH) {
		return nil, cancelOutput{}, fmt.Errorf("escalating %s: %w", in.ID, kerr)
	}
	out.Escalated = true
	out.EscalateSignal = int(escSig)
	return nil, out, nil
}
