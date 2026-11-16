package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
)

// sandboxFixture is a project root, a home directory, and a directory of test scripts, all with
// symlinks resolved so Seatbelt path filters match them.
type sandboxFixture struct {
	t       *testing.T
	Root    string
	Home    string
	Scripts string

	Platform string
	Probe    func(profile string, defines []string) model.SandboxProbe
}

func newSandboxFixture(t *testing.T) *sandboxFixture {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())

	// The probe scripts read the inherited proxy variables, so a host that exports them would
	// change what the real-sandbox tests observe.
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(name, "")
	}

	real := func() string {
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}

		return dir
	}

	return &sandboxFixture{t: t, Root: real(), Home: real(), Scripts: real(), Platform: "darwin"}
}

// fakeProbe returns a probe that reports a fixed class, so gate tests need no sandbox-exec.
func fakeProbe(class model.SandboxClass, stderr string) func(string, []string) model.SandboxProbe {
	return func(string, []string) model.SandboxProbe {
		return model.SandboxProbe{Class: class, Stderr: stderr}
	}
}

func (f *sandboxFixture) write(path, content string, mode os.FileMode) string {
	f.t.Helper()

	if !filepath.IsAbs(path) {
		path = filepath.Join(f.Root, path)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		f.t.Fatal(err)
	}

	return path
}

// load builds a gate over the project file. Probe and Platform default to the real ones.
func (f *sandboxFixture) load(project string, blindly bool) *gate {
	f.t.Helper()

	f.write(".cg.yaml", project, 0o644)

	store, err := approve.Load(approve.LoadOptions{
		GlobalPath:  filepath.Join(f.Root, "no-global.yaml"),
		ProjectRoot: f.Root,
		Home:        func() (string, error) { return f.Home, nil },
		Platform:    f.Platform,
		Probe:       f.Probe,
	})
	if err != nil {
		f.t.Fatalf("approve.Load: %v", err)
	}

	return &gate{store: store, blindlyAllow: blindly}
}

// denyWritesProfile denies writes under the project and the home directory, the two parameters cg
// passes, and allows everything else.
const denyWritesProfile = "(version 1)\n(allow default)\n(deny file-write* (subpath (param \"PROJECT_PATH\")))\n(deny file-write* (subpath (param \"HOME\")))\n"

// probeScript reports the proxy it was given and whether it can write into the directory it is passed.
const probeScript = `#!/bin/sh
echo "proxy=${HTTPS_PROXY:-none} noproxy=${NO_PROXY:-none}"
if echo x > "$1/probe.out" 2>/dev/null; then echo wrote; else echo denied; fi
`

func requireSandboxExec(t *testing.T) {
	t.Helper()

	if runtime.GOOS != "darwin" {
		t.Skipf("seatbelt applies on darwin only, not %s", runtime.GOOS)
	}

	if probe := model.ProbeSeatbelt("(version 1)\n(allow default)\n", nil); probe.Class != model.SandboxApplies {
		t.Skipf("sandbox-exec cannot apply a profile here: class %s, stderr %q", probe.Class, probe.Stderr)
	}
}

func sandboxRules(f *sandboxFixture, script string, extra string) string {
	return "version: 2\nsandboxes:\n" +
		"  go:\n    seatbelt: {profile: p.sb, already_sandboxed: refuse}\n" +
		"  proxied:\n    seatbelt: {profile: p.sb, already_sandboxed: refuse, proxy: 'http://localhost:3128'}\n" +
		"allow:\n  - prefix: [" + script + ", " + f.Root + "]\n    sandbox: go\n" + extra
}

