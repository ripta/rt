package approve

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/model"
)

const sandboxProfileText = "(version 1)\n(allow default)\n"

// sandboxFixture is a project root, a home directory, and a global rules file path, with a recording
// probe standing in for sandbox-exec so the load logic runs on any platform and outside a sandbox.
type sandboxFixture struct {
	t      *testing.T
	Root   string
	Home   string
	Global string

	Platform string
	Class    model.SandboxClass
	Stderr   string
	Compile  func(profile string) bool

	Probes []probeCall
}

type probeCall struct {
	Profile string
	Defines []string
}

func newSandboxFixture(t *testing.T) *sandboxFixture {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return &sandboxFixture{t: t, Root: root, Home: home, Global: filepath.Join(t.TempDir(), "approve.yaml"), Platform: "darwin", Class: model.SandboxApplies}
}

func (f *sandboxFixture) write(path, content string) string {
	f.t.Helper()

	if !filepath.IsAbs(path) {
		path = filepath.Join(f.Root, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}

	return path
}

func (f *sandboxFixture) options() LoadOptions {
	return LoadOptions{
		GlobalPath:  f.Global,
		ProjectRoot: f.Root,
		Home:        func() (string, error) { return f.Home, nil },
		Platform:    f.Platform,
		Probe: func(profile string, defines []string) model.SandboxProbe {
			f.Probes = append(f.Probes, probeCall{Profile: profile, Defines: slices.Clone(defines)})
			if f.Compile != nil && f.Compile(profile) {
				return model.SandboxProbe{CompileFailed: true, Class: model.SandboxUnknown, ExitCode: 65, Stderr: "profile: syntax error"}
			}

			return model.SandboxProbe{Class: f.Class, Stderr: f.Stderr}
		},
	}
}

// load writes the global and project files, when given, and loads them.
func (f *sandboxFixture) load(global, project string) (*Store, error) {
	f.t.Helper()

	if global != "" {
		f.write(f.Global, global)
	}
	if project != "" {
		f.write(".cg.yaml", project)
	}

	return Load(f.options())
}

func (f *sandboxFixture) mustLoad(global, project string) *Store {
	f.t.Helper()

	s, err := f.load(global, project)
	if err != nil {
		f.t.Fatalf("Load: %v", err)
	}

	return s
}

const goSandboxYAML = `version: 2
sandboxes:
  go:
    seatbelt:
      profile: go.sb
      already_sandboxed: refuse
allow:
  - prefix: [go, test]
    sandbox: go
`

type sandboxSchemaTest struct {
	Name    string
	YAML    string
	WantErr string
}

var sandboxSchemaTests = []sandboxSchemaTest{
	{Name: "a version 2 file with sandboxes loads", YAML: goSandboxYAML},
	{
		Name:    "a version 1 file may not use sandboxes",
		YAML:    "version: 1\nsandboxes:\n  go:\n    seatbelt: {profile: go.sb, already_sandboxed: run}\n",
		WantErr: "version: 2",
	},
	{
		Name:    "a version 1 file may not use sandbox on a rule",
		YAML:    "version: 1\nallow:\n  - prefix: [go]\n    sandbox: go\n",
		WantErr: "version: 2",
	},
	{
		Name: "a version 1 file keeps its leniency about unknown keys",
		YAML: "version: 1\nfuture: true\nallow:\n  - prefix: [go]\n    sanbox: go\n",
	},
	{
		Name:    "a version 2 file rejects an unknown top-level key with its line",
		YAML:    "version: 2\nallow: []\nsanboxes: {}\n",
		WantErr: `line 3: unknown key "sanboxes" in the rules file`,
	},
	{
		Name:    "a version 2 file rejects a typo in a rule with its line",
		YAML:    "version: 2\nallow:\n  - prefix: [go]\n    sanbox: go\n",
		WantErr: `line 4: unknown key "sanbox" in a allow rule`,
	},
	{
		Name:    "a version 2 file rejects an unknown key in a sandbox",
		YAML:    "version: 2\nsandboxes:\n  go:\n    bubblewrap: {}\n",
		WantErr: `line 4: unknown key "bubblewrap" in sandbox "go"`,
	},
	{
		Name:    "a version 2 file rejects an unknown key in the seatbelt block",
		YAML:    "version: 2\nsandboxes:\n  go:\n    seatbelt:\n      profile: go.sb\n      already_sandboxed: run\n      proxie: x\n",
		WantErr: `line 7: unknown key "proxie" in the seatbelt block of sandbox "go"`,
	},
	{
		Name: "sandbox names and params keys are data",
		YAML: "version: 2\nsandboxes:\n  message:\n    seatbelt:\n      profile: go.sb\n      already_sandboxed: run\n      params:\n        profile: \"1\"\n        LOOPBACK: \"1\"\n",
	},
	{
		Name:    "sandbox on a deny rule fails",
		YAML:    "version: 2\ndeny:\n  - prefix: [rm]\n    sandbox: go\n",
		WantErr: "sandbox is valid only on an exact, prefix, glob, or regex allow rule",
	},
	{
		Name:    "sandbox on a restrict rule fails",
		YAML:    "version: 2\nrestrict:\n  - prefix: [rm]\n    sandbox: go\n",
		WantErr: "sandbox is valid only on an exact, prefix, glob, or regex allow rule",
	},
	{
		Name:    "sandbox on a wrapper rule fails",
		YAML:    "version: 2\nallow:\n  - wrapper: [nice]\n    sandbox: go\n",
		WantErr: "sandbox is valid only on an exact, prefix, glob, or regex allow rule",
	},
	{
		Name:    "an empty sandbox name fails",
		YAML:    "version: 2\nallow:\n  - prefix: [go]\n    sandbox: \"\"\n",
		WantErr: "sandbox must name a sandbox",
	},
	{
		Name:    "a rule naming a sandbox cannot permit a DYLD variable",
		YAML:    "version: 2\nallow:\n  - prefix: [go]\n    sandbox: go\n    permit_unsafe_envs: [DYLD_LIBRARY_PATH]\n",
		WantErr: "cannot permit a DYLD_ variable",
	},
	{
		Name:    "a seatbelt block with no already_sandboxed fails",
		YAML:    "version: 2\nsandboxes:\n  go:\n    seatbelt:\n      profile: go.sb\n",
		WantErr: "seatbelt needs already_sandboxed",
	},
	{
		Name:    "an unknown already_sandboxed value fails",
		YAML:    "version: 2\nsandboxes:\n  go:\n    seatbelt:\n      profile: go.sb\n      already_sandboxed: maybe\n",
		WantErr: `already_sandboxed "maybe" is not one of run, refuse, or ask`,
	},
	{
		Name:    "a seatbelt block with no profile fails",
		YAML:    "version: 2\nsandboxes:\n  go:\n    seatbelt:\n      already_sandboxed: run\n",
		WantErr: "seatbelt needs a profile path",
	},
	{
		Name:    "a params boolean fails",
		YAML:    "version: 2\nsandboxes:\n  go:\n    seatbelt:\n      profile: go.sb\n      already_sandboxed: run\n      params:\n        LOOPBACK: true\n",
		WantErr: "must be a YAML string",
	},
	{
		Name:    "a params number fails",
		YAML:    "version: 2\nsandboxes:\n  go:\n    seatbelt:\n      profile: go.sb\n      already_sandboxed: run\n      params:\n        LOOPBACK: 0\n",
		WantErr: "must be a YAML string",
	},
	{
		Name:    "a params null fails",
		YAML:    "version: 2\nsandboxes:\n  go:\n    seatbelt:\n      profile: go.sb\n      already_sandboxed: run\n      params:\n        LOOPBACK:\n",
		WantErr: "must be a YAML string",
	},
	{
		Name:    "a reserved params key fails",
		YAML:    "version: 2\nsandboxes:\n  go:\n    seatbelt:\n      profile: go.sb\n      already_sandboxed: run\n      params:\n        HOME: x\n",
		WantErr: `params key "HOME" is set by cg`,
	},
	{
		Name:    "a params key with an equals sign fails",
		YAML:    "version: 2\nsandboxes:\n  go:\n    seatbelt:\n      profile: go.sb\n      already_sandboxed: run\n      params:\n        \"A=B\": x\n",
		WantErr: "without '='",
	},
	{
		Name:    "a proxy that is not a URL fails",
		YAML:    "version: 2\nsandboxes:\n  go:\n    seatbelt:\n      profile: go.sb\n      already_sandboxed: run\n      proxy: localhost\n",
		WantErr: "is not a URL",
	},
}

func TestSandboxSchema(t *testing.T) {
	t.Parallel()

	for _, test := range sandboxSchemaTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()

			_, _, err := ParseDocument([]byte(test.YAML))
			switch {
			case test.WantErr == "" && err != nil:
				t.Errorf("ParseDocument: %v", err)
			case test.WantErr != "" && (err == nil || !strings.Contains(err.Error(), test.WantErr)):
				t.Errorf("ParseDocument error = %v, want it to contain %q", err, test.WantErr)
			}
		})
	}
}

