package mcp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/model"
)

const (
	// maxPoolRuns caps the pool's total run count (commands × repeat), the real
	// resource knob: run directories, child work, and listing scan cost.
	maxPoolRuns = 500

	// maxPoolParallelism caps concurrency; each worker slot is a child plus a
	// per-run supervisor.
	maxPoolParallelism = 32

	// maxPoolCommands caps distinct argvs. The gate elicits per distinct
	// unapproved argv, so a product-only bound would let one malformed call
	// fire hundreds of approval prompts before any side effect.
	maxPoolCommands = 100

	// poolExcerptBudget caps the total excerpt bytes across a pool's summary,
	// making the worst case a constant instead of a function of pool size.
	poolExcerptBudget = 16384
)

// runManyInput is the argument shape for `cg_run_many`.
type runManyInput struct {
	Commands      [][]string        `json:"commands" jsonschema:"list of argvs to execute; index 0 of each is the program"`
	Repeat        int               `json:"repeat,omitempty" jsonschema:"how many times each command runs (default 1)"`
	Parallelism   int               `json:"parallelism,omitempty" jsonschema:"worker count (default 1, max 32); with 1 runs execute in listed order with repeats consecutive"`
	OnError       string            `json:"on_error,omitempty" jsonschema:"what a failure does to the rest of the pool: \"continue\" (default) runs everything, \"stop\" schedules nothing new, \"kill\" additionally cancels in-flight runs"`
	Cwd           string            `json:"cwd,omitempty" jsonschema:"working directory shared by every run; inherits the server's cwd when empty"`
	Env           map[string]string `json:"env,omitempty" jsonschema:"environment overrides shared by every run; merged onto the server's env"`
	Wait          *bool             `json:"wait,omitempty" jsonschema:"block until the pool finishes or wait_timeout_ms elapses (default true)"`
	WaitTimeoutMs int               `json:"wait_timeout_ms,omitempty" jsonschema:"how long to wait before returning timed_out=true with a partial summary (default 60000)"`
	ExcerptBytes  *int              `json:"excerpt_bytes,omitempty" jsonschema:"per-stream tail excerpt cap in bytes for failed runs (default 4096, max 16384); 0 disables excerpts"`
}

// runManyOutput is the result shape for `cg_run_many`: the pool ID plus the
// embedded summary. Started rides alone on wait:false; TimedOut marks a
// partial summary for a pool that is still running.
type runManyOutput struct {
	ID              string `json:"id"`
	Started         bool   `json:"started,omitempty"`
	TimedOut        bool   `json:"timed_out,omitempty"`
	StartError      string `json:"start_error,omitempty"`
	RememberWarning string `json:"remember_warning,omitempty"`
	poolSummary
}

// poolSummary is the aggregate pool result shared by `cg_run_many` and
// `cg_wait` on a pool ID: a counts header, the commands array echoed once, and
// one flat record per run in manifest order. Counts omitted from the JSON are
// zero.
type poolSummary struct {
	Total     int             `json:"total,omitempty"`
	Succeeded int             `json:"succeeded,omitempty"`
	Failed    int             `json:"failed,omitempty"`
	Skipped   int             `json:"skipped,omitempty"`
	Running   int             `json:"running,omitempty"`
	Pending   int             `json:"pending,omitempty"`
	OnError   string          `json:"on_error,omitempty"`
	Commands  [][]string      `json:"commands,omitempty"`
	Runs      []poolRunResult `json:"runs,omitempty"`
}

// poolRunResult is one scheduled run in the summary. Command indexes the
// commands array. Pending and skipped runs carry no run ID. Failed runs carry
// tail excerpts of both streams until the pool-wide budget is spent; past it
// they carry excerpt_omitted instead.
type poolRunResult struct {
	Command        int    `json:"command"`
	RunID          string `json:"run_id,omitempty"`
	Status         string `json:"status"`
	ExitCode       *int   `json:"exit_code,omitempty"`
	Signal         *int   `json:"signal,omitempty"`
	StartError     string `json:"start_error,omitempty"`
	StdoutExcerpt  string `json:"stdout_excerpt,omitempty"`
	StderrExcerpt  string `json:"stderr_excerpt,omitempty"`
	ExcerptOmitted bool   `json:"excerpt_omitted,omitempty"`
}

