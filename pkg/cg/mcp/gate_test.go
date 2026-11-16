package mcp

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/approve"
)

// plantScript writes an executable shell script under dir that echoes marker,
// and returns its path. Used to drive resolution and exec end-to-end.
func plantScript(t *testing.T, dir, name, marker string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body := "#!/bin/sh\necho " + marker + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("planting %s: %v", path, err)
	}
	return path
}

// canonicalPath resolves symlinks so an approval rule can name the executable's
// canonical path, which is what the gate matches after resolution.
func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", path, err)
	}
	return got
}

// newTestGate builds a gate from project YAML written under a fresh temp project
// root. Use newTestGateAt when the test needs the root path to inspect the file.
func newTestGate(t *testing.T, projectYAML string, blindly bool) *gate {
	t.Helper()
	return newTestGateAt(t, t.TempDir(), projectYAML, blindly)
}

// newTestGateAt builds a gate rooted at root. The global layer is pointed at a
// nonexistent path so the test never reads the real ~/.config/cg/approve.yaml.
// An empty projectYAML leaves the project file absent.
func newTestGateAt(t *testing.T, root, projectYAML string, blindly bool) *gate {
	t.Helper()

	if projectYAML != "" {
		if err := os.WriteFile(filepath.Join(root, ".cg.yaml"), []byte(projectYAML), 0o644); err != nil {
			t.Fatalf("write .cg.yaml: %v", err)
		}
	}

	store, err := approve.Load(approve.LoadOptions{
		GlobalPath:  filepath.Join(root, "no-global.yaml"),
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatalf("approve.Load: %v", err)
	}
	return &gate{store: store, blindlyAllow: blindly}
}

func TestGateAllowRuns(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\nallow:\n  - prefix: [echo]\n", false)
	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"echo", "hi"},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", out.ExitCode)
	}
}

func TestGateDenyRefusesWithMessage(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\ndeny:\n  - prefix: [rm, -rf]\n    message: delete specific paths instead\n", false)
	_, _, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"rm", "-rf", "x"},
	})
	if err == nil {
		t.Fatalf("expected refusal for denied command")
	}
	if !strings.Contains(err.Error(), "delete specific paths instead") {
		t.Errorf("err = %v, want the rule message surfaced", err)
	}
}

func TestGateEqualDenyRulesAreAllNamed(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	rules := "version: 1\ndeny:\n  - regex: '^rm( |$)'\n    as_basename: true\n    message: use trash\n  - prefix: [rm]\n    message: never rm\n"
	g := newTestGate(t, rules, false)

	_, _, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{Command: []string{"rm", "x"}})
	if err == nil {
		t.Fatalf("expected refusal for denied command")
	}
	for _, want := range []string{"2 equally specific deny rules", "never rm", "use trash"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
	if strings.Index(err.Error(), "never rm") > strings.Index(err.Error(), "use trash") {
		t.Errorf("err = %v, want the rules named in rule-text order, the prefix first", err)
	}
}

// TestRefusalErrorNamesTiedAllowRules builds the verdict by hand, since no allow field exists yet
// that two tied rules can differ in. It keeps the refusal's wording from rotting until one does.
func TestRefusalErrorNamesTiedAllowRules(t *testing.T) {
	_, doc, err := approve.ParseDocument([]byte("version: 1\nallow:\n  - prefix: [make]\n  - regex: '^make( |$)'\n    as_basename: true\n"))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	tied := []*approve.Rule{&doc.Allow[0], &doc.Allow[1]}

	got := refusalError("cg_run", approve.Verdict{Decision: approve.DecisionRefuse, Step: approve.StepTieRefusal, Rule: tied[0], Deciding: tied})
	for _, want := range []string{"cg_run refused", "the command matches equally specific allow rules that disagree", "prefix [make]", `regex "^make( |$)"`} {
		if !strings.Contains(got.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", got, want)
		}
	}

	chained := approve.Verdict{
		Decision: approve.DecisionRefuse,
		Rule:     tied[0],
		Deciding: tied,
		Chain: []approve.ChainLayer{
			{Offset: 0, Argv: []string{"nice", "make"}, Step: approve.StepPeel},
			{Offset: 1, Argv: []string{"make"}, Step: approve.StepTieRefusal},
		},
	}
	if got := refusalError("cg_run", chained); !strings.Contains(got.Error(), "the inner command `make` matches equally specific allow rules") {
		t.Errorf("err = %v, want it to name the inner layer", got)
	}
}

