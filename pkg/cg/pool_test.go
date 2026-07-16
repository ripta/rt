package cg

import (
	"reflect"
	"testing"
	"time"
)

func intp(v int) *int {
	return &v
}

func TestPoolState(t *testing.T) {
	dir := t.TempDir()
	m := &PoolManifest{}

	lock, err := acquireRunLock(dir)
	if err != nil {
		t.Fatalf("acquiring lock: %v", err)
	}
	if got := PoolState(dir, m); got != PoolStateRunning {
		t.Fatalf("held lock, no finished_at: got %q, want %q", got, PoolStateRunning)
	}

	if err := lock.Close(); err != nil {
		t.Fatalf("releasing lock: %v", err)
	}
	if got := PoolState(dir, m); got != PoolStateAbandoned {
		t.Fatalf("released lock, no finished_at: got %q, want %q", got, PoolStateAbandoned)
	}

	now := time.Now()
	m.FinishedAt = &now
	if got := PoolState(dir, m); got != PoolStateFinished {
		t.Fatalf("finished_at set: got %q, want %q", got, PoolStateFinished)
	}
}

type poolRunFailedTest struct {
	Name string
	Rec  PoolRunRecord
	Want bool
}

var poolRunFailedTests = []poolRunFailedTest{
	{Name: "pending", Rec: PoolRunRecord{Status: PoolRunPending}, Want: false},
	{Name: "running", Rec: PoolRunRecord{Status: PoolRunRunning}, Want: false},
	{Name: "skipped", Rec: PoolRunRecord{Status: PoolRunSkipped}, Want: false},
	{Name: "start error", Rec: PoolRunRecord{Status: PoolRunStartError, StartError: "nope"}, Want: true},
	{Name: "exit zero", Rec: PoolRunRecord{Status: PoolRunFinished, ExitCode: intp(0)}, Want: false},
	{Name: "exit nonzero", Rec: PoolRunRecord{Status: PoolRunFinished, ExitCode: intp(2)}, Want: true},
	{Name: "signalled", Rec: PoolRunRecord{Status: PoolRunFinished, ExitCode: intp(0), Signal: intp(15)}, Want: true},
}

func TestPoolRunRecordFailed(t *testing.T) {
	for _, test := range poolRunFailedTests {
		t.Run(test.Name, func(t *testing.T) {
			if got := test.Rec.Failed(); got != test.Want {
				t.Fatalf("Failed() = %v, want %v", got, test.Want)
			}
		})
	}
}

func TestPoolManifestCounts(t *testing.T) {
	m := &PoolManifest{
		Runs: []PoolRunRecord{
			{Status: PoolRunFinished, ExitCode: intp(0)},
			{Status: PoolRunFinished, ExitCode: intp(1)},
			{Status: PoolRunStartError, StartError: "nope"},
			{Status: PoolRunSkipped},
			{Status: PoolRunRunning},
			{Status: PoolRunPending},
		},
	}

	got := m.Counts()
	want := PoolCounts{Total: 6, Succeeded: 1, Failed: 2, Skipped: 1, Running: 1, Pending: 1}
	if got != want {
		t.Fatalf("Counts() = %+v, want %+v", got, want)
	}
}

func TestPoolManifestMemberIDs(t *testing.T) {
	m := &PoolManifest{
		Runs: []PoolRunRecord{
			{Status: PoolRunFinished, RunID: "RY85VP"},
			{Status: PoolRunPending},
			{Status: PoolRunRunning, RunID: "0AB1CD"},
			{Status: PoolRunFinished, RunID: "../../etc"},
			{Status: PoolRunFinished, RunID: "short"},
		},
	}

	got := m.MemberIDs()
	want := []string{"RY85VP", "0AB1CD"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MemberIDs() = %v, want %v", got, want)
	}
}
