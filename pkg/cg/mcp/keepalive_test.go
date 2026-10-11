package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/model"
)

const testProgressToken = "keepalive-token"

// keepAliveQuiet spans several shortened keep-alive intervals.
const keepAliveQuiet = 60 * time.Millisecond

// progressRecorder collects the progress notifications a client receives.
type progressRecorder struct {
	mu  sync.Mutex
	got []*mcpsdk.ProgressNotificationParams
}

func (r *progressRecorder) handle(_ context.Context, req *mcpsdk.ProgressNotificationClientRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, req.Params)
}

func (r *progressRecorder) snapshot() []*mcpsdk.ProgressNotificationParams {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*mcpsdk.ProgressNotificationParams(nil), r.got...)
}

// settle waits until the recorder count has not changed for quiet, or until a deadline passes,
// and returns the settled notifications.
func (r *progressRecorder) settle(quiet time.Duration) []*mcpsdk.ProgressNotificationParams {
	deadline := time.Now().Add(5 * time.Second)
	last := len(r.snapshot())
	lastChange := time.Now()

	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)

		if n := len(r.snapshot()); n != last {
			last = n
			lastChange = time.Now()
		} else if time.Since(lastChange) >= quiet {
			break
		}
	}

	return r.snapshot()
}

func setKeepAliveInterval(t *testing.T, d time.Duration) {
	t.Helper()

	prev := keepAliveInterval
	keepAliveInterval = d
	t.Cleanup(func() { keepAliveInterval = prev })
}

func connectProgressClient(t *testing.T, rec *progressRecorder) *mcpsdk.ClientSession {
	t.Helper()

	ctx := context.Background()
	server := newServer("test", time.Now(), "", &gate{blindlyAllow: true})

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0"}, &mcpsdk.ClientOptions{
		ProgressNotificationHandler: rec.handle,
	})
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

func callTool(t *testing.T, cs *mcpsdk.ClientSession, meta mcpsdk.Meta, name string, args map[string]any) map[string]any {
	t.Helper()

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Meta: meta, Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s) tool error: %+v", name, res.Content)
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("CallTool(%s) structured content = %T, want object", name, res.StructuredContent)
	}
	return out
}

// keepAliveScenario drives one blocking call to completion and returns its result.
type keepAliveScenario struct {
	Name string
	Run  func(t *testing.T, rec *progressRecorder, meta mcpsdk.Meta) map[string]any
}

var keepAliveScenarios = []keepAliveScenario{
	{
		Name: "cg_run",
		Run: func(t *testing.T, rec *progressRecorder, meta mcpsdk.Meta) map[string]any {
			cs := connectProgressClient(t, rec)
			return callTool(t, cs, meta, "cg_run", map[string]any{
				"command":         []string{"sh", "-c", "sleep 0.5; echo hi"},
				"wait_timeout_ms": 20000,
			})
		},
	},
	{
		Name: "cg_wait registry path",
		Run: func(t *testing.T, rec *progressRecorder, meta mcpsdk.Meta) map[string]any {
			cs := connectProgressClient(t, rec)
			started := callTool(t, cs, nil, "cg_run", map[string]any{
				"command": []string{"sh", "-c", "sleep 0.5; echo hi"},
				"wait":    false,
			})
			return callTool(t, cs, meta, "cg_wait", map[string]any{
				"id":         started["id"],
				"timeout_ms": 20000,
			})
		},
	},
	{
		Name: "cg_wait polling path",
		Run: func(t *testing.T, rec *progressRecorder, meta mcpsdk.Meta) map[string]any {
			starter := connectProgressClient(t, &progressRecorder{})
			started := callTool(t, starter, nil, "cg_run", map[string]any{
				"command": []string{"sh", "-c", "sleep 0.5; echo hi"},
				"wait":    false,
			})

			// A second server has an empty registry, so cg_wait has to poll the filesystem.
			cs := connectProgressClient(t, rec)
			return callTool(t, cs, meta, "cg_wait", map[string]any{
				"id":         started["id"],
				"timeout_ms": 20000,
			})
		},
	},
}

func TestKeepAliveTokenedCall(t *testing.T) {
	for _, tt := range keepAliveScenarios {
		t.Run(tt.Name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			setKeepAliveInterval(t, 10*time.Millisecond)

			rec := &progressRecorder{}
			out := tt.Run(t, rec, mcpsdk.Meta{"progressToken": testProgressToken})

			if finished, ok := out["finished"].(bool); ok && !finished {
				t.Fatalf("wait did not finish: %+v", out)
			}

			got := rec.settle(keepAliveQuiet)
			if len(got) < 2 {
				t.Fatalf("got %d notifications, want at least 2", len(got))
			}

			prev := -1.0
			for i, n := range got {
				if n.ProgressToken != testProgressToken {
					t.Errorf("notification %d token = %v, want %q", i, n.ProgressToken, testProgressToken)
				}
				if n.Progress <= prev {
					t.Errorf("notification %d progress = %v, want > %v", i, n.Progress, prev)
				}
				prev = n.Progress

				if n.Total != 0 {
					t.Errorf("notification %d total = %v, want unset", i, n.Total)
				}
				if !strings.HasPrefix(n.Message, "running ") || !strings.Contains(n.Message, "stdout ") || !strings.Contains(n.Message, "stderr ") {
					t.Errorf("notification %d message = %q, want run status", i, n.Message)
				}
			}

			if after := rec.settle(keepAliveQuiet); len(after) != len(got) {
				t.Errorf("%d notifications arrived after the result", len(after)-len(got))
			}
		})
	}
}

func TestKeepAliveUntokenedCall(t *testing.T) {
	for _, tt := range keepAliveScenarios {
		t.Run(tt.Name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			setKeepAliveInterval(t, 10*time.Millisecond)

			rec := &progressRecorder{}
			tt.Run(t, rec, nil)

			if got := rec.settle(keepAliveQuiet); len(got) != 0 {
				t.Errorf("got %d notifications without a progress token, want 0", len(got))
			}
		})
	}
}

func TestWaitWithKeepAliveTimeout(t *testing.T) {
	setKeepAliveInterval(t, 5*time.Millisecond)

	var mu sync.Mutex
	reports := 0
	report := func(context.Context) {
		mu.Lock()
		defer mu.Unlock()
		reports++
	}

	finished, err := waitWithKeepAlive(context.Background(), make(chan struct{}), nil, 80*time.Millisecond, report)
	if err != nil {
		t.Fatalf("waitWithKeepAlive: %v", err)
	}
	if finished {
		t.Errorf("finished = true, want false on timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if reports == 0 {
		t.Errorf("reports = 0, want at least one before the timeout")
	}
}

func TestWaitWithKeepAliveCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := waitWithKeepAlive(ctx, make(chan struct{}), nil, time.Minute, nil)
	if err == nil {
		t.Fatalf("expected context error")
	}
}

func TestNewProgressReporterWithoutToken(t *testing.T) {
	if r := newProgressReporter(nil, time.Now(), nil); r != nil {
		t.Errorf("reporter for nil request is non-nil")
	}

	req := &mcpsdk.CallToolRequest{Params: &mcpsdk.CallToolParamsRaw{}}
	if r := newProgressReporter(req, time.Now(), nil); r != nil {
		t.Errorf("reporter for request without session is non-nil")
	}
}

func TestRunStatus(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stdout"), make([]byte, 2048), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := runStatus(dir, 842*time.Second)
	want := "running " + model.FormatDuration(842*time.Second) + "; stdout 2.0KB, stderr 0B"
	if got != want {
		t.Errorf("runStatus = %q, want %q", got, want)
	}
}
