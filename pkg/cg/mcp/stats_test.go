package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/model"
)

var statsTestBase = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

type statsTestRun struct {
	ID          string
	Argv        []string
	Cwd         string
	Day         int
	DurationMs  int64
	ExitCode    int
	Parallelism int
}

// newStatsTestHistory opens a history under a fixed key in fresh temp dirs. Duplicated from the
// model tests to avoid exporting a test-only helper.
func newStatsTestHistory(t *testing.T) *model.History {
	t.Helper()

	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("TMPDIR", t.TempDir())

	dir := filepath.Join(state, "cg")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating history dir: %v", err)
	}

	key := bytes.Repeat([]byte{0x42}, 32)
	if err := os.WriteFile(filepath.Join(dir, model.HistoryKeyFilename), key, 0o600); err != nil {
		t.Fatalf("writing key: %v", err)
	}

	h, err := model.OpenHistory()
	if err != nil {
		t.Fatalf("OpenHistory: %v", err)
	}

	waitForSupervisorsAtCleanup(t)

	return h
}

// waitForSupervisorsAtCleanup registers a cleanup that blocks until every supervisor under the
// capture root has exited. A supervisor keeps writing after meta.json lands, so the temp
// directories cannot be removed before then. The cleanup runs before the directories' own, since
// those were registered earlier.
func waitForSupervisorsAtCleanup(t *testing.T) {
	t.Helper()

	root := model.CaptureRoot()
	t.Cleanup(func() {
		entries, err := os.ReadDir(root)
		if err != nil {
			return
		}

		for _, e := range entries {
			dir := filepath.Join(root, e.Name())
			deadline := time.Now().Add(15 * time.Second)
			for model.LockFileExists(dir) && !model.RunLockReleased(dir) {
				if time.Now().After(deadline) {
					t.Errorf("supervisor for %s still running at cleanup", dir)

					break
				}

				time.Sleep(20 * time.Millisecond)
			}
		}
	})
}

func appendStatsTestRuns(t *testing.T, h *model.History, runs []statsTestRun) {
	t.Helper()

	for _, r := range runs {
		meta := &model.Meta{
			RunInfo: model.RunInfo{
				ID:        r.ID,
				Command:   r.Argv,
				Cwd:       r.Cwd,
				StartedAt: statsTestBase.AddDate(0, 0, r.Day),
			},
			DurationMs: r.DurationMs,
			ExitCode:   r.ExitCode,
		}
		if r.Parallelism > 0 {
			meta.Pool = "POOLID"
		}

		if err := h.Append(h.NewRunRecord(meta, r.Parallelism)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
}

func callStats(t *testing.T, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()

	ctx := context.Background()
	server := newServer("test", time.Now(), "SESS", &gate{blindlyAllow: true})

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

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "cg_stats", Arguments: args})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	return res
}

func statsResultJSON(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()

	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}

	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling structured content: %v", err)
	}

	var out model.StatsResult
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshaling stats: %v", err)
	}

	indented, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatalf("marshaling stats: %v", err)
	}

	return string(indented)
}

func runStatsCLI(t *testing.T, args ...string) string {
	t.Helper()

	var out, errOut bytes.Buffer

	cmd := model.NewStatsCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"-o", "json"}, args...))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cg stats: %v (stderr: %s)", err, errOut.String())
	}

	return strings.TrimSpace(out.String())
}

var statsToolRuns = []statsTestRun{
	{ID: "AAAAA1", Argv: []string{"make", "test"}, Cwd: "/", Day: 0, DurationMs: 100000},
	{ID: "AAAAA2", Argv: []string{"make", "test"}, Cwd: "/", Day: 1, DurationMs: 200000},
	{ID: "AAAAA3", Argv: []string{"make", "test"}, Cwd: "/", Day: 2, DurationMs: 300000, Parallelism: 4},
	{ID: "AAAAA4", Argv: []string{"make", "test", "-j4"}, Cwd: "/", Day: 3, DurationMs: 400000},
	{ID: "AAAAA5", Argv: []string{"make"}, Cwd: "/", Day: 0, DurationMs: 5000},
	{ID: "AAAAA6", Argv: []string{"make", "test"}, Cwd: "/", Day: 4, DurationMs: 1000, ExitCode: 1},
	{ID: "AAAAA7", Argv: []string{"make", "test"}, Cwd: "/tmp", Day: 4, DurationMs: 2000},
}

type statsParityTest struct {
	Name string
	Args map[string]any
	CLI  []string
}

