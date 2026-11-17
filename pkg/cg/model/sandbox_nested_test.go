package model

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type preflightClassTest struct {
	Name  string
	Spec  SandboxSpec
	Probe SandboxProbe

	WantErr string
	WantRun bool
}

const nestedLine = "sandbox-exec: sandbox_apply: Operation not permitted"

var preflightClassTests = []preflightClassTest{
	{
		Name:    "a probe that agrees with the load-time class runs",
		Spec:    SandboxSpec{Class: SandboxApplies},
		Probe:   SandboxProbe{Class: SandboxApplies},
		WantRun: true,
	},
	{
		Name:    "a spec with no class is read as applies",
		Probe:   SandboxProbe{Class: SandboxApplies},
		WantRun: true,
	},
	{
		Name:    "a probe that now finds an outer sandbox refuses with its stderr",
		Spec:    SandboxSpec{Class: SandboxApplies},
		Probe:   SandboxProbe{Class: SandboxNested, ExitCode: 71, Stderr: nestedLine},
		WantErr: "classified as applies at load and nested now: " + nestedLine,
	},
	{
		Name:    "a probe that no longer finds the outer sandbox refuses too",
		Spec:    SandboxSpec{Class: SandboxNested},
		Probe:   SandboxProbe{Class: SandboxApplies},
		WantErr: "classified as nested at load and applies now",
	},
	{
		Name:    "an unknown outcome refuses with its stderr",
		Spec:    SandboxSpec{Class: SandboxApplies},
		Probe:   SandboxProbe{Class: SandboxUnknown, ExitCode: 3, Stderr: "sandbox-exec: odd"},
		WantErr: "classified as applies at load and unknown now: sandbox-exec: odd",
	},
	{
		Name:    "a compile failure still refuses with the compiler's message",
		Spec:    SandboxSpec{Class: SandboxApplies},
		Probe:   SandboxProbe{Class: SandboxUnknown, CompileFailed: true, ExitCode: 65, Stderr: "bad profile"},
		WantErr: "no longer compiles: bad profile",
	},
	{
		Name:    "a skipped profile preflights nothing",
		Spec:    SandboxSpec{Class: SandboxNested, Skipped: "cg is inside an outer sandbox"},
		Probe:   SandboxProbe{Class: SandboxUnknown, Stderr: "must not be consulted"},
		WantRun: true,
	},
}

func TestPreflightComparesItsClassWithTheLoadTimeOne(t *testing.T) {
	for _, test := range preflightClassTests {
		t.Run(test.Name, func(t *testing.T) {
			spec := test.Spec
			spec.Name = "go"
			spec.Resolved = "/bin/echo"
			if spec.Skipped != "" {
				spec.Resolved = ""
			}

			probed := false
			err := spec.preflight(func(string, []string) SandboxProbe {
				probed = true

				return test.Probe
			})

			if test.WantRun {
				if err != nil {
					t.Fatalf("preflight = %v, want it to pass", err)
				}
				if probed && spec.Skipped != "" {
					t.Errorf("a skipped profile ran the probe")
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), test.WantErr) {
				t.Errorf("preflight = %v, want an error containing %q", err, test.WantErr)
			}
		})
	}
}

func TestSuperviseSkippedSandboxRunsAsTypedAndRecordsTheSkip(t *testing.T) {
	cap := newSuperviseDir(t)

	reason := "cg is inside an outer sandbox, and already_sandboxed is run"
	typed := []string{"/bin/echo", "hi"}

	r, err := ResolveCommand(typed, "")
	if err != nil {
		t.Fatal(err)
	}

	spec := &SuperviseSpec{
		Argv: typed, Resolved: r.Resolved, Canonical: r.Canonical,
		Sandbox: &SandboxSpec{Name: "go", Command: typed, Class: SandboxNested, Skipped: reason},
	}
	if _, _, err := runSupervise(t, cap.Dir, spec); err != nil {
		t.Fatalf("superviseRun: %v", err)
	}

	meta, err := ReadMeta(cap.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(meta.Command, typed) || meta.Sandbox != "go" || meta.SandboxSkipped != reason {
		t.Errorf("record = command %q sandbox %q skipped %q, want the typed command, the name, and the reason", meta.Command, meta.Sandbox, meta.SandboxSkipped)
	}

	out, _ := os.ReadFile(filepath.Join(cap.Dir, "stdout"))
	if string(out) != "hi\n" {
		t.Errorf("stdout = %q, want the command run directly", out)
	}
}

func TestSuperviseRefusesWhenTheClassChangedSinceLoad(t *testing.T) {
	requireSeatbelt(t)
	cap := newSuperviseDir(t)

	sb, argv, res := sandboxedSpec(t, permissiveProfile, []string{"/bin/echo", "hi"})
	sb.Class = SandboxNested

	spec := &SuperviseSpec{Argv: argv, Resolved: res.Resolved, Canonical: res.Canonical, Sandbox: sb}
	ack, _, err := runSupervise(t, cap.Dir, spec)
	if err == nil || ack.Started {
		t.Fatalf("ack = %+v, err = %v, want the run refused before it starts", ack, err)
	}
	if !strings.Contains(ack.StartError, "classified as nested at load and applies now") {
		t.Errorf("start error = %q, want the disagreement named", ack.StartError)
	}
}