func runOutputOf(t *testing.T, dir string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestSandboxedRunAppliesProfileAndRecordsTypedCommand(t *testing.T) {
	requireSandboxExec(t)

	f := newSandboxFixture(t)
	f.write("p.sb", denyWritesProfile, 0o644)
	script := f.write(filepath.Join(f.Scripts, "probe"), probeScript, 0o755)

	g := f.load(sandboxRules(f, script, "  - prefix: ["+script+", "+f.Scripts+"]\n"), false)

	command := []string{script, f.Root}
	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{Command: command})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 || out.StdoutExcerpt != "proxy=none noproxy=none\ndenied\n" {
		t.Fatalf("output = %+v, want the profile's write denial and no proxy variables", out)
	}

	meta, err := model.ReadMeta(filepath.Join(model.CaptureRoot(), out.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(meta.Command, command) || meta.Sandbox != "go" {
		t.Errorf("record = command %q sandbox %q, want the typed command and the sandbox name", meta.Command, meta.Sandbox)
	}

	_, out, err = handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{Command: []string{script, f.Scripts}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !strings.HasSuffix(out.StdoutExcerpt, "wrote\n") {
		t.Errorf("output = %q, want a rule with no sandbox to run unrestricted", out.StdoutExcerpt)
	}
}

func TestSandboxedRunProxyEnvironment(t *testing.T) {
	requireSandboxExec(t)

	f := newSandboxFixture(t)
	f.write("p.sb", denyWritesProfile, 0o644)
	script := f.write(filepath.Join(f.Scripts, "probe"), probeScript, 0o755)

	rules := sandboxRules(f, script, "  - prefix: ["+script+", "+f.Scripts+"]\n    sandbox: proxied\n")
	g := f.load(rules, false)

	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{Command: []string{script, f.Scripts}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if want := "proxy=http://localhost:3128 noproxy=localhost,127.0.0.1,::1\nwrote\n"; out.StdoutExcerpt != want {
		t.Errorf("output = %q, want %q", out.StdoutExcerpt, want)
	}

	_, out, err = handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{
		Command: []string{script, f.Scripts},
		Env:     map[string]string{"HTTPS_PROXY": "http://caller:1"},
	})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !strings.HasPrefix(out.StdoutExcerpt, "proxy=http://caller:1 ") {
		t.Errorf("output = %+v, want the caller's env to override the sandbox's", out)
	}

	_, out, err = handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{Command: []string{script, f.Root}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !strings.HasPrefix(out.StdoutExcerpt, "proxy=none ") {
		t.Errorf("output = %+v, want a sandbox with no proxy key to set none", out)
	}
}

func TestSandboxedRunManyGivesEachMemberItsOwnSandboxAndProxy(t *testing.T) {
	requireSandboxExec(t)

	f := newSandboxFixture(t)
	f.write("p.sb", denyWritesProfile, 0o644)
	script := f.write(filepath.Join(f.Scripts, "probe"), probeScript, 0o755)

	rules := sandboxRules(f, script, "  - prefix: ["+script+", "+f.Scripts+"]\n    sandbox: proxied\n  - prefix: ["+script+", "+f.Home+"]\n")
	g := f.load(rules, false)

	commands := [][]string{{script, f.Root}, {script, f.Scripts}, {script, f.Home}}

	_, out, err := handleRunMany(context.Background(), nil, g, nil, runOrigin{}, runManyInput{Commands: commands, Repeat: 2})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if len(out.Runs) != 6 {
		t.Fatalf("runs = %d, want 6", len(out.Runs))
	}

	want := []string{
		"proxy=none noproxy=none\ndenied\n",
		"proxy=http://localhost:3128 noproxy=localhost,127.0.0.1,::1\nwrote\n",
		"proxy=none noproxy=none\nwrote\n",
	}

	for _, run := range out.Runs {
		got := runOutputOf(t, filepath.Join(model.CaptureRoot(), run.RunID))
		if got != want[run.Command] {
			t.Errorf("command %d output = %q, want %q", run.Command, got, want[run.Command])
		}
	}

	if !slices.Equal(out.Commands[0], commands[0]) {
		t.Errorf("recorded commands = %v, want the typed commands", out.Commands)
	}
}

// TestSandboxedRunKeepsTheInvokedPath pins what argv[0] a sandboxed command sees. A multiplexer shim
// dispatches on the basename of argv[0], and a venv interpreter is a link to its base interpreter. Both
// only work when the command is exec'd through the path the user invoked, not the canonical one.
func TestSandboxedRunKeepsTheInvokedPath(t *testing.T) {
	requireSandboxExec(t)

	f := newSandboxFixture(t)
	f.write("p.sb", "(version 1)\n(allow default)\n", 0o644)

	shim := f.write(filepath.Join(f.Scripts, "rustup"), "#!/bin/sh\ncase \"$(basename \"$0\")\" in\n  cargo) echo cargo-dispatch ;;\n  *) echo rustup-itself ;;\nesac\n", 0o755)
	cargo := filepath.Join(f.Scripts, "cargo")
	if err := os.Symlink(shim, cargo); err != nil {
		t.Fatal(err)
	}

	base := f.write(filepath.Join(f.Scripts, "python3.99"), "#!/bin/sh\necho \"$0\"\n", 0o755)
	venv := filepath.Join(f.Scripts, "venv", "bin")
	if err := os.MkdirAll(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(venv, "python")
	if err := os.Symlink(base, python); err != nil {
		t.Fatal(err)
	}

	rules := "version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: p.sb, already_sandboxed: refuse}\nallow:\n" +
		"  - prefix: [" + cargo + "]\n    sandbox: go\n  - prefix: [" + python + "]\n    sandbox: go\n"
	g := f.load(rules, false)

	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{Command: []string{cargo}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.StdoutExcerpt != "cargo-dispatch\n" {
		t.Errorf("shim output = %q, want the shim to dispatch to cargo", out.StdoutExcerpt)
	}

	_, out, err = handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{Command: []string{python}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if out.StdoutExcerpt != python+"\n" {
		t.Errorf("interpreter output = %q, want the venv path %q", out.StdoutExcerpt, python)
	}
}

func TestSandboxedRunWrappedCommandRunsUnderInnerSandbox(t *testing.T) {
	requireSandboxExec(t)

	if _, err := exec.LookPath("nice"); err != nil {
		t.Skipf("nice is not installed: %v", err)
	}

	f := newSandboxFixture(t)
	f.write("p.sb", denyWritesProfile, 0o644)
	script := f.write(filepath.Join(f.Scripts, "probe"), probeScript, 0o755)

	rules := sandboxRules(f, script, "  - wrapper: [nice, -n, '19']\n")
	g := f.load(rules, false)

	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{Command: []string{"nice", "-n", "19", script, f.Root}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !strings.HasSuffix(out.StdoutExcerpt, "denied\n") {
		t.Errorf("output = %q, want the inner command sandboxed by its own rule", out.StdoutExcerpt)
	}
}

func TestSandboxedRunBrokenImportRefusesWithCompileError(t *testing.T) {
	requireSandboxExec(t)

	f := newSandboxFixture(t)

	imported := f.write(filepath.Join(f.Scripts, "extra.sb"), "(version 1)\n", 0o644)
	f.write("p.sb", "(version 1)\n(allow default)\n(import \""+imported+"\")\n", 0o644)
	script := f.write(filepath.Join(f.Scripts, "probe"), probeScript, 0o755)

	g := f.load(sandboxRules(f, script, ""), false)

	f.write(imported, "(this-is-not-a-form", 0o644)

	_, out, err := handleRun(context.Background(), nil, g, nil, runOrigin{}, runInput{Command: []string{script, f.Root}})
	if err != nil {
		t.Fatalf("handleRun: %v", err)
	}
	if !strings.Contains(out.StartError, "no longer compiles") || out.ExitCode != nil {
		t.Errorf("output = %+v, want a start error naming the compile failure, not the command's exit status", out)
	}
}

type sandboxGateTest struct {
	Name     string
	Platform string
	Class    model.SandboxClass
	Stderr   string
	Command  []string
	WantErr  string
}

var sandboxGateRefusalTests = []sandboxGateTest{
	{
		Name:     "a platform with no block for the sandbox refuses",
		Platform: "linux",
		Class:    model.SandboxApplies,
		WantErr:  `sandbox "go", which has no block for this platform`,
	},
	{
		Name:     "a profile that classifies as unknown refuses with the probe's stderr",
		Platform: "darwin",
		Class:    model.SandboxUnknown,
		Stderr:   "sandbox-exec: something odd",
		WantErr:  "classified it as unknown: sandbox-exec: something odd",
	},
	{
		Name:     "a command whose executable does not resolve refuses",
		Platform: "darwin",
		Class:    model.SandboxApplies,
		Command:  []string{"definitely-not-a-real-program-xyz", "a"},
		WantErr:  "did not resolve",
	},
}

func TestGateRefusesSandboxThatCannotBeApplied(t *testing.T) {
	for _, test := range sandboxGateRefusalTests {
		t.Run(test.Name, func(t *testing.T) {
			f := newSandboxFixture(t)
			f.Platform = test.Platform
			f.Probe = fakeProbe(test.Class, test.Stderr)
			f.write("p.sb", "(version 1)\n", 0o644)

			for _, blindly := range []bool{false, true} {
				g := f.load("version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: p.sb, already_sandboxed: run}\nallow:\n  - prefix: [echo]\n    sandbox: go\n  - prefix: [definitely-not-a-real-program-xyz]\n    sandbox: go\n", blindly)

				command := test.Command
				if command == nil {
					command = []string{"echo", "hi"}
				}

				resolved, _ := model.ResolveCommand(command, "")
				call := approvalCall{Tool: "cg_run", Argvs: [][]string{command}, Resolutions: []*model.Resolution{resolved}}

				if _, err := g.check(call); err == nil || !strings.Contains(err.Error(), test.WantErr) {
					t.Errorf("blindly=%v: check error = %v, want it to contain %q", blindly, err, test.WantErr)
				}
			}
		})
	}
}

func TestGateSandboxFollowsTheDecidingRule(t *testing.T) {
	f := newSandboxFixture(t)
	f.Probe = fakeProbe(model.SandboxApplies, "")
	f.write("p.sb", "(version 1)\n", 0o644)

	rules := "version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: p.sb, already_sandboxed: run}\n  go-loopback:\n    seatbelt: {profile: p.sb, already_sandboxed: run, params: {LOOPBACK: '1'}}\n" +
		"allow:\n  - prefix: [echo]\n    sandbox: go\n  - prefix: [echo, loop]\n    sandbox: go-loopback\n"

	sandboxOf := func(g *gate, argv ...string) string {
		t.Helper()

		resolved, _ := model.ResolveCommand(argv, "")
		out, err := g.check(approvalCall{Tool: "cg_run", Argvs: [][]string{argv}, Resolutions: []*model.Resolution{resolved}})
		if err != nil {
			t.Fatalf("check %v: %v", argv, err)
		}
		if sb := out.sandboxAt(0); sb != nil {
			return sb.Name
		}

		return ""
	}

	check := func(g *gate, label string) {
		t.Helper()

		if got := sandboxOf(g, "echo", "loop", "x"); got != "go-loopback" {
			t.Errorf("%s: narrow command sandbox = %q, want go-loopback", label, got)
		}
		if got := sandboxOf(g, "echo", "x"); got != "go" {
			t.Errorf("%s: broad command sandbox = %q, want go", label, got)
		}
	}

	g := f.load(rules, false)
	check(g, "at start")

	if err := g.store.AppendProjectAllowPrefix([]string{"ls"}, approve.WriteDirect); err != nil {
		t.Fatalf("remember: %v", err)
	}
	check(g, "after a remember")

	restarted, err := approve.Load(approve.LoadOptions{
		GlobalPath:  filepath.Join(f.Root, "no-global.yaml"),
		ProjectRoot: f.Root,
		Home:        func() (string, error) { return f.Home, nil },
		Platform:    f.Platform,
		Probe:       f.Probe,
	})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	check(&gate{store: restarted}, "after a restart")
}

func TestGateSandboxUnderAllowAllAndBlindlyAllow(t *testing.T) {
	f := newSandboxFixture(t)
	f.Probe = fakeProbe(model.SandboxApplies, "")
	f.write("p.sb", "(version 1)\n", 0o644)

	if _, err := exec.LookPath("nice"); err != nil {
		t.Skipf("nice is not installed: %v", err)
	}

	echo, _ := model.ResolveCommand([]string{"echo"}, "")

	body := "sandboxes:\n  go:\n    seatbelt: {profile: p.sb, already_sandboxed: run}\n" +
		"allow:\n  - wrapper: [nice, -n, '19']\n  - prefix: [" + echo.Resolved + "]\n    sandbox: go\n"

	modes := map[string]*gate{
		"mode allow-all":  f.load("version: 2\nmode: allow-all\n"+body, false),
		"--blindly-allow": f.load("version: 2\n"+body, true),
	}

	for name, g := range modes {
		call := func(argv ...string) approvalCall {
			resolved, _ := model.ResolveCommand(argv, "")

			return approvalCall{
				Tool: "cg_run", Argvs: [][]string{argv}, Resolutions: []*model.Resolution{resolved},
				Env: map[string]string{"PATH": "/x"},
			}
		}

		out, err := g.check(call("nice", "-n", "19", "echo", "hi"))
		if err != nil {
			t.Fatalf("%s: check: %v", name, err)
		}
		if sb := out.sandboxAt(0); sb == nil || sb.Name != "go" {
			t.Errorf("%s: wrapped command sandbox = %v, want go despite the dangerous env", name, sb)
		}
		if got := out.Rewrites[0][3]; got != echo.Resolved {
			t.Errorf("%s: inner pin = %q, want the literal resolved path %q", name, got, echo.Resolved)
		}

		out, err = g.check(call("sh", "-c", "echo hi"))
		if err != nil {
			t.Fatalf("%s: check: %v", name, err)
		}
		if sb := out.sandboxAt(0); sb != nil {
			t.Errorf("%s: unmatched command sandbox = %v, want it to run unsandboxed", name, sb.Name)
		}
		if out.Rewrites[0] != nil {
			t.Errorf("%s: unsandboxed command rewrites = %v, want none, as before", name, out.Rewrites[0])
		}
	}
}

func TestGateTiedSandboxesRefuseAndNameEveryRule(t *testing.T) {
	f := newSandboxFixture(t)
	f.Probe = fakeProbe(model.SandboxApplies, "")
	f.write("p.sb", "(version 1)\n", 0o644)

	rules := "version: 2\nsandboxes:\n  a:\n    seatbelt: {profile: p.sb, already_sandboxed: run}\n  b:\n    seatbelt: {profile: p.sb, already_sandboxed: run}\n" +
		"allow:\n  - prefix: [echo]\n    sandbox: a\n  - prefix: [echo]\n    sandbox: a\n    permit_unsafe_envs: [FOO]\n  - prefix: [echo]\n    sandbox: b\n"

	for _, blindly := range []bool{false, true} {
		g := f.load(rules, blindly)
		resolved, _ := model.ResolveCommand([]string{"echo", "x"}, "")

		_, err := g.check(approvalCall{Tool: "cg_run", Argvs: [][]string{{"echo", "x"}}, Resolutions: []*model.Resolution{resolved}})
		if err == nil || !strings.Contains(err.Error(), "disagree") || strings.Count(err.Error(), "prefix [echo]") != 3 {
			t.Errorf("blindly=%v: error = %v, want a tie refusal that names all three tied rules", blindly, err)
		}
	}
}

func TestGateDangerousEnvNeverMovesToBroaderRulesSandbox(t *testing.T) {
	f := newSandboxFixture(t)
	f.Probe = fakeProbe(model.SandboxApplies, "")
	f.write("p.sb", "(version 1)\n", 0o644)

	rules := "version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: p.sb, already_sandboxed: run}\n  loose:\n    seatbelt: {profile: p.sb, already_sandboxed: run}\n" +
		"allow:\n  - prefix: [echo, hi]\n    sandbox: go\n  - prefix: [echo]\n    sandbox: loose\n    permit_unsafe_envs: [PYTHONPATH]\n"
	g := f.load(rules, false)

	resolved, _ := model.ResolveCommand([]string{"echo", "hi"}, "")
	call := approvalCall{Tool: "cg_run", Argvs: [][]string{{"echo", "hi"}}, Resolutions: []*model.Resolution{resolved}, Env: map[string]string{"PYTHONPATH": "/x"}}

	if _, err := g.check(call); err == nil || !strings.Contains(err.Error(), "PYTHONPATH") {
		t.Errorf("check error = %v, want the deciding rule to refuse the env override", err)
	}
}

func TestSandboxedRunManyBrokenImportRefusesEveryMember(t *testing.T) {
	requireSandboxExec(t)

	f := newSandboxFixture(t)

	imported := f.write(filepath.Join(f.Scripts, "extra.sb"), "(version 1)\n", 0o644)
	f.write("p.sb", "(version 1)\n(allow default)\n(import \""+imported+"\")\n", 0o644)
	script := f.write(filepath.Join(f.Scripts, "probe"), probeScript, 0o755)

	g := f.load(sandboxRules(f, script, ""), false)

	f.write(imported, "(this-is-not-a-form", 0o644)

	_, out, err := handleRunMany(context.Background(), nil, g, nil, runOrigin{}, runManyInput{Commands: [][]string{{script, f.Root}}, Repeat: 3})
	if err != nil {
		t.Fatalf("handleRunMany: %v", err)
	}
	if len(out.Runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(out.Runs))
	}
	for i, run := range out.Runs {
		if !strings.Contains(run.StartError, "no longer compiles") {
			t.Errorf("run %d = %+v, want each repeat refused by its own preflight", i, run)
		}
	}
}
