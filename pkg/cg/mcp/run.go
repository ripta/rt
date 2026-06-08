package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg"
)

const (
	defaultWaitTimeoutMs = 60000
	defaultExcerptBytes  = 4096
	maxExcerptBytes      = 16384
)

// runInput is the argument shape for `cg_run`.
type runInput struct {
	Command       []string          `json:"command" jsonschema:"argv to execute; index 0 is the program"`
	Cwd           string            `json:"cwd,omitempty" jsonschema:"working directory; inherits the server's cwd when empty"`
	Env           map[string]string `json:"env,omitempty" jsonschema:"environment overrides; merged onto the server's env"`
	Wait          *bool             `json:"wait,omitempty" jsonschema:"block until the child exits or wait_timeout_ms elapses (default true)"`
	WaitTimeoutMs int               `json:"wait_timeout_ms,omitempty" jsonschema:"how long to wait before returning timed_out=true (default 60000)"`
	ExcerptBytes  int               `json:"excerpt_bytes,omitempty" jsonschema:"per-stream head-excerpt cap in bytes (default 4096, max 16384)"`
}

// runOutput is the result shape for `cg_run`.
type runOutput struct {
	ID            string `json:"id"`
	Started       bool   `json:"started,omitempty"`
	TimedOut      bool   `json:"timed_out,omitempty"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	Signal        *int   `json:"signal,omitempty"`
	DurationMs    *int64 `json:"duration_ms,omitempty"`
	StdoutLines   *int64 `json:"stdout_lines,omitempty"`
	StderrLines   *int64 `json:"stderr_lines,omitempty"`
	StdoutExcerpt string `json:"stdout_excerpt"`
	StderrExcerpt string `json:"stderr_excerpt"`
	Truncated     bool   `json:"truncated"`
}

func registerRun(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_run",
		Description: "Run a command with capture. Returns metadata, exit code, and short head-excerpts of stdout and stderr. The run is recorded on disk under $TMPDIR/cg/<id>/ and can be inspected with the other cg tools.",
	}, handleRun)
}

func handleRun(ctx context.Context, _ *mcpsdk.CallToolRequest, in runInput) (*mcpsdk.CallToolResult, runOutput, error) {
	if len(in.Command) == 0 {
		return nil, runOutput{}, fmt.Errorf("command must contain at least one element")
	}

	excerpt := in.ExcerptBytes
	if excerpt <= 0 {
		excerpt = defaultExcerptBytes
	}
	if excerpt > maxExcerptBytes {
		excerpt = maxExcerptBytes
	}

	wait := true
	if in.Wait != nil {
		wait = *in.Wait
	}

	run, err := cg.RunCapture(in.Command, in.Cwd, in.Env)
	if err != nil {
		return nil, runOutput{}, fmt.Errorf("starting capture: %w", err)
	}

	if !wait {
		return nil, runOutput{ID: run.ID, Started: true}, nil
	}

	timeoutMs := in.WaitTimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = defaultWaitTimeoutMs
	}

	timer := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-run.Done:
		return nil, finishedOutput(run, excerpt), nil
	case <-timer.C:
		return nil, timedOutOutput(run, excerpt), nil
	case <-ctx.Done():
		return nil, runOutput{}, ctx.Err()
	}
}

// finishedOutput builds the result for a fully completed run, reading
// meta.json to fill exit/signal/duration/line-count fields.
func finishedOutput(run *cg.CaptureRun, excerpt int) runOutput {
	out := runOutput{ID: run.ID}

	if meta, err := cg.ReadMeta(run.Dir); err == nil {
		ec := meta.ExitCode
		dur := meta.DurationMs
		outLines := meta.StdoutLines
		errLines := meta.StderrLines
		out.ExitCode = &ec
		out.DurationMs = &dur
		out.StdoutLines = &outLines
		out.StderrLines = &errLines
		if meta.Signal != nil {
			sig := *meta.Signal
			out.Signal = &sig
		}
	}

	stdout, outMore, _ := readExcerpt(filepath.Join(run.Dir, "stdout"), excerpt)
	stderr, errMore, _ := readExcerpt(filepath.Join(run.Dir, "stderr"), excerpt)
	out.StdoutExcerpt = stdout
	out.StderrExcerpt = stderr
	out.Truncated = outMore || errMore
	return out
}

// timedOutOutput builds the result for a run still in flight when the wait
// timeout fires. The child is left alone; capture continues on disk. The
// caller can use cg_meta / cg_stdout to check on it later.
func timedOutOutput(run *cg.CaptureRun, excerpt int) runOutput {
	stdout, outMore, _ := readExcerpt(filepath.Join(run.Dir, "stdout"), excerpt)
	stderr, errMore, _ := readExcerpt(filepath.Join(run.Dir, "stderr"), excerpt)
	return runOutput{
		ID:            run.ID,
		TimedOut:      true,
		StdoutExcerpt: stdout,
		StderrExcerpt: stderr,
		Truncated:     outMore || errMore,
	}
}

// readExcerpt reads up to limit bytes from the head of path. hasMore reports
// whether the file holds more data than was returned.
func readExcerpt(path string, limit int) (content string, hasMore bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	defer f.Close()

	buf := make([]byte, limit)
	n, err := io.ReadFull(f, buf)
	switch {
	case err == nil:
		// Read filled the buffer; check if there's anything beyond it.
		one := make([]byte, 1)
		extra, _ := f.Read(one)
		return string(buf[:n]), extra > 0, nil
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return string(buf[:n]), false, nil
	default:
		return string(buf[:n]), false, err
	}
}
