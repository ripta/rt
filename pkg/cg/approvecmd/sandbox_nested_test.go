package approvecmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
)

// loadWithProbe loads a project whose only rule names sandbox go, with a fake load probe. The probe
// is how a nested shell is simulated on any host.
func loadWithProbe(t *testing.T, class model.SandboxClass, stderr string) (*approve.Ruleset, approve.Verdict) {
	t.Helper()

	root, home := t.TempDir(), t.TempDir()
	writeProfile(t, root, "(version 1)\n")

	rules := "version: 2\nsandboxes:\n  go:\n    seatbelt: {profile: go.sb, already_sandboxed: ask}\nallow:\n  - prefix: [echo]\n    sandbox: go\n"
	if err := os.WriteFile(filepath.Join(root, ".cg.yaml"), []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := approve.Load(approve.LoadOptions{
		GlobalPath:  filepath.Join(root, "no-global.yaml"),
		ProjectRoot: root,
		Home:        func() (string, error) { return home, nil },
		Platform:    "darwin",
		Probe: func(string, []string) model.SandboxProbe {
			return model.SandboxProbe{Class: class, Stderr: stderr}
		},
	})
	if err != nil {
		t.Fatalf("approve.Load: %v", err)
	}

	rs := store.Ruleset()
	resolved, _ := model.ResolveCommand([]string{"echo", "hi"}, "")
	v := rs.Evaluate(approve.Subject{Argv: []string{"echo", "hi"}, Canonical: resolved.CanonicalArgv(), Resolved: resolved.ResolvedArgv()}, nil, approve.CommandResolver(""))

	return rs, v
}

func TestCheckLabelsANestingResultAsTheShellsState(t *testing.T) {
	const nested = "sandbox-exec: sandbox_apply: Operation not permitted"

	rs, v := loadWithProbe(t, model.SandboxNested, nested)

	info := sandboxInfoFor(rs, v)
	if info == nil || info.Class != "nested" || info.ProbeScope != "shell" {
		t.Fatalf("info = %+v, want the nested class labeled as the shell's", info)
	}

	var buf bytes.Buffer
	writeSandboxText(&buf, info)

	for _, want := range []string{"probe in this shell: nested", "this shell is inside a sandbox", "The server checks its own state", nested} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("text = %q, want it to contain %q", buf.String(), want)
		}
	}
}

func TestCheckLabelsAnAppliesResultAsTheShellsStateWithoutNestingNotes(t *testing.T) {
	rs, v := loadWithProbe(t, model.SandboxApplies, "")

	info := sandboxInfoFor(rs, v)
	if info == nil || info.ProbeScope != "shell" {
		t.Fatalf("info = %+v, want the probe labeled as the shell's", info)
	}

	var buf bytes.Buffer
	writeSandboxText(&buf, info)

	if !strings.Contains(buf.String(), "probe in this shell: applies") || strings.Contains(buf.String(), "inside a sandbox") {
		t.Errorf("text = %q, want the label and no nesting note", buf.String())
	}
}
