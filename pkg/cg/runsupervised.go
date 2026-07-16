package cg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
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

// StartFailure is returned by RunSupervised when the child process cannot be
// started. The run directory is preserved on disk with a debug.json for
// post-mortem inspection via cg_meta and the other cg tools.
type StartFailure struct {
	RunID string
	Dir   string
	Err   error
}

func (e *StartFailure) Error() string { return e.Err.Error() }
func (e *StartFailure) Unwrap() error { return e.Err }

// RunSupervised starts args[0] with args[1:] under capture, parented by a
// detached `cg supervise` process whose lifetime matches the run's. stdout and
// stderr are written to $TMPDIR/cg/<ID>/{stdout,stderr}. cwd is passed
// through; empty inherits the caller's working directory. env entries are
// appended to the supervisor's inherited environ, so MCP-supplied keys
// override the parent's. They ride the spec pipe and never appear in argv or
// on disk.
//
// `resolved` is the executable identity computed for args; when nil,
// RunSupervised resolves it itself. The supervisor execs resolved.ExecPath,
// the canonical path, while keeping args[0] as the child's argv[0], so a fresh
// PATH lookup at exec time cannot select a different file than the one the
// approval gate matched.
//
// The supervisor runs in its own session courtesy Setsid, so the caller's exit
// cannot signal it. Done is driven by EOF on the supervisor's status pipe,
// which arrives only after meta.json is written, preserving the Done contract.
func RunSupervised(args []string, resolved *Resolution, cwd string, env map[string]string) (*CaptureRun, error) {
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
	if err := cap.Close(); err != nil {
		return nil, fmt.Errorf("closing capture files: %w", err)
	}

	cwd = effectiveCwd(cwd)

	// A failure to launch the supervisor is reported like a child start
	// failure: the server writes debug.json and the run dir is preserved for
	// post-mortem inspection.
	failStart := func(err error) (*CaptureRun, error) {
		info := RunInfo{ID: cap.ID, Command: args, Cwd: cwd, StartedAt: time.Now().UTC()}
		_ = WriteStartDebug(cap.Dir, buildStartDebug(info, env, resolved, err))
		return nil, &StartFailure{RunID: cap.ID, Dir: cap.Dir, Err: err}
	}

	exe, err := os.Executable()
	if err != nil {
		return failStart(fmt.Errorf("locating cg executable: %w", err))
	}

	sup := exec.Command(exe, "supervise", cap.Dir)
	sup.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	stdin, err := sup.StdinPipe()
	if err != nil {
		return failStart(fmt.Errorf("creating spec pipe: %w", err))
	}
	stdout, err := sup.StdoutPipe()
	if err != nil {
		return failStart(fmt.Errorf("creating status pipe: %w", err))
	}

	if err := sup.Start(); err != nil {
		return failStart(fmt.Errorf("starting supervisor: %w", err))
	}

	spec := SuperviseSpec{Argv: args, Cwd: cwd, Env: env}
	if resolved != nil {
		spec.Resolved = resolved.Resolved
		spec.Canonical = resolved.Canonical
	}

	ack, err := sendSpec(stdin, stdout, &spec)
	if err != nil {
		_ = sup.Wait()
		return failStart(err)
	}

	if !ack.Started {
		// The supervisor wrote debug.json for a child start failure and exits
		// on its own; reap it without blocking the caller.
		go func() {
			_, _ = io.Copy(io.Discard, stdout)
			_ = sup.Wait()
		}()
		return nil, &StartFailure{RunID: cap.ID, Dir: cap.Dir, Err: errors.New(ack.StartError)}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, stdout)
		_ = sup.Wait()
	}()

	return &CaptureRun{ID: cap.ID, Dir: cap.Dir, Done: done}, nil
}

// sendSpec writes the spawn spec to the supervisor's stdin, closes it, and
// decodes the single ack line from the status pipe. An EOF or decode error
// means the supervisor died before acking.
func sendSpec(stdin io.WriteCloser, stdout io.Reader, spec *SuperviseSpec) (SuperviseAck, error) {
	var ack SuperviseAck

	data, err := json.Marshal(spec)
	if err != nil {
		return ack, fmt.Errorf("encoding spawn spec: %w", err)
	}
	if _, err := stdin.Write(data); err != nil {
		return ack, fmt.Errorf("writing spawn spec: %w", err)
	}
	if err := stdin.Close(); err != nil {
		return ack, fmt.Errorf("closing spawn spec pipe: %w", err)
	}

	if err := json.NewDecoder(stdout).Decode(&ack); err != nil {
		return ack, fmt.Errorf("reading supervisor ack: %w", err)
	}
	return ack, nil
}

// buildStartDebug assembles the diagnostic payload written to debug.json when
// child.Start fails. info carries the shared start-time facts. resolved carries
// the absolute resolved path and the symlink-canonical path when they could be
// determined, so a post-mortem shows both the original command and the file cg
// tried to exec.
func buildStartDebug(info RunInfo, env map[string]string, resolved *Resolution, startErr error) *StartDebug {
	d := &StartDebug{
		RunInfo:    info,
		StartError: startErr.Error(),
	}
	if resolved != nil {
		d.ResolvedPath = resolved.Resolved
		d.CanonicalPath = resolved.Canonical
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