func TestGateRestrictRefusesWithMessage(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\nrestrict:\n  - prefix: [echo]\n    message: only echo hi is permitted here\n", false)
	_, _, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"echo", "bye"},
	})
	if err == nil {
		t.Fatalf("expected refusal for in-scope restricted command")
	}
	if !strings.Contains(err.Error(), "matches a restrict rule") {
		t.Errorf("err = %v, want it to name the restrict rule", err)
	}
	if !strings.Contains(err.Error(), "only echo hi is permitted here") {
		t.Errorf("err = %v, want the rule message surfaced", err)
	}
}

func TestGateRestrictAllowCarvesOut(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	// allow carves echo hi out of the echo restrict scope; echo bye stays refused.
	g := newTestGate(t, "version: 1\nallow:\n  - prefix: [echo, hi]\nrestrict:\n  - prefix: [echo]\n", false)

	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"echo", "hi"},
	})
	if err != nil {
		t.Fatalf("handleRun(echo hi): %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("echo hi ExitCode = %v, want 0", out.ExitCode)
	}

	_, _, err = handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"echo", "bye"},
	})
	if err == nil {
		t.Fatalf("expected echo bye to be refused by the restrict scope")
	}
	if !strings.Contains(err.Error(), "matches a restrict rule") {
		t.Errorf("err = %v, want it to name the restrict rule", err)
	}
}

func TestGateBuiltinDenyRefuses(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	_, _, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"sh", "-c", "echo hi"},
	})
	if err == nil {
		t.Fatalf("expected built-in deny to refuse sh")
	}
}

func TestGateBlindlyAllowBypass(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	// sh is in the built-in deny set; --blindly-allow runs it anyway.
	g := newTestGate(t, "version: 1\n", true)
	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"sh", "-c", "echo bypassed"},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !strings.Contains(out.StdoutExcerpt, "bypassed") {
		t.Errorf("StdoutExcerpt = %q, want to contain %q", out.StdoutExcerpt, "bypassed")
	}
}

func TestGateFailsClosedOnUnmatched(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\n", false)
	_, _, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"echo", "hi"},
	})
	if err == nil {
		t.Fatalf("expected fail-closed refusal for unmatched command")
	}
	if !strings.Contains(err.Error(), "--blindly-allow") {
		t.Errorf("err = %v, want a hint about --blindly-allow", err)
	}
}

func TestGateEnvGateRefuses(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\nallow:\n  - prefix: [echo]\n", false)
	_, _, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"echo", "hi"},
		Env:     map[string]string{"LD_PRELOAD": "evil.so"},
	})
	if err == nil {
		t.Fatalf("expected env-gate refusal")
	}
	if !strings.Contains(err.Error(), "LD_PRELOAD") {
		t.Errorf("err = %v, want the offending var named", err)
	}
}

func TestGateEnvGatePermitted(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGate(t, "version: 1\nallow:\n  - prefix: [echo]\n    permit_unsafe_envs: [PATH]\n", false)
	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"echo", "hi"},
		Env:     map[string]string{"PATH": os.Getenv("PATH")},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", out.ExitCode)
	}
}

func TestGateAllowAllPassesEnv(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	// allow-all short-circuits to run and does not apply the env gate.
	g := newTestGate(t, "version: 1\nmode: allow-all\n", false)
	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"echo", "hi"},
		Env:     map[string]string{"PATH": os.Getenv("PATH")},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", out.ExitCode)
	}
}

