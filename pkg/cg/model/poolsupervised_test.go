package model

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPoolSupervisedRunsPool(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	spec := poolSpec(t, []string{"echo", "one"}, []string{"sh", "-c", "exit 3"})
	pool, err := PoolSupervised(spec)
	if err != nil {
		t.Fatalf("PoolSupervised: %v", err)
	}

	select {
	case <-pool.Done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for pool to finish")
	}

	m := readManifest(t, pool.Dir)
	if m.ID != pool.ID {
		t.Errorf("manifest.ID = %q, want %q", m.ID, pool.ID)
	}
	if m.FinishedAt == nil {
		t.Error("manifest has no finished_at; Done must close only after the final write")
	}

	if len(m.Runs) != 2 {
		t.Fatalf("len(runs) = %d, want 2", len(m.Runs))
	}
	for i, r := range m.Runs {
		if r.Status != PoolRunFinished {
			t.Errorf("runs[%d].Status = %q, want %q", i, r.Status, PoolRunFinished)
		}
	}
	if m.Runs[0].ExitCode == nil || *m.Runs[0].ExitCode != 0 {
		t.Errorf("runs[0].ExitCode = %v, want 0", m.Runs[0].ExitCode)
	}
	if m.Runs[1].ExitCode == nil || *m.Runs[1].ExitCode != 3 {
		t.Errorf("runs[1].ExitCode = %v, want 3", m.Runs[1].ExitCode)
	}
}

func TestPoolSupervisedStartFailure(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	// A negative repeat passes the server's marshaling but fails the
	// supervisor's normalize, so the ack comes back as a start error.
	spec := poolSpec(t, []string{"echo", "one"})
	spec.Repeat = -1

	_, err := PoolSupervised(spec)

	var sf *StartFailure
	if !errors.As(err, &sf) {
		t.Fatalf("PoolSupervised error = %v, want *StartFailure", err)
	}
	if !strings.Contains(sf.Error(), "repeat") {
		t.Errorf("error = %q, want the supervisor's repeat complaint", sf.Error())
	}

	dbg, derr := ReadStartDebug(sf.Dir)
	if derr != nil {
		t.Fatalf("ReadStartDebug: %v", derr)
	}
	if !strings.Contains(dbg.StartError, "repeat") {
		t.Errorf("debug.StartError = %q, want the supervisor's repeat complaint", dbg.StartError)
	}

	if _, lerr := LookupRunDir(sf.RunID); !errors.Is(lerr, ErrFailedRun) {
		t.Errorf("LookupRunDir(%s) = %v, want ErrFailedRun", sf.RunID, lerr)
	}
}
