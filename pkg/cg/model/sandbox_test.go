package model

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// permissiveProfile applies and restricts nothing, so a run under it proves the sandbox-exec
// plumbing without depending on what a profile denies.
const permissiveProfile = "(version 1)\n(allow default)\n"

// requireSeatbelt skips a test that applies a real profile when sandbox-exec cannot apply one here.
// That is every non-darwin host, and any process already inside a sandbox.
func requireSeatbelt(t *testing.T) {
	t.Helper()

	if runtime.GOOS != "darwin" {
		t.Skipf("seatbelt applies on darwin only, not %s", runtime.GOOS)
	}

	probe := ProbeSeatbelt(permissiveProfile, nil)
	if probe.Class != SandboxApplies {
		t.Skipf("sandbox-exec cannot apply a profile here: class %s, stderr %q", probe.Class, probe.Stderr)
	}
}

type probeClassTest struct {
	Name     string
	Exit     int
	Stderr   string
	SpawnErr bool
	Want     SandboxClass
	Compile  bool
}

var probeClassTests = []probeClassTest{
	{Name: "clean exit applies", Exit: 0, Want: SandboxApplies},
	{Name: "exec failure applies", Exit: 71, Stderr: "sandbox-exec: execvp() of '/usr/bin/true' failed: Operation not permitted\n", Want: SandboxApplies},
	{Name: "exact nesting line is nested", Exit: 71, Stderr: "sandbox-exec: sandbox_apply: Operation not permitted\n", Want: SandboxNested},
	{Name: "nesting line with extra text is unknown", Exit: 71, Stderr: "sandbox-exec: sandbox_apply: Operation not permitted\nmore\n", Want: SandboxUnknown},
	{Name: "other 71 text is unknown", Exit: 71, Stderr: "sandbox-exec: something else\n", Want: SandboxUnknown},
	{Name: "65 is a compile failure", Exit: 65, Stderr: "sandbox-exec: invalid profile\n", Want: SandboxUnknown, Compile: true},
	{Name: "another status is unknown", Exit: 2, Want: SandboxUnknown},
	{Name: "a spawn error is unknown", SpawnErr: true, Want: SandboxUnknown},
}

func TestClassifyProbe(t *testing.T) {
	for _, test := range probeClassTests {
		t.Run(test.Name, func(t *testing.T) {
			var spawnErr error
			if test.SpawnErr {
				spawnErr = os.ErrNotExist
			}

			got := classifyProbe(test.Exit, test.Stderr, spawnErr)
			if got.Class != test.Want || got.CompileFailed != test.Compile {
				t.Errorf("classifyProbe = {%s compile=%v}, want {%s compile=%v}", got.Class, got.CompileFailed, test.Want, test.Compile)
			}
		})
	}
}

func TestProbeSeatbeltCompileFailure(t *testing.T) {
	requireSeatbelt(t)

	probe := ProbeSeatbelt("(version 1)\n(this-is-not-a-form", nil)
	if !probe.CompileFailed || probe.Stderr == "" {
		t.Errorf("probe = %+v, want a compile failure with the compiler's message", probe)
	}
}

func TestProbeSeatbeltDeniedTrueStillApplies(t *testing.T) {
	requireSeatbelt(t)

	probe := ProbeSeatbelt("(version 1)\n(allow default)\n(deny process-exec (literal \"/usr/bin/true\"))\n", nil)
	if probe.CompileFailed || probe.Class != SandboxApplies {
		t.Errorf("probe = %+v, want a healthy profile that denies running /usr/bin/true to classify as applies", probe)
	}
}

func TestProbeSeatbeltPassesDefines(t *testing.T) {
	requireSeatbelt(t)

	broken := "(version 1)\n(allow default)\n(if (param \"X\") (this-is-not-a-form))\n"

	if probe := ProbeSeatbelt(broken, nil); probe.CompileFailed {
		t.Fatalf("probe without the parameter = %+v, want the guarded form skipped", probe)
	}
	if probe := ProbeSeatbelt(broken, []string{"X=1"}); !probe.CompileFailed {
		t.Errorf("probe with the parameter = %+v, want the guarded form to fail the compile", probe)
	}
}

