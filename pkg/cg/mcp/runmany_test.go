package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/model"
)

// runManyBoundsTest is one rejected cg_run_many call: the input and the
// substrings its collect-all validation error must carry.
type runManyBoundsTest struct {
	Name string
	In   runManyInput
	Want []string
}

var runManyBoundsTests = []runManyBoundsTest{
	{
		Name: "empty commands",
		In:   runManyInput{},
		Want: []string{"at least one command"},
	},
	{
		Name: "empty argv",
		In:   runManyInput{Commands: [][]string{{"echo"}, {}}},
		Want: []string{"commands[1]"},
	},
	{
		Name: "negative repeat",
		In:   runManyInput{Commands: [][]string{{"echo"}}, Repeat: -1},
		Want: []string{"repeat must be positive"},
	},
	{
		Name: "negative parallelism",
		In:   runManyInput{Commands: [][]string{{"echo"}}, Parallelism: -2},
		Want: []string{"parallelism must be positive"},
	},
	{
		Name: "parallelism over cap",
		In:   runManyInput{Commands: [][]string{{"echo"}}, Repeat: 100, Parallelism: 33},
		Want: []string{"parallelism 33 exceeds the maximum 32"},
	},
	{
		Name: "total runs over cap",
		In:   runManyInput{Commands: [][]string{{"echo"}}, Repeat: 501},
		Want: []string{"total runs 501"},
	},
	{
		Name: "distinct commands over cap",
		In:   runManyInput{Commands: manyDistinctCommands(101)},
		Want: []string{"101 distinct commands"},
	},
	{
		Name: "unknown on_error",
		In:   runManyInput{Commands: [][]string{{"echo"}}, OnError: "explode"},
		Want: []string{"unknown on_error"},
	},
	{
		Name: "collects every violation",
		In:   runManyInput{Commands: [][]string{{"echo"}}, Repeat: 501, Parallelism: 40, OnError: "explode"},
		Want: []string{"total runs 501", "parallelism 40", "unknown on_error"},
	},
}

func manyDistinctCommands(n int) [][]string {
	out := make([][]string, n)
	for i := range out {
		out[i] = []string{"echo", strconv.Itoa(i)}
	}
	return out
}

func TestRunManyBoundsReject(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	for _, test := range runManyBoundsTests {
		t.Run(test.Name, func(t *testing.T) {
			_, _, err := handleRunMany(context.Background(), nil, nil, nil, "", test.In)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			for _, want := range test.Want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %q, want it to contain %q", err.Error(), want)
				}
			}
		})
	}

	assertCaptureRootEmpty(t)
}

// assertCaptureRootEmpty fails the test when any run or pool directory exists,
// proving a rejected or refused call spawned nothing.
func assertCaptureRootEmpty(t *testing.T) {
	t.Helper()

	entries, err := os.ReadDir(model.CaptureRoot())
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("reading capture root: %v", err)
	}
	if len(entries) > 0 {
		t.Errorf("capture root has %d entries, want none spawned", len(entries))
	}
}

func TestRunManyParallelismClampsToPoolSize(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, "", runManyInput{
		Commands:    [][]string{{"echo", "clamp"}},
		Repeat:      2,
		Parallelism: 32,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}

	m, err := model.ReadPoolManifest(filepath.Join(model.CaptureRoot(), out.ID))
	if err != nil {
		t.Fatalf("ReadPoolManifest: %v", err)
	}
	if m.Parallelism != 2 {
		t.Errorf("manifest.Parallelism = %d, want 2 (clamped to pool size)", m.Parallelism)
	}
}

func TestRunManyGateDenialSpawnsNothing(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\nallow:\n  - prefix: [echo]\ndeny:\n  - prefix: [rm]\n", false)
	_, _, err := handleRunMany(context.Background(), nil, g, nil, "", runManyInput{
		Commands: [][]string{{"echo", "hi"}, {"rm", "-rf", "x"}},
	})
	if err == nil {
		t.Fatalf("expected refusal for denied command")
	}
	if !strings.Contains(err.Error(), "cg_run_many refused") {
		t.Errorf("err = %v, want it to name cg_run_many", err)
	}

	assertCaptureRootEmpty(t)
}