// TestGateSymlinkCanonicalAllow allows the executable by its canonical path and
// invokes it through a symlink in another directory. Resolution canonicalizes
// the link to its target, so the path-based allow rule matches and the program
// runs.
func TestGateSymlinkCanonicalAllow(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	realDir := t.TempDir()
	linkDir := t.TempDir()
	real := plantScript(t, realDir, "tool", "ran-real")
	link := filepath.Join(linkDir, "tool")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	yaml := "version: 1\nallow:\n  - prefix: ['" + canonicalPath(t, real) + "']\n"
	g := newTestGate(t, yaml, false)
	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{link},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !strings.Contains(out.StdoutExcerpt, "ran-real") {
		t.Errorf("StdoutExcerpt = %q, want it to contain %q", out.StdoutExcerpt, "ran-real")
	}
}

// TestGateSymlinkCanonicalDeny denies a directory by its canonical path and
// invokes an executable inside it through a symlink elsewhere. The deny fires on
// the canonicalized target even though the invoked path is the link.
func TestGateSymlinkCanonicalDeny(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	realDir := t.TempDir()
	linkDir := t.TempDir()
	real := plantScript(t, realDir, "tool", "should-not-run")
	link := filepath.Join(linkDir, "tool")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	denyDir := regexp.QuoteMeta(canonicalPath(t, realDir))
	yaml := "version: 1\ndeny:\n  - regex: '^" + denyDir + "/'\n    message: no executables from that directory\n"
	g := newTestGate(t, yaml, false)
	_, _, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{link},
	})
	if err == nil {
		t.Fatalf("expected refusal for canonical-path deny")
	}
	if !strings.Contains(err.Error(), "no executables from that directory") {
		t.Errorf("err = %v, want the rule message surfaced", err)
	}
}

// TestGateEnvPathDoesNotRedirectExec plants the same program name on the server
// PATH and in an off-PATH directory, then runs it with env.PATH pointed at the
// off-PATH copy. The server-PATH binary resolves and execs, so env.PATH cannot
// redirect the approved top-level executable; the dangerous-env gate still
// requires the explicit permit_unsafe_envs exemption to run at all.
func TestGateEnvPathDoesNotRedirectExec(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	serverDir := t.TempDir()
	evilDir := t.TempDir()
	plantScript(t, serverDir, "tool", "server")
	plantScript(t, evilDir, "tool", "evil")
	t.Setenv("PATH", serverDir)

	g := newTestGate(t, "version: 1\nallow:\n  - prefix: [tool]\n    permit_unsafe_envs: [PATH]\n", false)
	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"tool"},
		Env:     map[string]string{"PATH": evilDir},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !strings.Contains(out.StdoutExcerpt, "server") {
		t.Errorf("StdoutExcerpt = %q, want the server-PATH copy to run", out.StdoutExcerpt)
	}
	if strings.Contains(out.StdoutExcerpt, "evil") {
		t.Errorf("StdoutExcerpt = %q, env.PATH redirected the exec", out.StdoutExcerpt)
	}
}

// TestGateMultiplexerShimAllow mirrors rustup's layout: cargo and rustc are
// symlinks to one multiplexer binary, so their canonical paths coincide. An
// allow rule pinning the cargo shim's resolved path lets cargo run, and does not
// leak to rustc, whose resolved path names a different tool.
func TestGateMultiplexerShimAllow(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	binDir := t.TempDir()
	plantScript(t, binDir, "rustup", "proxied")
	for _, shim := range []string{"cargo", "rustc"} {
		if err := os.Symlink("rustup", filepath.Join(binDir, shim)); err != nil {
			t.Fatalf("symlink %s: %v", shim, err)
		}
	}
	t.Setenv("PATH", binDir)

	yaml := "version: 1\nallow:\n  - prefix: ['" + filepath.Join(binDir, "cargo") + "', fmt]\n"
	g := newTestGate(t, yaml, false)

	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"cargo", "fmt"},
	})
	if err != nil {
		t.Fatalf("handleRun(cargo fmt): %v", err)
	}
	if !strings.Contains(out.StdoutExcerpt, "proxied") {
		t.Errorf("StdoutExcerpt = %q, want the shim target to run", out.StdoutExcerpt)
	}

	_, _, err = handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{"rustc", "fmt"},
	})
	if err == nil {
		t.Fatalf("expected rustc fmt to fall through the cargo shim rule")
	}
}