func TestSandboxLoadFreezesProfileOnce(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	profile := f.write("go.sb", sandboxProfileText)

	s := f.mustLoad("", goSandboxYAML)

	sb, ok := s.Ruleset().Sandbox("go")
	if !ok || sb.Seatbelt == nil {
		t.Fatalf("Sandbox(go) = %+v, %v, want a frozen seatbelt block", sb, ok)
	}
	if sb.Seatbelt.Text != sandboxProfileText || sb.Seatbelt.Profile != profile || sb.Seatbelt.AlreadySandboxed != AlreadyRefuse {
		t.Errorf("seatbelt = %+v, want the file's text, path, and already_sandboxed", sb.Seatbelt)
	}

	if err := os.WriteFile(profile, []byte("(version 1)\n(deny default)\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	again, _ := s.Ruleset().Sandbox("go")
	if again.Seatbelt.Text != sandboxProfileText {
		t.Errorf("text after the file changed = %q, want the text frozen at load", again.Seatbelt.Text)
	}
	if len(f.Probes) != 1 {
		t.Errorf("probes = %d, want one probe at load and none after", len(f.Probes))
	}
}

func TestSandboxLoadDefinesResolveSymlinks(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	f.write("go.sb", sandboxProfileText)

	realRoot := f.Root
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	f.Root = link

	f.write("go.sb", sandboxProfileText)

	project := strings.Replace(goSandboxYAML, "already_sandboxed: refuse", "already_sandboxed: refuse\n      params:\n        LOOPBACK: \"1\"\n        AAA: \"x\"", 1)
	s := f.mustLoad("", project)

	sb, _ := s.Ruleset().Sandbox("go")
	want := []string{"HOME=" + f.Home, "PROJECT_PATH=" + realRoot, "AAA=x", "LOOPBACK=1"}
	if !slices.Equal(sb.Seatbelt.Defines, want) {
		t.Errorf("defines = %q, want %q", sb.Seatbelt.Defines, want)
	}
	if len(f.Probes) != 1 || !slices.Equal(f.Probes[0].Defines, want) {
		t.Errorf("probe defines = %v, want the same parameters the runs use", f.Probes)
	}
}

func TestSandboxLoadUnknownNameFails(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)

	_, err := f.load("", "version: 2\nallow:\n  - prefix: [go]\n    sandbox: nope\n")
	if err == nil || !strings.Contains(err.Error(), `names sandbox "nope", which neither the project nor the global rules file defines`) {
		t.Errorf("Load error = %v, want the unknown sandbox named", err)
	}
}

func TestSandboxLoadRuleMayNameGlobalSandbox(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	profile := f.write(filepath.Join(t.TempDir(), "go.sb"), sandboxProfileText)

	global := "version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: " + profile + ", already_sandboxed: run}\n"
	s := f.mustLoad(global, "version: 2\nallow:\n  - prefix: [go]\n    sandbox: go\n")

	sb, ok := s.Ruleset().Sandbox("go")
	if !ok || sb.File != f.Global {
		t.Errorf("sandbox = %+v, %v, want the global definition", sb, ok)
	}
}

func TestSandboxLoadProjectOverridesGlobalAndOnlyWinnerIsChecked(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	f.write("go.sb", sandboxProfileText)

	missing := filepath.Join(t.TempDir(), "gone.sb")
	global := "version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: " + missing + ", already_sandboxed: run}\n"

	s := f.mustLoad(global, goSandboxYAML)

	sb, _ := s.Ruleset().Sandbox("go")
	if sb.File != filepath.Join(f.Root, ".cg.yaml") || sb.Seatbelt.AlreadySandboxed != AlreadyRefuse {
		t.Errorf("sandbox = %+v, want the project's definition", sb)
	}
	if len(f.Probes) != 1 {
		t.Errorf("probes = %d, want only the winning definition probed", len(f.Probes))
	}
}

func TestSandboxLoadMissingProfileFailsNamingTheFile(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)

	_, err := f.load("", goSandboxYAML)
	if err == nil || !strings.Contains(err.Error(), ".cg.yaml") || !strings.Contains(err.Error(), "reading profile") {
		t.Errorf("Load error = %v, want a missing profile to name the project file", err)
	}
}

