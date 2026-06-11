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

// StartFailure is returned by RunCapture when the child process cannot be
// started. The run directory is preserved on disk with a debug.json for
// post-mortem inspection via cg_meta and the other cg tools.
type StartFailure struct {
	RunID string
	Dir   string
	Err   error
}

func (e *StartFailure) Error() string { return e.Err.Error() }
func (e *StartFailure) Unwrap() error { return e.Err }

// RunCapture starts args[0] with args[1:] under capture. stdout and stderr are
// written to $TMPDIR/cg/<ID>/{stdout,stderr}. cwd is passed through; empty
// inherits the caller's working directory. env entries are appended to
// os.Environ, so MCP-supplied keys override the parent's.
//
// resolved is the executable identity computed for args; when nil, RunCapture
// resolves it itself. The child execs resolved.ExecPath, the canonical path,
// while keeping args[0] as the child's argv[0], so a fresh PATH lookup at exec
// time cannot select a different file than the one the approval gate matched. An
// unresolved command falls back to args[0] so exec still surfaces the start
// failure.
//
// The child runs in its own process group, so cancelling a caller's context
// does not kill it. A background goroutine waits for the child, writes
// meta.json, and closes Done.
func RunCapture(args []string, resolved *Resolution, cwd string, env map[string]string) (*CaptureRun, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("command is empty")
	}

	if resolved == nil {
		resolved, _ = ResolveCommand(args, cwd)
	}

	cap, err := NewCapture()
	if err != nil {
		return nil, err
	}

	outCounter := &lineCountingWriter{w: cap.Stdout}
	errCounter := &lineCountingWriter{w: cap.Stderr}

	child := exec.Command(resolved.ExecPath(), args[1:]...)
	child.Args[0] = args[0]
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
		_ = WriteStartDebug(cap.Dir, buildStartDebug(args, cwd, env, resolved, err))
		return nil, &StartFailure{RunID: cap.ID, Dir: cap.Dir, Err: fmt.Errorf("starting child: %w", err)}
	}

	_ = WritePidFile(cap.Dir, child.Process.Pid)

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
		RemovePidFile(cap.Dir)
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

// buildStartDebug assembles the diagnostic payload written to debug.json when
// child.Start fails. resolved carries the absolute resolved path and the
// symlink-canonical path when they could be determined, so a post-mortem shows
// both the original command and the file cg tried to exec.
func buildStartDebug(args []string, cwd string, env map[string]string, resolved *Resolution, startErr error) *StartDebug {
	d := &StartDebug{
		Command:    args,
		StartError: startErr.Error(),
	}
	if resolved != nil {
		d.ResolvedPath = resolved.Resolved
		d.CanonicalPath = resolved.Canonical
	}
	if cwd != "" {
		d.Cwd = cwd
	} else if wd, err := os.Getwd(); err == nil {
		d.Cwd = wd
	}
	if v, ok := env["PATH"]; ok {
		d.Path = v
	} else {
		d.Path = os.Getenv("PATH")
	}
	return d
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
