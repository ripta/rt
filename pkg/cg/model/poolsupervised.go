package model

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// PoolRun is an in-flight or completed pool of capture runs. Dir holds
// pool.json and the pool lock; member runs are ordinary sibling run
// directories. Done closes when the pool supervisor exits, which happens only
// after the final manifest has been written.
type PoolRun struct {
	ID   string
	Dir  string
	Done <-chan struct{}
}

// PoolSupervised starts a pool of capture runs under a detached
// `cg supervise-pool` process whose lifetime matches the pool's, mirroring
// RunSupervised. The spec, including env overrides, rides the supervisor's
// stdin and never lands in argv or on disk.
//
// The supervisor runs in its own session courtesy Setsid, so the caller's exit
// cannot signal it. Done is driven by EOF on the supervisor's status pipe,
// which arrives only after the final pool.json write.
//
// A failure to launch or ack is reported like a run start failure: debug.json
// is written into the pool dir for post-mortem inspection and a *StartFailure
// carries the pool ID. The debug record's command field stays empty; a pool
// has several commands and they live in the spec, not the record.
func PoolSupervised(spec *PoolSpec) (*PoolRun, error) {
	id, dir, err := NewPoolDir()
	if err != nil {
		return nil, err
	}

	failStart := func(err error) (*PoolRun, error) {
		info := RunInfo{ID: id, Cwd: effectiveCwd(spec.Cwd), StartedAt: time.Now().UTC()}
		_ = WriteStartDebug(dir, buildStartDebug(info, spec.Env, nil, err))
		return nil, &StartFailure{RunID: id, Dir: dir, Err: err}
	}

	exe, err := os.Executable()
	if err != nil {
		return failStart(fmt.Errorf("locating cg executable: %w", err))
	}

	sup := exec.Command(exe, "supervise-pool", dir)
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
		return failStart(fmt.Errorf("starting pool supervisor: %w", err))
	}

	ack, err := sendSpec(stdin, stdout, spec)
	if err != nil {
		_ = sup.Wait()
		return failStart(err)
	}

	if !ack.Started {
		// Unlike a run supervisor, the pool supervisor writes nothing on a
		// pre-ack failure, so the debug record is the server's to write; reap
		// the supervisor without blocking the caller.
		go func() {
			_, _ = io.Copy(io.Discard, stdout)
			_ = sup.Wait()
		}()
		return failStart(errors.New(ack.StartError))
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, stdout)
		_ = sup.Wait()
	}()

	return &PoolRun{ID: id, Dir: dir, Done: done}, nil
}
