package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCollectInfoUptimeAndBuild(t *testing.T) {
	started := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	now := started.Add(1500 * time.Millisecond)

	out, err := collectInfo("v1.2.3", started, "SESS", now)
	if err != nil {
		t.Fatalf("collectInfo: %v", err)
	}
	if !out.StartedAt.Equal(started) {
		t.Errorf("StartedAt = %v, want %v", out.StartedAt, started)
	}
	if out.UptimeMs != 1500 {
		t.Errorf("UptimeMs = %d, want 1500", out.UptimeMs)
	}
	if out.SessionID != "SESS" {
		t.Errorf("SessionID = %q, want %q", out.SessionID, "SESS")
	}
	if out.Build.Version != "v1.2.3" {
		t.Errorf("Build.Version = %q, want %q", out.Build.Version, "v1.2.3")
	}
	if out.Cwd == "" {
		t.Errorf("Cwd is empty")
	}
}

// TestTwoServersReportDistinctSessionIDs confirms cg_info echoes the server's
// own session ID over the wire, so two differently-minted servers report
// different IDs. The IDs are fixed here rather than minted; distinctness of
// freshly-minted IDs is the generator's contract, covered in the model package.
func TestTwoServersReportDistinctSessionIDs(t *testing.T) {
	if got := serverInfoSessionID(t, "S1"); got != "S1" {
		t.Errorf("first server session_id = %q, want %q", got, "S1")
	}
	if got := serverInfoSessionID(t, "S2"); got != "S2" {
		t.Errorf("second server session_id = %q, want %q", got, "S2")
	}
}

// serverInfoSessionID builds a server with sessionID, calls cg_info over an
// in-memory transport, and returns the reported session_id.
func serverInfoSessionID(t *testing.T, sessionID string) string {
	t.Helper()

	ctx := context.Background()
	server := newServer("test", time.Now(), sessionID, &gate{blindlyAllow: true})

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

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "cg_info"})
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
	var out infoOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshaling info: %v", err)
	}
	return out.SessionID
}

func TestAllowedEnvReportsOnlySetAllowlistedVars(t *testing.T) {
	t.Setenv("USER", "tester")
	t.Setenv("SECRET_TOKEN", "do-not-leak")

	env := allowedEnv()

	if env["USER"] != "tester" {
		t.Errorf("USER = %q, want %q", env["USER"], "tester")
	}
	if _, ok := env["SECRET_TOKEN"]; ok {
		t.Errorf("SECRET_TOKEN leaked into env output")
	}
	for k := range env {
		allowed := false
		for _, a := range infoEnvAllowlist {
			if a == k {
				allowed = true
				break
			}
		}
		if !allowed {
			t.Errorf("reported non-allowlisted variable %q", k)
		}
	}
}

func TestAllowedEnvOmitsUnsetVars(t *testing.T) {
	// t.Setenv registers cleanup, then immediately unset so the variable is
	// absent for the duration of the test but restored afterward.
	t.Setenv("TMPDIR", "placeholder")
	if err := os.Unsetenv("TMPDIR"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}
	if _, ok := allowedEnv()["TMPDIR"]; ok {
		t.Errorf("unset allowlisted variable reported in env output")
	}
}
