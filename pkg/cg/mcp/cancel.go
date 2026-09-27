package mcp

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/model"
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
		Description: "Signal a capture run's process group. Sends signal (default SIGTERM) to the run started by this server. Already-finished or already-gone runs return {signaled: false, finished: true} without error; unknown IDs are a tool error, and so are in-flight runs started by the shell, by another server, or by this server before a restart. With escalate_after_ms > 0, waits up to that long for the child to exit and sends escalate_signal (default SIGKILL) if it is still running. A pool ID signals the pool supervisor instead: SIGTERM stops scheduling and lets in-flight runs finish, SIGINT additionally cancels them, and anything else (including the SIGKILL escalation default) abandons the pool.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in cancelInput) (*mcpsdk.CallToolResult, cancelOutput, error) {
		return handleCancel(ctx, reg, in)
	})
}

func handleCancel(ctx context.Context, reg *runRegistry, in cancelInput) (*mcpsdk.CallToolResult, cancelOutput, error) {
	sig, err := model.ParseSignal(in.Signal, syscall.SIGTERM)
	if err != nil {
		return nil, cancelOutput{}, fmt.Errorf("signal: %w", err)
	}
	escSig, err := model.ParseSignal(in.EscalateSignal, syscall.SIGKILL)
	if err != nil {
		return nil, cancelOutput{}, fmt.Errorf("escalate_signal: %w", err)
	}

	out := cancelOutput{ID: in.ID, Signal: int(sig)}

	dir, lerr := model.LookupRunDir(in.ID)
	switch {
	case errors.Is(lerr, model.ErrUnknownRunID):
		return nil, cancelOutput{}, fmt.Errorf("unknown run id: %s", in.ID)
	case lerr == nil:
		// Finished run: the thing you wanted dead is already dead.
		out.Finished = true
		return nil, out, nil
	case errors.Is(lerr, model.ErrFailedRun):
		// Child never started; nothing to cancel.
		out.Finished = true
		return nil, out, nil
	case !errors.Is(lerr, model.ErrIncompleteRun):
		return nil, cancelOutput{}, lerr
	}

	entry, ok := reg.lookup(in.ID)
	if !ok {
		return cancelUntracked(in, out, dir)
	}
	if entry.pool {
		return handlePoolCancel(ctx, reg, in, out, dir, entry.pid, sig, escSig)
	}

	pid := entry.pid
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

// cancelUntracked answers for an in-flight-looking run or pool that this server
// is not tracking. It reports finished when the disk says so, since that needs
// no signal and a forged record can do no harm. Anything else is refused: the
// only pid on hand would come from a file under the capture root.
//
// The finished checks also cover a run that completed between LookupRunDir and
// the registry lookup, since the janitor drops the entry once Done closes.
func cancelUntracked(in cancelInput, out cancelOutput, dir string) (*mcpsdk.CallToolResult, cancelOutput, error) {
	if m, err := model.ReadPoolManifest(dir); err == nil {
		out.Pool = true
		if m.FinishedAt != nil {
			out.Finished = true
			return nil, out, nil
		}
	}

	if _, err := model.LookupRunDir(in.ID); err == nil || errors.Is(err, model.ErrFailedRun) || model.RunLockReleased(dir) {
		out.Finished = true
		return nil, out, nil
	}

	return nil, cancelOutput{}, fmt.Errorf("cannot cancel %s: not started by this server", in.ID)
}

// handlePoolCancel signals the pool supervisor with sig. The pid is positive on
// purpose: the supervisor is its own session leader and members run in their
// own sessions, so a group signal would reach nothing else anyway, and the
// protocol is defined on the supervisor process. Escalation waits for the pool
// to finish and then signals the supervisor again with escSig.
func handlePoolCancel(ctx context.Context, reg *runRegistry, in cancelInput, out cancelOutput, dir string, pid int, sig, escSig syscall.Signal) (*mcpsdk.CallToolResult, cancelOutput, error) {
	out.Pool = true

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