func TestSandboxLoadGlobalFailuresNameTheGlobalFile(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	profile := f.write(filepath.Join(t.TempDir(), "bad.sb"), "(broken")
	f.Compile = func(text string) bool { return strings.HasPrefix(text, "(broken") }

	global := "version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: " + profile + ", already_sandboxed: run}\n"

	_, err := f.load(global, "")
	if err == nil || !strings.Contains(err.Error(), f.Global) || !strings.Contains(err.Error(), "does not compile: profile: syntax error") {
		t.Errorf("Load error = %v, want the global file and the compiler's message", err)
	}
}

func TestSandboxLoadStoresProbeClassification(t *testing.T) {
	t.Parallel()

	for _, class := range []model.SandboxClass{model.SandboxApplies, model.SandboxNested, model.SandboxUnknown} {
		f := newSandboxFixture(t)
		f.write("go.sb", sandboxProfileText)
		f.Class = class
		f.Stderr = "probe said " + string(class)

		s := f.mustLoad("", goSandboxYAML)

		sb, _ := s.Ruleset().Sandbox("go")
		if sb.Seatbelt.Class != class || sb.Seatbelt.ProbeStderr != "probe said "+string(class) {
			t.Errorf("classification = %s %q, want %s with the probe's stderr", sb.Seatbelt.Class, sb.Seatbelt.ProbeStderr, class)
		}
	}
}