var statsParityTests = []statsParityTest{
	{
		Name: "defaults",
		Args: map[string]any{"command": []string{"make", "test", "-j4"}, "cwd": "/"},
		CLI:  []string{"--cwd", "/", "--", "make", "test", "-j4"},
	},
	{
		Name: "standalone only",
		Args: map[string]any{"command": []string{"make", "test"}, "cwd": "/", "pool": "none"},
		CLI:  []string{"--cwd", "/", "--pool", "none", "--", "make", "test"},
	},
	{
		Name: "pooled only",
		Args: map[string]any{"command": []string{"make", "test"}, "cwd": "/", "pool": "only"},
		CLI:  []string{"--cwd", "/", "--pool", "only", "--", "make", "test"},
	},
	{
		Name: "failed runs",
		Args: map[string]any{"command": []string{"make", "test"}, "cwd": "/", "exit_code": "!=0"},
		CLI:  []string{"--cwd", "/", "--exit-code", "!=0", "--", "make", "test"},
	},
	{
		Name: "since and samples",
		Args: map[string]any{"command": []string{"make", "test"}, "cwd": "/", "since": "2026-10-02", "samples": 1},
		CLI:  []string{"--cwd", "/", "--since", "2026-10-02", "--samples", "1", "--", "make", "test"},
	},
	{
		Name: "no samples",
		Args: map[string]any{"command": []string{"cargo", "build"}, "cwd": "/"},
		CLI:  []string{"--cwd", "/", "--", "cargo", "build"},
	},
}

func TestStatsToolMatchesCLI(t *testing.T) {
	for _, test := range statsParityTests {
		t.Run(test.Name, func(t *testing.T) {
			h := newStatsTestHistory(t)
			appendStatsTestRuns(t, h, statsToolRuns)

			got := statsResultJSON(t, callStats(t, test.Args))
			want := runStatsCLI(t, test.CLI...)

			if got != want {
				t.Errorf("tool JSON differs from CLI JSON\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

func TestStatsToolDefaultsToServerCwd(t *testing.T) {
	h := newStatsTestHistory(t)

	dir, err := model.CanonicalCwd(t.TempDir())
	if err != nil {
		t.Fatalf("CanonicalCwd: %v", err)
	}
	t.Chdir(dir)

	appendStatsTestRuns(t, h, []statsTestRun{
		{ID: "AAAAA1", Argv: []string{"make", "test"}, Cwd: dir, DurationMs: 123000},
		{ID: "AAAAA2", Argv: []string{"make", "test"}, Cwd: "/", DurationMs: 999000},
	})

	res := callStats(t, map[string]any{"command": []string{"make", "test"}})
	got := statsResultJSON(t, res)

	var out model.StatsResult
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("unmarshaling: %v", err)
	}

	if out.Cwd != dir {
		t.Errorf("cwd = %q, want %q", out.Cwd, dir)
	}

	exact := out.ExactStandalone()
	if exact == nil || exact.Stats.Count != 1 || exact.Stats.LastMs != 123000 {
		t.Errorf("exact match = %+v, want one run of 123000ms in the server cwd", exact)
	}
}

type statsInvalidTest struct {
	Name string
	Args map[string]any
	Want string
}

var statsInvalidTests = []statsInvalidTest{
	{"no command", map[string]any{"command": []string{}}, "command is required"},
	{"bad pool", map[string]any{"command": []string{"make"}, "pool": "some"}, "pool"},
	{"too many samples", map[string]any{"command": []string{"make"}, "samples": 1001}, "samples"},
	{"negative samples", map[string]any{"command": []string{"make"}, "samples": -1}, "samples"},
	{"bad exit code", map[string]any{"command": []string{"make"}, "exit_code": "abc"}, "invalid exit_code"},
	{"bad since", map[string]any{"command": []string{"make"}, "since": "whenever"}, "invalid since"},
}

func TestStatsToolRejectsInvalidInput(t *testing.T) {
	for _, test := range statsInvalidTests {
		t.Run(test.Name, func(t *testing.T) {
			newStatsTestHistory(t)

			res := callStats(t, test.Args)
			if !res.IsError {
				t.Fatalf("expected a tool error, got %+v", res.StructuredContent)
			}

			var text strings.Builder
			for _, c := range res.Content {
				if tc, ok := c.(*mcpsdk.TextContent); ok {
					text.WriteString(tc.Text)
				}
			}
			if !strings.Contains(text.String(), test.Want) {
				t.Errorf("error %q does not mention %q", text.String(), test.Want)
			}
		})
	}
}
