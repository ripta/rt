package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/model"
)

// changedSupervisor records a stand-in cg executable, then swaps it, as a command able to write the
// real executable's path could. Verification fails before any spawn, so the stand-in never runs.
func changedSupervisor(t *testing.T) *model.ExecIdentity {
	t.Helper()

	path := filepath.Join(t.TempDir(), "cg")
	if err := os.WriteFile(path, []byte("original"), 0o755); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	id, err := model.NewExecIdentity(path)
	if err != nil {
		t.Fatalf("NewExecIdentity: %v", err)
	}

	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte("swapped"), 0o755); err != nil {
		t.Fatalf("writing %s: %v", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("renaming over %s: %v", path, err)
	}
	return id
}

func TestHandleRunRefusesChangedSupervisor(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	marker := filepath.Join(t.TempDir(), "ran")
	_, out, err := handleRun(context.Background(), nil, nil, nil, runOrigin{Supervisor: changedSupervisor(t)}, runInput{
		Command: []string{"touch", marker},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}

	if out.Started || !strings.Contains(out.StartError, "restart cg") {
		t.Errorf("out = %+v, want a start error telling the user to restart cg", out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the command ran")
	}
}

func TestHandleRunManyRefusesChangedSupervisor(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, runOrigin{Supervisor: changedSupervisor(t)}, runManyInput{
		Commands: [][]string{{"echo", "one"}},
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}

	if out.Started || !strings.Contains(out.StartError, "restart cg") {
		t.Errorf("out = %+v, want a start error telling the user to restart cg", out)
	}
}

func TestHandleRunUnchangedSupervisorRuns(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	id, err := model.NewExecIdentity(os.Args[0])
	if err != nil {
		t.Fatalf("NewExecIdentity: %v", err)
	}

	_, out, err := handleRun(context.Background(), nil, nil, nil, runOrigin{Supervisor: id}, runInput{
		Command: []string{"echo", "hi"},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.StartError != "" || out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("out = %+v, want a clean run", out)
	}
}