func TestSandboxLoadOtherPlatformNeitherReadsNorProbes(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	f.Platform = "linux"

	s := f.mustLoad("", goSandboxYAML)

	sb, ok := s.Ruleset().Sandbox("go")
	if !ok || sb.Seatbelt != nil || !slices.Equal(sb.Declared, []string{"seatbelt"}) {
		t.Errorf("sandbox = %+v, %v, want a definition with no applicable block", sb, ok)
	}
	if len(f.Probes) != 0 {
		t.Errorf("probes = %d, want none: the profile does not exist here and must not be read", len(f.Probes))
	}
}

func TestSandboxLoadRelativeProfile(t *testing.T) {
	t.Parallel()

	t.Run("project resolves against the project root", func(t *testing.T) {
		t.Parallel()

		f := newSandboxFixture(t)
		f.write("profiles/go.sb", sandboxProfileText)

		s := f.mustLoad("", strings.Replace(goSandboxYAML, "go.sb", "profiles/go.sb", 1))
		sb, _ := s.Ruleset().Sandbox("go")
		if sb.Seatbelt.Profile != filepath.Join(f.Root, "profiles", "go.sb") {
			t.Errorf("profile = %q, want it under the project root", sb.Seatbelt.Profile)
		}
	})

	t.Run("global fails the load", func(t *testing.T) {
		t.Parallel()

		f := newSandboxFixture(t)

		_, err := f.load("version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: go.sb, already_sandboxed: run}\n", "")
		if err == nil || !strings.Contains(err.Error(), "relative profile path") || !strings.Contains(err.Error(), f.Global) {
			t.Errorf("Load error = %v, want the global file named with advice to use ~/ or an absolute path", err)
		}
	})

	t.Run("lint of the global file alone reports it", func(t *testing.T) {
		t.Parallel()

		f := newSandboxFixture(t)
		f.write(f.Global, "version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: go.sb, already_sandboxed: run}\n")

		global, project, err := Diagnose(f.options())
		if err != nil {
			t.Fatal(err)
		}
		if global.Err == nil || !strings.Contains(global.Err.Error(), "relative profile path") || project.Err != nil {
			t.Errorf("diagnoses = global %v, project %v, want only the global layer to report it", global.Err, project.Err)
		}
	})

	t.Run("tilde expands against the home the load read", func(t *testing.T) {
		t.Parallel()

		f := newSandboxFixture(t)
		f.write(filepath.Join(f.Home, "sb", "go.sb"), sandboxProfileText)

		s := f.mustLoad("version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: ~/sb/go.sb, already_sandboxed: run}\n", "")
		sb, _ := s.Ruleset().Sandbox("go")
		if sb.Seatbelt.Profile != filepath.Join(f.Home, "sb", "go.sb") {
			t.Errorf("profile = %q, want ~ expanded", sb.Seatbelt.Profile)
		}
	})
}

func TestSandboxLoadArgMax(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	f.write("go.sb", "(version 1)\n;"+strings.Repeat("x", 800<<10)+"\n")

	_, err := f.load("", goSandboxYAML)
	if err == nil || !strings.Contains(err.Error(), "bytes of argument space") {
		t.Errorf("Load error = %v, want the argument limit named", err)
	}
	if len(f.Probes) != 0 {
		t.Errorf("probes = %d, want the size check to refuse before any probe", len(f.Probes))
	}
}

func TestMergedIssuesReportsWhatOnlyTheMergeFinds(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	f.write(".cg.yaml", goSandboxYAML)

	global, project, err := Diagnose(f.options())
	if err != nil {
		t.Fatal(err)
	}

	if issues := MergedIssues(global, project); issues == nil || !strings.Contains(issues.Error(), "reading profile") {
		t.Errorf("MergedIssues = %v, want the missing profile the server load would report", issues)
	}

	f.write("go.sb", sandboxProfileText)

	global, project, err = Diagnose(f.options())
	if err != nil {
		t.Fatal(err)
	}
	if issues := MergedIssues(global, project); issues != nil {
		t.Errorf("MergedIssues = %v, want none for a healthy profile", issues)
	}
}

