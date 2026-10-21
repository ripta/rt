package approve

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type warnTest struct {
	Name    string
	Global  string
	Project string
	Want    []WarningKind
}

var warnTests = []warnTest{
	{
		Name:    "a clean wrapper rule and an inner allow warn nothing",
		Project: "version: 1\nallow:\n  - wrapper: [nice, -n, \"19\"]\n  - prefix: [make]\n",
	},
	{
		Name:    "a short flag with a value after it does not warn",
		Project: "version: 1\nallow:\n  - wrapper: [timeout, -s, KILL, \"5\"]\n",
	},
	{
		Name:    "a trailing separator never warns",
		Project: "version: 1\nallow:\n  - wrapper: [nice, -n, \"19\", --]\n",
	},
	{
		Name:    "a flag that carries its value does not warn",
		Project: "version: 1\nallow:\n  - wrapper: [sudo, --user=bob]\n  - wrapper: [nice, -n19]\n",
	},
	{
		Name:    "a trailing short flag warns",
		Project: "version: 1\nallow:\n  - wrapper: [sudo, -u]\n",
		Want:    []WarningKind{WarnDanglingFlag},
	},
	{
		Name:    "a trailing boolean flag warns too",
		Project: "version: 1\nallow:\n  - wrapper: [caffeinate, -i]\n",
		Want:    []WarningKind{WarnDanglingFlag},
	},
	{
		Name:    "a trailing long flag without a value warns",
		Project: "version: 1\nallow:\n  - wrapper: [sudo, --preserve-env]\n",
		Want:    []WarningKind{WarnDanglingFlag},
	},
	{
		Name:    "a single dash is not a flag",
		Project: "version: 1\nallow:\n  - wrapper: [tool, \"-\"]\n",
	},
	{
		Name:    "a wrapper rule on a listed program warns that it has no effect",
		Project: "version: 1\nallow:\n  - wrapper: [watch]\n",
		Want:    []WarningKind{WarnBreaksContract},
	},
	{
		Name:    "a listed two-token entry is matched as a leading prefix of the rule",
		Project: "version: 1\nallow:\n  - wrapper: [docker, exec, -i]\n",
		Want:    []WarningKind{WarnBreaksContract, WarnDanglingFlag},
	},
	{
		Name:    "a path-form wrapper rule is matched by basename",
		Project: "version: 1\nallow:\n  - wrapper: [/usr/bin/watch, -n, \"2\"]\n",
		Want:    []WarningKind{WarnBreaksContract},
	},
	{
		Name:    "a rule shorter than a listed entry still peels most layers and does not warn",
		Project: "version: 1\nallow:\n  - wrapper: [sudo]\n",
	},
	{
		Name:    "a deny rule that starts with a dash warns",
		Project: "version: 1\ndeny:\n  - prefix: [-n]\n",
		Want:    []WarningKind{WarnDashFirstToken},
	},
	{
		Name:    "a restrict rule that starts with a dash warns",
		Project: "version: 1\nrestrict:\n  - exact: [-x, y]\n",
		Want:    []WarningKind{WarnDashFirstToken},
	},
	{
		Name:    "a deny glob that starts with a dash is not checked",
		Project: "version: 1\ndeny:\n  - glob: \"-n *\"\n",
	},
	{
		Name:    "a prefix allow on the wrapper's program covers the wrapper rule",
		Project: "version: 1\nallow:\n  - prefix: [nice]\n  - wrapper: [nice, -n, \"19\"]\n",
		Want:    []WarningKind{WarnCoveredByAllow},
	},
	{
		Name:    "equal tokens are covered",
		Project: "version: 1\nallow:\n  - prefix: [nice, -n, \"19\"]\n  - wrapper: [nice, -n, \"19\"]\n",
		Want:    []WarningKind{WarnCoveredByAllow},
	},
	{
		Name:    "a path-form prefix covers a bare-name wrapper rule by basename",
		Project: "version: 1\nallow:\n  - prefix: [/usr/bin/nice]\n  - wrapper: [nice, -n, \"19\"]\n",
		Want:    []WarningKind{WarnCoveredByAllow},
	},
	{
		Name:    "a bare-name prefix covers a path-form wrapper rule by basename",
		Project: "version: 1\nallow:\n  - prefix: [nice]\n  - wrapper: [/usr/bin/nice, -n, \"19\"]\n",
		Want:    []WarningKind{WarnCoveredByAllow},
	},
	{
		Name:    "two path-form first tokens compare byte-exact",
		Project: "version: 1\nallow:\n  - prefix: [/usr/bin/nice]\n  - wrapper: [/usr/local/bin/nice, -n, \"19\"]\n",
	},
	{
		Name:    "a prefix that differs after the first token does not cover",
		Project: "version: 1\nallow:\n  - prefix: [nice, -n, \"20\"]\n  - wrapper: [nice, -n, \"19\"]\n",
	},
	{
		Name:    "a prefix longer than the wrapper rule does not cover",
		Project: "version: 1\nallow:\n  - prefix: [nice, -n, \"19\", make]\n  - wrapper: [nice, -n, \"19\"]\n",
	},
	{
		Name:    "an exact allow is not checked",
		Project: "version: 1\nallow:\n  - exact: [nice, -n, \"19\"]\n  - wrapper: [nice, -n, \"19\"]\n",
	},
	{
		Name:    "a glob allow is not checked",
		Project: "version: 1\nallow:\n  - glob: \"nice *\"\n  - wrapper: [nice, -n, \"19\"]\n",
	},
	{
		Name:    "a prefix allow in the other layer covers the wrapper rule",
		Global:  "version: 1\nallow:\n  - prefix: [/usr/bin/nice]\n",
		Project: "version: 1\nallow:\n  - wrapper: [nice, -n, \"19\"]\n",
		Want:    []WarningKind{WarnCoveredByAllow},
	},
	{
		Name:    "a prefix deny makes the wrapper rule dead",
		Global:  "version: 1\ndeny:\n  - prefix: [nice]\n",
		Project: "version: 1\nallow:\n  - wrapper: [nice, -n, \"19\"]\n",
		Want:    []WarningKind{WarnDeadByDeny},
	},
	{
		Name:    "a built-in deny makes the wrapper rule dead",
		Project: "version: 1\nallow:\n  - wrapper: [env, A=1]\n",
		Want:    []WarningKind{WarnDeadByDeny},
	},
	{
		Name:    "an exact deny is not checked",
		Project: "version: 1\ndeny:\n  - exact: [nice, -n]\nallow:\n  - wrapper: [nice, -n, \"19\"]\n",
	},
}