// sandboxedSpec builds the launch of argv under a profile, the way the server does.
func sandboxedSpec(t *testing.T, profile string, argv []string) (*SandboxSpec, []string, *Resolution) {
	t.Helper()

	r, err := ResolveCommand(argv, "")
	if err != nil {
		t.Fatalf("ResolveCommand(%v): %v", argv, err)
	}

	sb := &SandboxSpec{Name: "test", Command: argv, Resolved: r.Resolved, Canonical: r.Canonical, Profile: profile, Defines: []string{"HOME=" + t.TempDir()}}
	encoded, res := SandboxLaunch(sb, nil)

	return sb, encoded, res
}

func TestSandboxedRunPidIsTheCommandPid(t *testing.T) {
	requireSeatbelt(t)
	t.Setenv("TMPDIR", t.TempDir())

	sb, argv, res := sandboxedSpec(t, permissiveProfile, []string{"/bin/sh", "-c", "echo $$; sleep 30"})

	run, err := RunSupervised(argv, SuperviseOptions{Resolved: res, Sandbox: sb})
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
	}

	var printed string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(run.Dir, "stdout"))
		if printed = strings.TrimSpace(string(data)); printed != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if printed != strconv.Itoa(run.Pid) {
		t.Fatalf("command printed pid %q, ack reported %d: sandbox-exec must exec the command in place", printed, run.Pid)
	}

	res2, err := CancelRun(context.Background(), run.ID, CancelOptions{})
	if err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if !res2.Signaled {
		t.Errorf("cancel result = %+v, want signaled", res2)
	}

	select {
	case <-run.Done:
	case <-time.After(5 * time.Second):
		t.Fatalf("run %s kept running after cancel", run.ID)
	}
}