// requireSeatbelt skips a test that applies a real profile when sandbox-exec cannot apply one here.
func requireSeatbelt(t *testing.T) {
	t.Helper()

	if runtime.GOOS != "darwin" {
		t.Skipf("seatbelt applies on darwin only, not %s", runtime.GOOS)
	}

	if probe := model.ProbeSeatbelt(sandboxProfileText, nil); probe.Class != model.SandboxApplies {
		t.Skipf("sandbox-exec cannot apply a profile here: class %s, stderr %q", probe.Class, probe.Stderr)
	}
}

func TestSandboxLoadRealProbe(t *testing.T) {
	requireSeatbelt(t)

	t.Run("a profile that does not compile fails the load with the compiler's message", func(t *testing.T) {
		f := newSandboxFixture(t)
		f.write("go.sb", "(version 1)\n(not-a-form")

		opts := f.options()
		opts.Probe = nil

		f.write(".cg.yaml", goSandboxYAML)

		_, err := Load(opts)
		if err == nil || !strings.Contains(err.Error(), "does not compile") {
			t.Errorf("Load error = %v, want a compile failure", err)
		}
	})

	t.Run("a valid profile that denies running true passes", func(t *testing.T) {
		f := newSandboxFixture(t)
		f.write("go.sb", "(version 1)\n(allow default)\n(deny process-exec (literal \"/usr/bin/true\"))\n")
		f.write(".cg.yaml", goSandboxYAML)

		opts := f.options()
		opts.Probe = nil

		s, err := Load(opts)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}

		sb, _ := s.Ruleset().Sandbox("go")
		if sb.Seatbelt.Class != model.SandboxApplies {
			t.Errorf("class = %s, want applies", sb.Seatbelt.Class)
		}
	})

	t.Run("a broken parameter-guarded form fails because the check passes the parameters", func(t *testing.T) {
		f := newSandboxFixture(t)
		f.write("go.sb", "(version 1)\n(allow default)\n(if (param \"LOOPBACK\") (not-a-form))\n")
		f.write(".cg.yaml", strings.Replace(goSandboxYAML, "already_sandboxed: refuse", "already_sandboxed: refuse\n      params:\n        LOOPBACK: \"1\"", 1))

		opts := f.options()
		opts.Probe = nil

		if _, err := Load(opts); err == nil || !strings.Contains(err.Error(), "does not compile") {
			t.Errorf("Load error = %v, want the guarded form checked", err)
		}
	})
}

type sandboxEvalTest struct {
	Name        string
	Rules       string
	Argv        []string
	Mode        Mode
	Env         map[string]string
	WantRun     bool
	WantSandbox string
	WantStep    Step
	WantTied    []string
}

const evalSandboxes = `version: 2
sandboxes:
  go:
    seatbelt: {profile: go.sb, already_sandboxed: run}
  go-loopback:
    seatbelt: {profile: go.sb, already_sandboxed: run, params: {LOOPBACK: "1"}}
`

