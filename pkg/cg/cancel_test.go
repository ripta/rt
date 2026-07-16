package cg

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

type parseSignalTest struct {
	name    string
	input   string
	want    syscall.Signal
	wantErr bool
}

var parseSignalTests = []parseSignalTest{
	{name: "empty uses default", input: "", want: syscall.SIGTERM},
	{name: "SIGTERM", input: "SIGTERM", want: syscall.SIGTERM},
	{name: "lowercase sigint", input: "sigint", want: syscall.SIGINT},
	{name: "SIGKILL", input: "SIGKILL", want: syscall.SIGKILL},
	{name: "numeric", input: "3", want: syscall.Signal(3)},
	{name: "zero rejected", input: "0", wantErr: true},
	{name: "too large rejected", input: "999", wantErr: true},
	{name: "garbage rejected", input: "SIGFOO", wantErr: true},
}

func TestParseSignal(t *testing.T) {
	for _, tt := range parseSignalTests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSignal(tt.input, syscall.SIGTERM)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseSignal(%q) = %v, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSignal(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseSignal(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestCancelRunAlreadyFinished(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	seedRunDir(t, "AAAAAA", &Meta{RunInfo: RunInfo{ID: "AAAAAA", Command: []string{"echo", "hi"}}})

	res, err := CancelRun(context.Background(), "AAAAAA", CancelOptions{})
	if err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if res.Signaled {
		t.Errorf("signaled = true, want false for finished run")
	}
	if !res.Finished {
		t.Errorf("finished = false, want true for finished run")
	}
}

func TestCancelRunUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := CancelRun(context.Background(), "ZZZZZZ", CancelOptions{}); !errors.Is(err, ErrUnknownRunID) {
		t.Errorf("err = %v, want ErrUnknownRunID", err)
	}
}

func TestCancelRunInvalidSignal(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := CancelRun(context.Background(), "AAAAAA", CancelOptions{Signal: "SIGFOO"}); err == nil {
		t.Fatalf("expected error for invalid signal")
	}
}

func TestCancelRunLive(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	run, err := RunSupervised([]string{"sleep", "30"}, SuperviseOptions{})
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
	}

	res, err := CancelRun(context.Background(), run.ID, CancelOptions{Signal: "SIGKILL"})
	if err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if !res.Signaled {
		t.Errorf("signaled = false, want true for live run")
	}

	select {
	case <-run.Done:
	case <-time.After(5 * time.Second):
		t.Fatalf("run %s did not finish after cancel", run.ID)
	}
}

func TestCancelPoolStopsScheduling(t *testing.T) {
	id, dir := newPoolTestDir(t)

	spec := poolSpec(t,
		[]string{"sh", "-c", "sleep 0.5; echo done"},
		[]string{"echo", "never"},
	)
	sup, _ := startDetachedPool(t, dir, spec)
	waitForRunning(t, dir, 0)

	res, err := CancelRun(context.Background(), id, CancelOptions{})
	if err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if !res.Pool || !res.Signaled {
		t.Errorf("result = %+v, want pool and signaled", res)
	}

	if err := sup.Wait(); err != nil {
		t.Fatalf("pool supervisor exit: %v", err)
	}
	m := readManifest(t, dir)
	if m.FinishedAt == nil {
		t.Error("manifest has no finished_at, want complete")
	}
	if m.Runs[0].Status != PoolRunFinished || m.Runs[0].Signal != nil {
		t.Errorf("in-flight run = %+v, want finished without a signal", m.Runs[0])
	}
	if m.Runs[1].Status != PoolRunSkipped {
		t.Errorf("pending run status = %q, want skipped", m.Runs[1].Status)
	}
}

func TestCancelPoolFinished(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	finished := time.Now().UTC()
	seedPoolDir(t, "PPPPPP", &PoolManifest{
		ID:         "PPPPPP",
		Commands:   [][]string{{"echo", "hi"}},
		StartedAt:  finished.Add(-time.Minute),
		FinishedAt: &finished,
		Runs:       []PoolRunRecord{{Command: 0, RunID: "AAAAAA", Status: PoolRunFinished, ExitCode: intp(0)}},
	})

	res, err := CancelRun(context.Background(), "PPPPPP", CancelOptions{})
	if err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if !res.Pool || res.Signaled || !res.Finished {
		t.Errorf("result = %+v, want pool, unsignaled, finished", res)
	}
}

func TestCancelPoolAbandoned(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	// Abandoned: no finished_at, released lock, and a stale pid file left by a
	// SIGKILLed supervisor. The guard must report finished without signalling.
	dir := seedPoolDir(t, "PPPPPP", &PoolManifest{
		ID:        "PPPPPP",
		Commands:  [][]string{{"sleep", "60"}},
		StartedAt: time.Now().UTC(),
		Runs:      []PoolRunRecord{{Command: 0, RunID: "AAAAAA", Status: PoolRunRunning}},
	})
	lock, err := acquireRunLock(dir)
	if err != nil {
		t.Fatalf("acquiring lock: %v", err)
	}
	lock.Close()
	if err := WritePidFile(dir, os.Getpid()); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	res, err := CancelRun(context.Background(), "PPPPPP", CancelOptions{})
	if err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if !res.Pool || res.Signaled || !res.Finished {
		t.Errorf("result = %+v, want pool, unsignaled, finished", res)
	}
}