func sortedKinds(warnings []Warning) []WarningKind {
	var kinds []WarningKind
	for _, w := range warnings {
		kinds = append(kinds, w.Kind)
	}
	slices.Sort(kinds)

	return kinds
}

func TestWarnings(t *testing.T) {
	t.Parallel()

	for _, test := range warnTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()

			globalPath := filepath.Join(t.TempDir(), "absent.yaml")
			if test.Global != "" {
				globalPath = writeGlobal(t, test.Global)
			}

			s, err := Load(LoadOptions{GlobalPath: globalPath, ProjectRoot: writeProject(t, test.Project)})
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}

			want := slices.Clone(test.Want)
			slices.Sort(want)

			got := s.Warnings()
			if !slices.Equal(sortedKinds(got), want) {
				t.Errorf("warning kinds = %v, want %v: %+v", sortedKinds(got), want, got)
			}
		})
	}
}

func TestWarningTextAdvisesOnTheBareFlag(t *testing.T) {
	t.Parallel()

	s, err := Load(LoadOptions{
		GlobalPath:  filepath.Join(t.TempDir(), "absent.yaml"),
		ProjectRoot: writeProject(t, "version: 1\nallow:\n  - wrapper: [sudo, -u]\n"),
	})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	got := s.Warnings()
	if len(got) != 1 {
		t.Fatalf("warnings = %+v, want one", got)
	}

	for _, want := range []string{"wrapper [sudo, -u]", "[caffeinate, -i]", "Appending --"} {
		if !strings.Contains(got[0].Message, want) {
			t.Errorf("message %q missing %q", got[0].Message, want)
		}
	}
}

func TestDiagnoseLayerWarningsExcludeMergedFindings(t *testing.T) {
	t.Parallel()

	global, project, err := Diagnose(LoadOptions{
		GlobalPath:  writeGlobal(t, "version: 1\nallow:\n  - prefix: [nice]\n"),
		ProjectRoot: writeProject(t, "version: 1\nallow:\n  - wrapper: [nice, -n, \"19\"]\n  - wrapper: [sudo, -u]\n"),
	})
	if err != nil {
		t.Fatalf("Diagnose() error: %v", err)
	}

	if len(global.Warnings) != 0 {
		t.Errorf("global warnings = %+v, want none", global.Warnings)
	}
	if kinds := sortedKinds(project.Warnings); !slices.Equal(kinds, []WarningKind{WarnDanglingFlag}) {
		t.Errorf("project warning kinds = %v, want only the dangling flag", kinds)
	}

	merged, ok := MergedWarnings(global, project)
	if !ok {
		t.Fatal("MergedWarnings ok = false, want true")
	}
	if kinds := sortedKinds(merged); !slices.Equal(kinds, []WarningKind{WarnCoveredByAllow}) {
		t.Errorf("merged warning kinds = %v, want only the covering prefix", kinds)
	}
}

func TestMergedWarningsSkippedWhenALayerFailsToParse(t *testing.T) {
	t.Parallel()

	global, project, err := Diagnose(LoadOptions{
		GlobalPath:  writeGlobal(t, "version: 1\nmode: loose\n"),
		ProjectRoot: writeProject(t, "version: 1\nallow:\n  - wrapper: [nice]\n"),
	})
	if err != nil {
		t.Fatalf("Diagnose() error: %v", err)
	}

	if _, ok := MergedWarnings(global, project); ok {
		t.Error("MergedWarnings ok = true, want false when a layer failed to parse")
	}
}