func registerRunMany(s *mcpsdk.Server, reg *runRegistry, g *gate, sessionID string) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_run_many",
		Description: "Run a flat pool of commands: each command runs repeat times, at most parallelism at once, with on_error deciding what a failure does to the rest. Returns the pool ID and a summary with per-run IDs; failed runs carry tail excerpts. Every run is an ordinary capture run for cg_meta/cg_stdout/cg_grep drill-down, and cg_wait on the pool ID aggregates. Not a workflow engine: no dependencies between runs, no conditionals, no per-run fallback.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in runManyInput) (*mcpsdk.CallToolResult, runManyOutput, error) {
		var el elicitor
		if elicitationAvailable(req) {
			el = req.Session
		}
		return handleRunMany(ctx, reg, g, el, sessionID, in)
	})
}

func handleRunMany(ctx context.Context, reg *runRegistry, g *gate, el elicitor, sessionID string, in runManyInput) (*mcpsdk.CallToolResult, runManyOutput, error) {
	repeat, parallelism, err := validateRunMany(in)
	if err != nil {
		return nil, runManyOutput{}, err
	}

	excerpt := defaultExcerptBytes
	if in.ExcerptBytes != nil {
		excerpt = *in.ExcerptBytes
		if excerpt < 0 {
			excerpt = 0
		}
		if excerpt > maxExcerptBytes {
			excerpt = maxExcerptBytes
		}
	}

	wait := true
	if in.Wait != nil {
		wait = *in.Wait
	}

	// Every distinct argv passes the gate before the first spawn; a denial
	// fails the whole call with nothing started. Identical argvs are checked
	// once, so repeat does not multiply prompts.
	resolutions := make(map[string]*model.Resolution, len(in.Commands))
	var warnings []string
	for _, argv := range in.Commands {
		key := argvKey(argv)
		if _, ok := resolutions[key]; ok {
			continue
		}

		resolved, _ := model.ResolveCommand(argv, in.Cwd)
		warning, err := g.check(ctx, "cg_run_many", runInput{Command: argv, Cwd: in.Cwd, Env: in.Env}, resolved, el)
		if err != nil {
			return nil, runManyOutput{}, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		resolutions[key] = resolved
	}
	warning := strings.Join(warnings, "; ")

	spec := &model.PoolSpec{
		Commands:    make([]model.PoolCommand, 0, len(in.Commands)),
		Repeat:      repeat,
		Parallelism: parallelism,
		OnError:     in.OnError,
		Cwd:         in.Cwd,
		Env:         in.Env,
		SessionID:   sessionID,
	}
	for _, argv := range in.Commands {
		pc := model.PoolCommand{Argv: argv}
		if r := resolutions[argvKey(argv)]; r != nil {
			pc.Resolved = r.Resolved
			pc.Canonical = r.Canonical
		}
		spec.Commands = append(spec.Commands, pc)
	}

	pool, err := model.PoolSupervised(spec)
	if err != nil {
		var sf *model.StartFailure
		if errors.As(err, &sf) {
			return nil, runManyOutput{ID: sf.RunID, StartError: err.Error()}, nil
		}
		return nil, runManyOutput{}, fmt.Errorf("starting pool: %w", err)
	}
	if reg != nil {
		reg.Add(pool.ID, pool.Done)
	}

	if !wait {
		return nil, runManyOutput{ID: pool.ID, Started: true, RememberWarning: warning}, nil
	}

	timeoutMs := in.WaitTimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = defaultWaitTimeoutMs
	}

	timer := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
	defer timer.Stop()

	out := runManyOutput{ID: pool.ID, RememberWarning: warning}
	select {
	case <-pool.Done:
	case <-timer.C:
		out.TimedOut = true
	case <-ctx.Done():
		return nil, runManyOutput{}, ctx.Err()
	}

	summary, err := buildPoolSummary(pool.Dir, excerpt)
	if err != nil {
		return nil, runManyOutput{}, err
	}
	out.poolSummary = summary
	return nil, out, nil
}

