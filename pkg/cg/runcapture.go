package cg

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// CaptureRun is an in-flight or completed capture. The on-disk layout matches
// the shell --capture path: $TMPDIR/cg/<ID>/{stdout,stderr,meta.json}. Done
// closes when the child exits and meta.json has been written.
type CaptureRun struct {
	ID   string
	Dir  string
	Done <-chan struct{}
}

// RunCapture starts args[0] with args[1:] under capture. stdout and stderr are
// written to $TMPDIR/cg/<ID>/{stdout,stderr}. cwd is passed through; empty
// inherits the caller's working directory. env entries are appended to
// os.Environ, so MCP-supplied keys override the parent's.
//
// The child runs in its own process group, so cancelling a caller's context
// does not kill it. A background goroutine waits for the child, writes
// meta.json, and closes Done.
func RunCapture(args []string, cwd string, env map[string]string) (*CaptureRun, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("command is empty")
	}

	cap, err := NewCapture()
	if err != nil {
		return nil, err
	}

	outCounter := &lineCountingWriter{w: cap.Stdout}
	errCounter := &lineCountingWriter{w: cap.Stderr}

	child := exec.Command(args[0], args[1:]...)
	child.Dir = cwd
	child.Stdout = outCounter
	child.Stderr = errCounter
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(env) > 0 {
		child.Env = mergeEnv(os.Environ(), env)
	}

	start := time.Now()
	if err := child.Start(); err != nil {
		_ = cap.Close()
		_ = os.RemoveAll(cap.Dir)
		return nil, fmt.Errorf("starting child: %w", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		waitErr := child.Wait()
		elapsed := time.Since(start)
		_ = cap.Close()

		meta := &Meta{
			ID:          cap.ID,
			Command:     args,
			StartedAt:   start.UTC(),
			FinishedAt:  start.Add(elapsed).UTC(),
			DurationMs:  elapsed.Milliseconds(),
			ExitCode:    ExitCodeFromError(waitErr),
			StdoutLines: outCounter.n.Load(),
			StderrLines: errCounter.n.Load(),
		}
		if ws := exitStatus(child); ws != nil && ws.Signaled() {
			sig := int(ws.Signal())
			meta.Signal = &sig
		}
		_ = WriteMeta(cap.Dir, meta)
	}()

	return &CaptureRun{ID: cap.ID, Dir: cap.Dir, Done: done}, nil
}

// lineCountingWriter wraps an io.Writer and counts '\n' bytes as they pass
// through.
type lineCountingWriter struct {
	w io.Writer
	n atomic.Int64
}

func (lc *lineCountingWriter) Write(p []byte) (int, error) {
	n, err := lc.w.Write(p)
	for _, b := range p[:n] {
		if b == '\n' {
			lc.n.Add(1)
		}
	}
	return n, err
}

// mergeEnv returns base with overrides applied: matching keys are replaced in
// place; new keys are appended.
func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	out := make([]string, 0, len(base)+len(overrides))
	seen := make(map[string]struct{}, len(overrides))

	for _, kv := range base {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if v, ok := overrides[key]; ok {
			out = append(out, key+"="+v)
			seen[key] = struct{}{}
			continue
		}
		out = append(out, kv)
	}

	for k, v := range overrides {
		if _, ok := seen[k]; ok {
			continue
		}
		out = append(out, k+"="+v)
	}
	return out
}