func TestSandboxLaunchEncoding(t *testing.T) {
	sb := &SandboxSpec{
		Name:      "go",
		Command:   []string{"nice", "-n", "19", "go", "test"},
		Resolved:  "/bin/nice",
		Canonical: "/bin/nice",
		Profile:   "(version 1)",
		Defines:   []string{"HOME=/h", "PROJECT_PATH=/p"},
	}

	argv, res := SandboxLaunch(sb, map[int]string{3: "/opt/go/bin/go"})

	want := []string{SandboxExecPath, "-p", "(version 1)", "-D", "HOME=/h", "-D", "PROJECT_PATH=/p", "/bin/nice", "-n", "19", "/opt/go/bin/go", "test"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
	if res.Resolved != SandboxExecPath || res.Canonical != SandboxExecPath {
		t.Errorf("resolution = %+v, want sandbox-exec as both paths", res)
	}
}

func TestSuperviseSandboxRecordsTypedCommand(t *testing.T) {
	requireSeatbelt(t)
	cap := newSuperviseDir(t)

	sb, argv, res := sandboxedSpec(t, permissiveProfile, []string{"/bin/echo", "hi"})

	spec := &SuperviseSpec{Argv: argv, Resolved: res.Resolved, Canonical: res.Canonical, Sandbox: sb}
	if _, _, err := runSupervise(t, cap.Dir, spec); err != nil {
		t.Fatalf("superviseRun: %v", err)
	}

	meta, err := ReadMeta(cap.Dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if !slices.Equal(meta.Command, []string{"/bin/echo", "hi"}) || meta.Sandbox != "test" {
		t.Errorf("record = command %q sandbox %q, want the typed command and the sandbox name", meta.Command, meta.Sandbox)
	}

	out, _ := os.ReadFile(filepath.Join(cap.Dir, "stdout"))
	if string(out) != "hi\n" {
		t.Errorf("stdout = %q, want the command's own output", out)
	}
}

func TestSuperviseSandboxRefusesBrokenImport(t *testing.T) {
	requireSeatbelt(t)
	cap := newSuperviseDir(t)

	imported := filepath.Join(t.TempDir(), "extra.sb")
	if err := os.WriteFile(imported, []byte("(version 1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	profile := "(version 1)\n(allow default)\n(import \"" + imported + "\")\n"
	sb, argv, res := sandboxedSpec(t, profile, []string{"/bin/echo", "hi"})

	if err := os.WriteFile(imported, []byte("(this-is-not-a-form"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := &SuperviseSpec{Argv: argv, Resolved: res.Resolved, Canonical: res.Canonical, Sandbox: sb}
	ack, _, err := runSupervise(t, cap.Dir, spec)
	if err == nil || ack.Started {
		t.Fatalf("ack = %+v, err = %v, want the run refused before it starts", ack, err)
	}
	if !strings.Contains(ack.StartError, "no longer compiles") {
		t.Errorf("start error = %q, want the compile error, not the command's exit status", ack.StartError)
	}
}

func TestSuperviseSandboxRefusesRetargetedCommand(t *testing.T) {
	requireSeatbelt(t)
	cap := newSuperviseDir(t)

	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	link := filepath.Join(dir, "tool")
	for _, p := range []string{first, second} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\necho "+filepath.Base(p)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}

	sb, argv, res := sandboxedSpec(t, permissiveProfile, []string{link})

	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}

	spec := &SuperviseSpec{Argv: argv, Resolved: res.Resolved, Canonical: res.Canonical, Sandbox: sb}
	ack, _, err := runSupervise(t, cap.Dir, spec)
	if err == nil || ack.Started || !strings.Contains(ack.StartError, "changed since it was approved") {
		t.Errorf("ack = %+v, err = %v, want a refusal because the command's canonical path moved", ack, err)
	}
}

// TestSuperviseSandboxReadsCanonicalFromSpecField pins that the pre-spawn check reads the canonical
// path from the sandbox field. The encoded argv and top-level paths name sandbox-exec, which never
// changes, so a check that read only those would pass a retargeted command.
func TestSuperviseSandboxReadsCanonicalFromSpecField(t *testing.T) {
	requireSeatbelt(t)
	cap := newSuperviseDir(t)

	sb, argv, res := sandboxedSpec(t, permissiveProfile, []string{"/bin/echo", "hi"})
	sb.Canonical = "/definitely/not/echo"

	spec := &SuperviseSpec{Argv: argv, Resolved: res.Resolved, Canonical: res.Canonical, Sandbox: sb}
	ack, _, err := runSupervise(t, cap.Dir, spec)
	if err == nil || ack.Started {
		t.Errorf("ack = %+v, err = %v, want a refusal from the sandbox field's canonical path", ack, err)
	}
}

// TestSuperviseSandboxUnderPrePinSupervisor decodes the encoded spec the way a supervisor built before
// the inner-exec pin and before the sandbox field does. That supervisor only execs the top-level
// resolved path with argv. The run must still execute the approved path, sandboxed.
func TestSuperviseSandboxUnderPrePinSupervisor(t *testing.T) {
	requireSeatbelt(t)
	cap := newSuperviseDir(t)

	scriptDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(scriptDir, "tool")
	body := "#!/bin/sh\necho ran-approved\nif echo x > " + filepath.Join(scriptDir, "outside") + " 2>/dev/null; then echo wrote; else echo denied; fi\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	profile := "(version 1)\n(allow default)\n(deny file-write* (subpath \"" + scriptDir + "\"))\n"
	sb, argv, res := sandboxedSpec(t, profile, []string{script})

	spec := &SuperviseSpec{Argv: argv, Resolved: res.Resolved, Canonical: res.Canonical, Sandbox: sb, InnerExec: map[int]string{}}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "sandbox")
	delete(fields, "inner_exec")

	legacy, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}

	var old SuperviseSpec
	if err := json.Unmarshal(legacy, &old); err != nil {
		t.Fatal(err)
	}
	if old.Sandbox != nil {
		t.Fatal("legacy spec still carries a sandbox field")
	}

	if _, _, err := runSupervise(t, cap.Dir, &old); err != nil {
		t.Fatalf("superviseRun: %v", err)
	}

	out, _ := os.ReadFile(filepath.Join(cap.Dir, "stdout"))
	if string(out) != "ran-approved\ndenied\n" {
		t.Errorf("stdout = %q, want the approved script to run with the profile's write denial", out)
	}
}

func TestLayerEnvAndProxyEnv(t *testing.T) {
	proxy := ProxyEnv("http://localhost:3128")

	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		if proxy[name] != "http://localhost:3128" {
			t.Errorf("%s = %q, want the proxy URL", name, proxy[name])
		}
	}
	for _, name := range []string{"NO_PROXY", "no_proxy"} {
		if proxy[name] != "localhost,127.0.0.1,::1" {
			t.Errorf("%s = %q, want the loopback list", name, proxy[name])
		}
	}

	merged := LayerEnv(proxy, map[string]string{"HTTPS_PROXY": "http://other:1", "A": "b"})
	if merged["HTTPS_PROXY"] != "http://other:1" || merged["HTTP_PROXY"] != "http://localhost:3128" || merged["A"] != "b" {
		t.Errorf("merged = %v, want the caller's values to win over the proxy's", merged)
	}
	if proxy["HTTPS_PROXY"] != "http://localhost:3128" {
		t.Error("LayerEnv mutated its first argument")
	}

	if got := LayerEnv(nil, nil); got != nil {
		t.Errorf("LayerEnv of nothing = %v, want nil", got)
	}
}

// TestPoolMemberEnvIsLayeredPerCommand pins the member environment order: inherited, then the
// command's own overrides, then the pool's shared Env. One command's proxy never reaches another's run.
func TestPoolMemberEnvIsLayeredPerCommand(t *testing.T) {
	_, dir := newPoolTestDir(t)

	own := []string{"sh", "-c", "echo ${CG_OWN:-none} ${CG_SHARED:-none}"}
	other := []string{"sh", "-c", "echo ${CG_OWN:-none} ${CG_SHARED:-none}"}

	spec := poolSpec(t, own, other)
	spec.Commands[0].Env = map[string]string{"CG_OWN": "mine", "CG_SHARED": "overridden"}
	spec.Env = map[string]string{"CG_SHARED": "shared"}

	if _, err := runPool(t, dir, spec); err != nil {
		t.Fatalf("supervisePool: %v", err)
	}

	m := readManifest(t, dir)
	want := []string{"mine shared\n", "none shared\n"}
	for i, rec := range m.Runs {
		if got := runOutputAt(t, rec.RunID); got != want[i] {
			t.Errorf("run %d output = %q, want %q", i, got, want[i])
		}
	}
}

func runOutputAt(t *testing.T, id string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(CaptureRoot(), id, "stdout"))
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestPoolSandboxedMembersRecordTypedCommandsAndPreflightEachSpawn(t *testing.T) {
	requireSeatbelt(t)
	_, dir := newPoolTestDir(t)

	imported := filepath.Join(t.TempDir(), "extra.sb")
	if err := os.WriteFile(imported, []byte("(version 1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	profile := "(version 1)\n(allow default)\n(import \"" + imported + "\")\n"
	sb, argv, res := sandboxedSpec(t, profile, []string{"/bin/echo", "hi"})

	if err := os.WriteFile(imported, []byte("(this-is-not-a-form"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := &PoolSpec{Repeat: 2, Commands: []PoolCommand{{Argv: argv, Resolved: res.Resolved, Canonical: res.Canonical, Sandbox: sb}}}
	if _, err := runPool(t, dir, spec); err != nil {
		t.Fatalf("supervisePool: %v", err)
	}

	m := readManifest(t, dir)
	if !slices.Equal(m.Commands[0], []string{"/bin/echo", "hi"}) {
		t.Errorf("manifest commands = %v, want the typed command", m.Commands)
	}
	if len(m.Runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(m.Runs))
	}
	for i, rec := range m.Runs {
		if rec.Status != PoolRunStartError || !strings.Contains(rec.StartError, "no longer compiles") {
			t.Errorf("run %d = %+v, want its own preflight to refuse with the compile error", i, rec)
		}
	}
}