func TestRunManyRepeatDedupesGateChecks(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	el := &fakeElicitor{results: []*mcpsdk.ElicitResult{accept(map[string]any{"remember": false})}}

	_, out, err := handleRunMany(context.Background(), nil, g, el, "", runManyInput{
		Commands: [][]string{{"echo", "hi"}, {"echo", "hi"}},
		Repeat:   5,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if len(el.calls) != 1 {
		t.Errorf("prompted %d times, want 1 (identical argvs are checked once)", len(el.calls))
	}
	if out.Succeeded != 10 {
		t.Errorf("Succeeded = %d, want 10", out.Succeeded)
	}
}

func TestRunManySyncSummary(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, "", runManyInput{
		Commands: [][]string{
			{"echo", "fine"},
			{"sh", "-c", "echo boom >&2; exit 3"},
		},
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}

	if out.ID == "" {
		t.Error("ID empty")
	}
	if out.TimedOut {
		t.Error("TimedOut = true, want false")
	}
	if out.Total != 2 || out.Succeeded != 1 || out.Failed != 1 {
		t.Errorf("counts = %d/%d/%d (total/succeeded/failed), want 2/1/1", out.Total, out.Succeeded, out.Failed)
	}
	if len(out.Commands) != 2 {
		t.Errorf("len(Commands) = %d, want 2", len(out.Commands))
	}
	if len(out.Runs) != 2 {
		t.Fatalf("len(Runs) = %d, want 2", len(out.Runs))
	}

	ok := out.Runs[0]
	if ok.Command != 0 || ok.RunID == "" || ok.Status != model.PoolRunFinished {
		t.Errorf("runs[0] = %+v, want finished command 0 with a run ID", ok)
	}
	if ok.StdoutExcerpt != "" || ok.StderrExcerpt != "" {
		t.Errorf("runs[0] carries excerpts %q/%q, want none on success", ok.StdoutExcerpt, ok.StderrExcerpt)
	}

	bad := out.Runs[1]
	if bad.ExitCode == nil || *bad.ExitCode != 3 {
		t.Errorf("runs[1].ExitCode = %v, want 3", bad.ExitCode)
	}
	if !strings.Contains(bad.StderrExcerpt, "boom") {
		t.Errorf("runs[1].StderrExcerpt = %q, want the tail of stderr", bad.StderrExcerpt)
	}
}

func TestRunManyExcerptBudget(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	// Each failure's stdout tail alone covers the whole pool-wide budget, so
	// the first record spends it and the second carries the omitted marker.
	excerpt := maxExcerptBytes
	_, out, err := handleRunMany(context.Background(), nil, nil, nil, "", runManyInput{
		Commands:     [][]string{{"sh", "-c", "head -c 20000 /dev/zero; exit 1"}},
		Repeat:       2,
		ExcerptBytes: &excerpt,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if len(out.Runs) != 2 {
		t.Fatalf("len(Runs) = %d, want 2", len(out.Runs))
	}

	if got := len(out.Runs[0].StdoutExcerpt); got != poolExcerptBudget {
		t.Errorf("runs[0] stdout excerpt = %d bytes, want the full budget %d", got, poolExcerptBudget)
	}
	if !out.Runs[0].ExcerptOmitted {
		t.Errorf("runs[0].ExcerptOmitted = false, want true (stderr no longer fits)")
	}
	if !out.Runs[1].ExcerptOmitted {
		t.Errorf("runs[1].ExcerptOmitted = false, want true past the budget")
	}
	if out.Runs[1].StdoutExcerpt != "" || out.Runs[1].StderrExcerpt != "" {
		t.Errorf("runs[1] carries excerpts, want none past the budget")
	}
}

func TestRunManyExcerptDisabled(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	zero := 0
	_, out, err := handleRunMany(context.Background(), nil, nil, nil, "", runManyInput{
		Commands:     [][]string{{"sh", "-c", "echo boom; exit 1"}},
		ExcerptBytes: &zero,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if len(out.Runs) != 1 {
		t.Fatalf("len(Runs) = %d, want 1", len(out.Runs))
	}
	if out.Runs[0].StdoutExcerpt != "" || out.Runs[0].ExcerptOmitted {
		t.Errorf("runs[0] = %+v, want no excerpts and no omitted marker with excerpt_bytes 0", out.Runs[0])
	}
}

func TestRunManyTimeoutPartialSummary(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, "", runManyInput{
		Commands:      [][]string{{"sleep", "1"}},
		WaitTimeoutMs: 150,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if !out.TimedOut {
		t.Errorf("TimedOut = false, want true")
	}
	if out.Total != 1 || out.Running+out.Pending != 1 {
		t.Errorf("counts = %+v, want one run still in flight", out.poolSummary)
	}

	// Let the pool finish before TMPDIR cleanup so the supervisor stops
	// rewriting the manifest under the removal.
	waitForPoolFinished(t, filepath.Join(model.CaptureRoot(), out.ID))
}

// waitForPoolFinished polls the manifest until finished_at lands.
func waitForPoolFinished(t *testing.T, dir string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m, err := model.ReadPoolManifest(dir); err == nil && m.FinishedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pool %s to finish", dir)
}

func TestRunManyAsyncThenWait(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	reg := newRunRegistry()
	async := false
	_, started, err := handleRunMany(context.Background(), reg, nil, nil, "", runManyInput{
		Commands: [][]string{{"echo", "round-trip"}},
		Wait:     &async,
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if !started.Started {
		t.Fatalf("pool not started: %+v", started)
	}
	if started.Total != 0 || len(started.Runs) != 0 {
		t.Errorf("async return carries a summary %+v, want none", started.poolSummary)
	}

	_, out, err := handleWait(context.Background(), reg, waitInput{ID: started.ID, TimeoutMs: 30000})
	if err != nil {
		t.Fatalf("handleWait: %v", err)
	}
	if !out.Finished {
		t.Errorf("Finished = false, want true")
	}
	if out.Total != 1 || out.Succeeded != 1 {
		t.Errorf("counts = %+v, want 1/1 (total/succeeded)", out.poolSummary)
	}
	if len(out.Runs) != 1 || out.Runs[0].RunID == "" {
		t.Errorf("Runs = %+v, want one record with a run ID", out.Runs)
	}
}

func TestRunManyThroughMCPLayer(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	ctx := context.Background()
	server := newServer("test", time.Now(), "", &gate{blindlyAllow: true})

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "cg_run_many",
		Arguments: map[string]any{
			"commands": [][]string{{"echo", "wire"}, {"false"}},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}

	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling structured content: %v", err)
	}
	var out runManyOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshaling summary: %v", err)
	}

	if out.ID == "" {
		t.Error("ID empty over the wire")
	}
	if out.Total != 2 || out.Succeeded != 1 || out.Failed != 1 {
		t.Errorf("counts = %d/%d/%d (total/succeeded/failed), want 2/1/1", out.Total, out.Succeeded, out.Failed)
	}
	if len(out.Commands) != 2 || len(out.Runs) != 2 {
		t.Errorf("Commands/Runs = %d/%d, want 2/2", len(out.Commands), len(out.Runs))
	}
}

func TestHandleRunManyThreadsSessionID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, out, err := handleRunMany(context.Background(), nil, nil, nil, "SESS", runManyInput{
		Commands: [][]string{{"echo", "one"}, {"echo", "two"}},
	})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}

	m, err := model.ReadPoolManifest(filepath.Join(model.CaptureRoot(), out.ID))
	if err != nil {
		t.Fatalf("ReadPoolManifest: %v", err)
	}
	if m.SessionID != "SESS" {
		t.Errorf("manifest.SessionID = %q, want %q", m.SessionID, "SESS")
	}

	for i, rec := range m.Runs {
		meta, err := model.ReadMeta(filepath.Join(model.CaptureRoot(), rec.RunID))
		if err != nil {
			t.Fatalf("ReadMeta on member %d: %v", i, err)
		}
		if meta.SessionID != "SESS" {
			t.Errorf("member %d meta.SessionID = %q, want %q", i, meta.SessionID, "SESS")
		}
	}
}