var sandboxEvalTests = []sandboxEvalTest{
	{
		Name:        "the matching rule supplies its sandbox",
		Rules:       "allow:\n  - prefix: [go, test]\n    sandbox: go\n",
		Argv:        []string{"go", "test", "./..."},
		WantRun:     true,
		WantSandbox: "go",
		WantStep:    StepAllow,
	},
	{
		Name:     "a rule with no sandbox runs unsandboxed",
		Rules:    "allow:\n  - prefix: [go, test]\n",
		Argv:     []string{"go", "test"},
		WantRun:  true,
		WantStep: StepAllow,
	},
	{
		Name:        "the narrower rule decides over the broader rule's sandbox",
		Rules:       "allow:\n  - prefix: [go, test]\n    sandbox: go\n  - prefix: [go, test, -run, Loop]\n    sandbox: go-loopback\n",
		Argv:        []string{"go", "test", "-run", "Loop"},
		WantRun:     true,
		WantSandbox: "go-loopback",
		WantStep:    StepAllow,
	},
	{
		Name:        "the broader rule decides what the narrower does not match",
		Rules:       "allow:\n  - prefix: [go, test]\n    sandbox: go\n  - prefix: [go, test, -run, Loop]\n    sandbox: go-loopback\n",
		Argv:        []string{"go", "test", "./..."},
		WantRun:     true,
		WantSandbox: "go",
		WantStep:    StepAllow,
	},
	{
		Name:        "the list order does not pick the sandbox",
		Rules:       "allow:\n  - prefix: [go, test, -run, Loop]\n    sandbox: go-loopback\n  - prefix: [go, test]\n    sandbox: go\n",
		Argv:        []string{"go", "test", "-run", "Loop"},
		WantRun:     true,
		WantSandbox: "go-loopback",
		WantStep:    StepAllow,
	},
	{
		Name:     "tied rules that differ in sandbox refuse and name each of them",
		Rules:    "allow:\n  - prefix: [go, test]\n    sandbox: go\n  - prefix: [go, test]\n    sandbox: go-loopback\n",
		Argv:     []string{"go", "test"},
		WantStep: StepTieRefusal,
		WantTied: []string{"go", "go-loopback"},
	},
	{
		Name:        "tied rules that agree on the sandbox run",
		Rules:       "allow:\n  - prefix: [go, test]\n    sandbox: go\n  - prefix: [go, test]\n    sandbox: go\n    permit_unsafe_envs: [FOO]\n",
		Argv:        []string{"go", "test"},
		WantRun:     true,
		WantSandbox: "go",
		WantStep:    StepAllow,
	},
	{
		Name:        "a wrapped command runs under its inner rule's sandbox",
		Rules:       "allow:\n  - wrapper: [nice, -n, \"19\"]\n  - prefix: [go, test]\n    sandbox: go\n",
		Argv:        []string{"nice", "-n", "19", "go", "test"},
		WantRun:     true,
		WantSandbox: "go",
		WantStep:    StepAllow,
	},
	{
		Name:     "a caller's dangerous env never moves the command to a broader rule's sandbox",
		Rules:    "allow:\n  - prefix: [go, test]\n    sandbox: go\n  - prefix: [go]\n    sandbox: go-loopback\n    permit_unsafe_envs: [PATH]\n",
		Argv:     []string{"go", "test"},
		Env:      map[string]string{"PATH": "/x"},
		WantStep: StepEnvRefusal,
	},
	{
		Name:        "allow-all still finds the deciding rule's sandbox",
		Mode:        ModeAllowAll,
		Rules:       "allow:\n  - prefix: [go, test]\n    sandbox: go\n",
		Argv:        []string{"go", "test"},
		WantRun:     true,
		WantSandbox: "go",
		WantStep:    StepAllow,
	},
	{
		Name:        "allow-all ignores a dangerous env and still sandboxes a wrapped command",
		Mode:        ModeAllowAll,
		Rules:       "allow:\n  - wrapper: [nice, -n, \"19\"]\n  - prefix: [go, test]\n    sandbox: go\n",
		Argv:        []string{"nice", "-n", "19", "go", "test"},
		Env:         map[string]string{"PATH": "/x"},
		WantRun:     true,
		WantSandbox: "go",
		WantStep:    StepAllow,
	},
	{
		Name:    "allow-all runs a command no rule matches with no sandbox",
		Mode:    ModeAllowAll,
		Rules:   "allow:\n  - prefix: [go, test]\n    sandbox: go\n",
		Argv:    []string{"sh", "-c", "go test ./..."},
		WantRun: true,
	},
	{
		Name:     "allow-all refuses a tie that differs in sandbox",
		Mode:     ModeAllowAll,
		Rules:    "allow:\n  - prefix: [go, test]\n    sandbox: go\n  - prefix: [go, test]\n    sandbox: go-loopback\n",
		Argv:     []string{"go", "test"},
		WantStep: StepTieRefusal,
		WantTied: []string{"go", "go-loopback"},
	},
}

func TestSandboxEvaluate(t *testing.T) {
	t.Parallel()

	for _, test := range sandboxEvalTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()

			f := newSandboxFixture(t)
			f.write("go.sb", sandboxProfileText)

			header := evalSandboxes
			if test.Mode != "" {
				header += "mode: " + string(test.Mode) + "\n"
			}

			s := f.mustLoad("", header+test.Rules)

			v := s.Ruleset().Evaluate(identitySubject(test.Argv), test.Env, nil)

			if test.WantRun != (v.Decision == DecisionRun) {
				t.Fatalf("decision = %v, step %v, want run=%v", v.Decision, v.Step, test.WantRun)
			}
			if got := v.Sandbox(); got != test.WantSandbox {
				t.Errorf("sandbox = %q, want %q", got, test.WantSandbox)
			}
			if !test.WantRun && refusalStepOf(v) != test.WantStep {
				t.Errorf("step = %v, want %v", refusalStepOf(v), test.WantStep)
			}

			if test.WantTied != nil {
				var got []string
				for _, rule := range v.Deciding {
					got = append(got, rule.Sandbox)
				}
				slices.Sort(got)

				if !slices.Equal(got, test.WantTied) {
					t.Errorf("deciding = %q, want every tied rule %q", got, test.WantTied)
				}
			}
		})
	}
}

