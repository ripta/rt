package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg"
)

const waitPollInterval = 100 * time.Millisecond

// waitInput is the argument shape for `cg_wait`.
type waitInput struct {
	ID        string `json:"id" jsonschema:"capture run ID"`
	TimeoutMs int    `json:"timeout_ms,omitempty" jsonschema:"how long to block before returning finished=false (default 60000)"`
}

// waitOutput is the result shape for `cg_wait`. Finished is always present;
// the embedded meta fields are populated only when Finished is true.
type waitOutput struct {
	ID       string `json:"id"`
	Finished bool   `json:"finished"`
	metaFields
}

func registerWait(s *mcpsdk.Server, reg *runRegistry) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_wait",
		Description: "Block until a capture run finishes or timeout_ms elapses. Returns {id, finished: true, ...meta} on completion or {id, finished: false} on timeout. Unknown ID is a tool error.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in waitInput) (*mcpsdk.CallToolResult, waitOutput, error) {
		return handleWait(ctx, reg, in)
	})
}

func handleWait(ctx context.Context, reg *runRegistry, in waitInput) (*mcpsdk.CallToolResult, waitOutput, error) {
	timeoutMs := in.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = defaultWaitTimeoutMs
	}

	dir, err := cg.LookupRunDir(in.ID)
	switch {
	case errors.Is(err, cg.ErrUnknownRunID):
		return nil, waitOutput{}, fmt.Errorf("unknown run id: %s", in.ID)
	case err == nil:
		out, ferr := finishedWaitOutput(in.ID, dir)
		return nil, out, ferr
	case !errors.Is(err, cg.ErrIncompleteRun):
		return nil, waitOutput{}, err
	}

	finished, werr := awaitFinish(ctx, reg, in.ID, time.Duration(timeoutMs)*time.Millisecond)
	if werr != nil {
		return nil, waitOutput{}, werr
	}
	if !finished {
		return nil, waitOutput{ID: in.ID, Finished: false}, nil
	}
	out, ferr := finishedWaitOutput(in.ID, dir)
	return nil, out, ferr
}

// awaitFinish blocks until the run identified by id finishes, the timeout
// elapses, or ctx is cancelled. It mirrors the two-path strategy cg_wait and
// cg_cancel both need: the registry Done channel when this server started the
// run, and filesystem polling otherwise. The caller must have already
// confirmed the run is in flight; awaitFinish only watches for the transition
// to finished. A false return with a nil error means the timeout fired.
func awaitFinish(ctx context.Context, reg *runRegistry, id string, timeout time.Duration) (bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	if reg != nil {
		if done, ok := reg.Done(id); ok {
			select {
			case <-done:
				return true, nil
			case <-timer.C:
				return false, nil
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}
	}

	ticker := time.NewTicker(waitPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			return false, nil
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
			_, e := cg.LookupRunDir(id)
			if e == nil {
				return true, nil
			}
			if errors.Is(e, cg.ErrUnknownRunID) {
				return false, fmt.Errorf("unknown run id: %s", id)
			}
			if !errors.Is(e, cg.ErrIncompleteRun) {
				return false, e
			}
		}
	}
}

// finishedWaitOutput reads meta.json from dir and builds the populated wait
// output. A read error surfaces as an MCP error rather than a finished:false
// response — the caller asked us to wait for finish, and the dir clearly
// transitioned to that state.
func finishedWaitOutput(id, dir string) (waitOutput, error) {
	m, err := cg.ReadMeta(dir)
	if err != nil {
		return waitOutput{}, fmt.Errorf("reading meta.json for %s: %w", id, err)
	}
	return waitOutput{
		ID:         m.ID,
		Finished:   true,
		metaFields: metaFieldsFrom(m),
	}, nil
}
