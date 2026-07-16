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

// waitOutput is the result shape for `cg_wait`. Finished is always present.
// For a run ID the embedded meta fields are populated only when Finished is
// true. For a pool ID the embedded pool summary is populated instead, partial
// on timeout.
type waitOutput struct {
	ID       string `json:"id"`
	Finished bool   `json:"finished"`
	metaFields
	poolSummary
}

func registerWait(s *mcpsdk.Server, reg *runRegistry) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_wait",
		Description: "Block until a capture run or pool finishes or timeout_ms elapses. For a run, returns {id, finished: true, ...meta} on completion or {id, finished: false} on timeout. For a pool, returns the same summary as cg_run_many, partial on timeout. Unknown ID is a tool error.",
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
	case errors.Is(err, cg.ErrFailedRun):
		return nil, waitOutput{ID: in.ID, Finished: true}, nil
	case !errors.Is(err, cg.ErrIncompleteRun):
		return nil, waitOutput{}, err
	}

	// A directory without meta.json is either an in-flight run or a pool; the
	// manifest's presence is what distinguishes the two.
	if m, perr := cg.ReadPoolManifest(dir); perr == nil {
		return handlePoolWait(ctx, reg, in.ID, dir, m, time.Duration(timeoutMs)*time.Millisecond)
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
			if errors.Is(e, cg.ErrFailedRun) {
				return true, nil
			}
			if !errors.Is(e, cg.ErrIncompleteRun) {
				return false, e
			}
		}
	}
}

// handlePoolWait blocks until the pool finishes or the timeout elapses, then
// returns the same summary as the sync cg_run_many call, built with the
// default excerpt size. A timeout returns finished: false with the partial
// summary; the pool keeps running.
func handlePoolWait(ctx context.Context, reg *runRegistry, id, dir string, m *cg.PoolManifest, timeout time.Duration) (*mcpsdk.CallToolResult, waitOutput, error) {
	finished := m.FinishedAt != nil
	if !finished {
		f, err := awaitPoolFinish(ctx, reg, id, dir, timeout)
		if err != nil {
			return nil, waitOutput{}, err
		}
		finished = f
	}

	summary, err := buildPoolSummary(dir, defaultExcerptBytes)
	if err != nil {
		return nil, waitOutput{}, err
	}
	return nil, waitOutput{ID: id, Finished: finished, poolSummary: summary}, nil
}

// awaitPoolFinish mirrors awaitFinish for pools: the registry Done channel
// when this server started the pool, and manifest polling otherwise, since a
// pool never grows a meta.json. A false return with a nil error means the
// timeout fired.
func awaitPoolFinish(ctx context.Context, reg *runRegistry, id, dir string, timeout time.Duration) (bool, error) {
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
			m, err := cg.ReadPoolManifest(dir)
			if err != nil {
				return false, fmt.Errorf("reading pool.json for %s: %w", id, err)
			}
			if m.FinishedAt != nil {
				return true, nil
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