// validateRunMany checks the input against the tool bounds, collecting every
// violation into one error so the caller can fix them in one pass. Counts are
// never clamped, except parallelism clamping down to the pool size, which
// cannot change what work runs. It returns repeat and parallelism with their
// defaults applied.
func validateRunMany(in runManyInput) (repeat, parallelism int, err error) {
	var violations []string

	if len(in.Commands) == 0 {
		violations = append(violations, "commands must contain at least one command")
	}
	for i, argv := range in.Commands {
		if len(argv) == 0 {
			violations = append(violations, fmt.Sprintf("commands[%d] must contain at least one element", i))
		}
	}

	repeat = in.Repeat
	switch {
	case repeat < 0:
		violations = append(violations, fmt.Sprintf("repeat must be positive, got %d", repeat))
	case repeat == 0:
		repeat = 1
	}

	parallelism = in.Parallelism
	switch {
	case parallelism < 0:
		violations = append(violations, fmt.Sprintf("parallelism must be positive, got %d", parallelism))
	case parallelism == 0:
		parallelism = 1
	case parallelism > maxPoolParallelism:
		violations = append(violations, fmt.Sprintf("parallelism %d exceeds the maximum %d", parallelism, maxPoolParallelism))
	}

	total := len(in.Commands) * repeat
	if total > maxPoolRuns {
		violations = append(violations, fmt.Sprintf("total runs %d (commands × repeat) exceeds the maximum %d", total, maxPoolRuns))
	}

	distinct := make(map[string]struct{}, len(in.Commands))
	for _, argv := range in.Commands {
		distinct[argvKey(argv)] = struct{}{}
	}
	if len(distinct) > maxPoolCommands {
		violations = append(violations, fmt.Sprintf("%d distinct commands exceeds the maximum %d", len(distinct), maxPoolCommands))
	}

	switch in.OnError {
	case "", model.OnErrorContinue, model.OnErrorStop, model.OnErrorKill:
	default:
		violations = append(violations, fmt.Sprintf("unknown on_error: %q (want %q, %q, or %q)", in.OnError, model.OnErrorContinue, model.OnErrorStop, model.OnErrorKill))
	}

	if len(violations) > 0 {
		return 0, 0, fmt.Errorf("invalid cg_run_many call: %s", strings.Join(violations, "; "))
	}

	if parallelism > total {
		parallelism = total
	}
	return repeat, parallelism, nil
}

// argvKey builds a map key for an argv. Argv tokens cannot contain NUL, so a
// NUL join is collision-free.
func argvKey(argv []string) string {
	return strings.Join(argv, "\x00")
}

// buildPoolSummary reads pool.json and builds the summary, attaching tail
// excerpts to failed runs at response time; excerpts never live in the
// manifest. excerpt is the per-stream cap, 0 to disable.
func buildPoolSummary(dir string, excerpt int) (poolSummary, error) {
	m, err := model.ReadPoolManifest(dir)
	if err != nil {
		return poolSummary{}, fmt.Errorf("reading pool.json: %w", err)
	}

	counts := m.Counts()
	s := poolSummary{
		Total:     counts.Total,
		Succeeded: counts.Succeeded,
		Failed:    counts.Failed,
		Skipped:   counts.Skipped,
		Running:   counts.Running,
		Pending:   counts.Pending,
		OnError:   m.OnError,
		Commands:  m.Commands,
		Runs:      make([]poolRunResult, 0, len(m.Runs)),
	}

	budget := poolExcerptBudget
	for _, r := range m.Runs {
		rec := poolRunResult{
			Command:    r.Command,
			RunID:      r.RunID,
			Status:     r.Status,
			ExitCode:   r.ExitCode,
			Signal:     r.Signal,
			StartError: r.StartError,
		}

		if r.Failed() && excerpt > 0 && rec.RunID != "" {
			attachPoolExcerpts(&rec, filepath.Join(model.CaptureRoot(), rec.RunID), excerpt, &budget)
		}

		s.Runs = append(s.Runs, rec)
	}
	return s, nil
}

// attachPoolExcerpts reads tail excerpts of both streams into rec, deducting
// the bytes read from the pool-wide budget. A record reached with no budget
// left, or whose stderr read the budget can no longer cover, carries
// excerpt_omitted instead.
func attachPoolExcerpts(rec *poolRunResult, dir string, excerpt int, budget *int) {
	if *budget <= 0 {
		rec.ExcerptOmitted = true
		return
	}

	stdout, _, _ := readTailExcerpt(filepath.Join(dir, "stdout"), min(excerpt, *budget))
	*budget -= len(stdout)
	rec.StdoutExcerpt = stdout

	if *budget <= 0 {
		rec.ExcerptOmitted = true
		return
	}

	stderr, _, _ := readTailExcerpt(filepath.Join(dir, "stderr"), min(excerpt, *budget))
	*budget -= len(stderr)
	rec.StderrExcerpt = stderr
}