func refusalStepOf(v Verdict) Step {
	if len(v.Chain) > 0 {
		return v.Chain[len(v.Chain)-1].Step
	}

	return v.Step
}

// TestSandboxTieOfThreeNamesEveryRule pins that a refusal from a tie of three rules, two agreeing and
// one differing, names all three and not only the odd one.
func TestSandboxTieOfThreeNamesEveryRule(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	f.write("go.sb", sandboxProfileText)

	rules := "allow:\n  - prefix: [go, test]\n    sandbox: go\n  - prefix: [go, test]\n    sandbox: go\n    permit_unsafe_envs: [FOO]\n  - prefix: [go, test]\n    sandbox: go-loopback\n"
	s := f.mustLoad("", evalSandboxes+rules)

	v := s.Ruleset().Evaluate(identitySubject([]string{"go", "test"}), nil, nil)
	if v.Decision != DecisionRefuse || v.Step != StepTieRefusal || len(v.Deciding) != 3 {
		t.Errorf("verdict = %v step %v deciding %d, want a tie refusal naming all three rules", v.Decision, v.Step, len(v.Deciding))
	}
}

// TestSandboxChainTiedWithOrdinaryAllow pins that a chain's fields are those of its inner deciding
// rule. The chain peels to a rule naming one sandbox, and an ordinary allow of equal rank names another.
func TestSandboxChainTiedWithOrdinaryAllow(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	f.write("go.sb", sandboxProfileText)

	rules := "allow:\n  - wrapper: [/usr/bin/nice]\n  - prefix: [/usr/bin/make]\n    sandbox: go\n  - exact: [nice, make]\n    sandbox: go-loopback\n"
	s := f.mustLoad("", evalSandboxes+rules)

	resolve := func(argv []string) *model.Resolution {
		return &model.Resolution{Argv: argv, Resolved: "/usr/bin/make", Canonical: "/usr/bin/make"}
	}

	subject := Subject{Argv: []string{"nice", "make"}, Canonical: []string{"/usr/bin/nice", "make"}}
	v := s.Ruleset().Evaluate(subject, nil, resolve)
	if v.Decision != DecisionRefuse || !v.tieRefused() {
		t.Fatalf("verdict = %v step %v, want a tie refusal: the chain's sandbox is its inner rule's", v.Decision, v.Step)
	}

	tied := 0
	for _, rule := range v.Deciding {
		if rule.Sandbox != "" {
			tied++
		}
	}
	if tied != 2 {
		t.Errorf("deciding set holds %d sandbox rules, want the ordinary allow and the chain's inner rule", tied)
	}
}

