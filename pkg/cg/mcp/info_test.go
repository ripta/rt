package mcp

import (
	"os"
	"testing"
	"time"
)

func TestCollectInfoUptimeAndBuild(t *testing.T) {
	started := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	now := started.Add(1500 * time.Millisecond)

	out, err := collectInfo("v1.2.3", started, now)
	if err != nil {
		t.Fatalf("collectInfo: %v", err)
	}
	if !out.StartedAt.Equal(started) {
		t.Errorf("StartedAt = %v, want %v", out.StartedAt, started)
	}
	if out.UptimeMs != 1500 {
		t.Errorf("UptimeMs = %d, want 1500", out.UptimeMs)
	}
	if out.Build.Version != "v1.2.3" {
		t.Errorf("Build.Version = %q, want %q", out.Build.Version, "v1.2.3")
	}
	if out.Cwd == "" {
		t.Errorf("Cwd is empty")
	}
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