func TestSandboxRememberKeepsDefinitionsAndClassification(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	f.write("go.sb", sandboxProfileText)
	f.Class = model.SandboxNested
	f.Stderr = "probe stderr"

	s := f.mustLoad("", goSandboxYAML)

	if err := s.AppendProjectAllowPrefix([]string{"make", "build"}, WriteDirect); err != nil {
		t.Fatalf("AppendProjectAllowPrefix: %v", err)
	}

	sb, ok := s.Ruleset().Sandbox("go")
	if !ok || sb.Seatbelt == nil || sb.Seatbelt.Class != model.SandboxNested || sb.Seatbelt.Text != sandboxProfileText {
		t.Errorf("sandbox after remember = %+v, %v, want the definition and its load-time class", sb, ok)
	}
	if len(f.Probes) != 1 {
		t.Errorf("probes = %d, want remember never to re-read or re-probe a profile", len(f.Probes))
	}

	v := s.Ruleset().Evaluate(identitySubject([]string{"go", "test"}), nil, nil)
	if v.Sandbox() != "go" {
		t.Errorf("sandbox of the rule that was there before = %q, want go", v.Sandbox())
	}

	v = s.Ruleset().Evaluate(identitySubject([]string{"make", "build"}), nil, nil)
	if v.Decision != DecisionRun || v.Sandbox() != "" {
		t.Errorf("remembered rule: decision %v sandbox %q, want run with no sandbox", v.Decision, v.Sandbox())
	}

	written, err := os.ReadFile(filepath.Join(f.Root, ".cg.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "sandbox: go") || !strings.Contains(string(written), "version: 2") {
		t.Errorf("project file after remember = %q, want the version and the sandbox rule kept", written)
	}
}

func TestSandboxReloadMergeWritesSandboxBack(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)
	f.write("go.sb", sandboxProfileText)

	s := f.mustLoad("", goSandboxYAML)

	f.write(".cg.yaml", strings.Replace(goSandboxYAML, "    sandbox: go\n", "    permit_unsafe_envs: []\n", 1))

	if err := s.AppendProjectAllowPrefix([]string{"make", "build"}, WriteReloadMerge); err != nil {
		t.Fatalf("AppendProjectAllowPrefix: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(f.Root, ".cg.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	_, parsed, err := ParseDocument(written)
	if err != nil {
		t.Fatalf("written file does not load: %v\n%s", err, written)
	}

	var sandboxed, plain bool
	for _, rule := range parsed.Allow {
		if slices.Equal(rule.Prefix, []string{"go", "test"}) {
			sandboxed = sandboxed || rule.Sandbox == "go"
			plain = plain || rule.Sandbox == ""
		}
	}
	if !sandboxed || !plain {
		t.Errorf("rules after reload-merge = %+v, want the in-memory rule written back with its sandbox beside the disk edit", parsed.Allow)
	}
}

func TestSandboxIdentityKeepsBothRules(t *testing.T) {
	t.Parallel()

	sandboxed := &Rule{Prefix: []string{"go", "test"}, kind: KindPrefix, Sandbox: "go"}
	plain := &Rule{Prefix: []string{"go", "test"}, kind: KindPrefix}

	if ruleEqual(sandboxed, plain) {
		t.Error("a rule that names a sandbox equals one that does not")
	}

	root := rootMapping(newEmptyProjectDoc())
	seq := ensureSeq(root, "allow")
	seq.Content = append(seq.Content, buildRuleEntry(plain, false), buildRuleEntry(sandboxed, false))
	canonicalizeRuleSeqs(root)

	if len(seq.Content) != 2 {
		t.Fatalf("canonicalization left %d rules, want both: dropping the sandbox rule would unsandbox the command", len(seq.Content))
	}

	var kept int
	for _, entry := range seq.Content {
		var r Rule
		if err := entry.Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.Sandbox == "go" {
			kept++
		}
	}
	if kept != 1 {
		t.Errorf("sandbox rules kept = %d, want 1", kept)
	}
}

func TestSandboxErrorsAreNotLostByJoin(t *testing.T) {
	t.Parallel()

	f := newSandboxFixture(t)

	_, err := f.load("", "version: 2\nallow:\n  - prefix: [a]\n    sandbox: x\n  - prefix: [b]\n    sandbox: y\n")
	if err == nil || !strings.Contains(err.Error(), `"x"`) || !strings.Contains(err.Error(), `"y"`) {
		t.Errorf("Load error = %v, want every unknown sandbox reported at once", err)
	}

	if errors.Is(err, ErrUnknownVersion) {
		t.Error("error wraps the version error")
	}
}

type rememberRefusalTest struct {
	Name string
	Disk string
	Want string
}

var rememberRefusalTests = []rememberRefusalTest{
	{
		Name: "a file edited back to version 1",
		Disk: "version: 1\nallow:\n  - prefix: [make]\n",
		Want: "version: 2",
	},
	{
		Name: "a file edited to drop sandboxes",
		Disk: "version: 2\nallow:\n  - prefix: [make]\n",
		Want: `names sandbox "go"`,
	},
}

// TestSandboxReloadMergeRefusesWhatWouldStopLoading pins that remember never writes a project file
// that stops loading. The in-memory rule names a sandbox the edited file no longer supports. A silent
// drop would unsandbox the command, so the remember is refused and the file stays as the user left it.
func TestSandboxReloadMergeRefusesWhatWouldStopLoading(t *testing.T) {
	t.Parallel()

	for _, test := range rememberRefusalTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()

			f := newSandboxFixture(t)
			f.write("go.sb", sandboxProfileText)

			s := f.mustLoad("", goSandboxYAML)
			path := f.write(".cg.yaml", test.Disk)

			err := s.AppendProjectAllowPrefix([]string{"ls"}, WriteReloadMerge)
			if err == nil || !strings.Contains(err.Error(), "changed on disk") || !strings.Contains(err.Error(), test.Want) {
				t.Fatalf("AppendProjectAllowPrefix error = %v, want a refusal naming the disk change and %q", err, test.Want)
			}

			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != test.Disk {
				t.Errorf("project file = %q, want it untouched at %q", got, test.Disk)
			}

			if v := s.Ruleset().Evaluate(identitySubject([]string{"ls"}), nil, nil); v.Decision == DecisionRun {
				t.Error("the live ruleset allows a rule that was refused")
			}
		})
	}
}
