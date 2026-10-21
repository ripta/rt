package approve

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/model"
)

func TestAppendProjectAllowExact(t *testing.T) {
	t.Parallel()

	root := writeProject(t, "version: 1\nallow:\n  - prefix: [make]\n")
	s := loadProject(t, root)

	tokens := []string{"/usr/bin/nice", "-n", "10", "make"}
	if err := s.AppendProjectAllowExact(tokens, WriteDirect); err != nil {
		t.Fatalf("AppendProjectAllowExact: %v", err)
	}

	raw, err := os.ReadFile(ProjectPath(root, ""))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if !strings.Contains(string(raw), "exact: [/usr/bin/nice, -n, \"10\", make]") {
		t.Errorf("written file lacks the exact rule:\n%s", raw)
	}

	if got := s.Ruleset().Evaluate(identitySubject(tokens), nil, nil); got.Decision != DecisionRun {
		t.Errorf("live ruleset decision = %v, want run", got.Decision)
	}
	if got := s.Ruleset().Evaluate(identitySubject(append(slices.Clone(tokens), "more")), nil, nil); got.Decision == DecisionRun {
		t.Errorf("an exact rule allowed a longer argv")
	}

	reloaded := loadProject(t, root)
	if got := reloaded.Ruleset().Evaluate(identitySubject(tokens), nil, nil); got.Decision != DecisionRun {
		t.Errorf("reloaded decision = %v, want run", got.Decision)
	}
}

func TestAppendProjectAllowRefusesLeadingDash(t *testing.T) {
	t.Parallel()

	root := writeProject(t, "version: 1\n")
	s := loadProject(t, root)

	if err := s.AppendProjectAllowPrefix([]string{"-n"}, WriteDirect); !errors.Is(err, ErrDashFirstToken) {
		t.Errorf("prefix: err = %v, want ErrDashFirstToken", err)
	}
	if err := s.AppendProjectAllowExact([]string{"-n", "19"}, WriteDirect); !errors.Is(err, ErrDashFirstToken) {
		t.Errorf("exact: err = %v, want ErrDashFirstToken", err)
	}

	raw, err := os.ReadFile(ProjectPath(root, ""))
	if err != nil {
		t.Fatalf("reading project file: %v", err)
	}
	if strings.Contains(string(raw), "-n") {
		t.Errorf("a refused rule was written:\n%s", raw)
	}
}

type suggestTest struct {
	Name     string
	Argv     []string
	Path     string
	Kind     RuleKind
	Want     []string
	Heads    bool
	NearMiss string
}

var suggestTests = []suggestTest{
	{Name: "a plain program keeps the prefix suggestion", Argv: []string{"make", "test"}, Path: "/usr/bin/make", Kind: KindPrefix, Want: []string{"/usr/bin/make", "test"}},
	{Name: "a bare wrapper token is read by basename", Argv: []string{"nice", "-n", "10", "make"}, Path: "/usr/bin/nice", Kind: KindExact, Want: []string{"/usr/bin/nice", "-n", "10", "make"}, Heads: true},
	{Name: "a wrapper program called by path still heads the rule", Argv: []string{"/usr/local/bin/nice"}, Path: "/usr/local/bin/nice", Kind: KindExact, Want: []string{"/usr/local/bin/nice"}, Heads: true},
	{Name: "a path wrapper token matches the canonical path", Argv: []string{"timeout", "9", "make"}, Path: "/opt/bin/timeout", Kind: KindExact, Want: []string{"/opt/bin/timeout", "9", "make"}, Heads: true},
	{Name: "a path wrapper token does not match another file of that name", Argv: []string{"timeout", "9", "make"}, Path: "/usr/bin/timeout", Kind: KindPrefix, Want: []string{"/usr/bin/timeout"}},
	{Name: "the guard case names the wrapper rule that nearly matched", Argv: []string{"nice", "-n", "19", "--", "make"}, Path: "/usr/bin/nice", Kind: KindExact, Want: []string{"/usr/bin/nice", "-n", "19", "--", "make"}, Heads: true, NearMiss: "wrapper [nice, -n, 19]"},
}

func TestSuggest(t *testing.T) {
	t.Parallel()

	root := writeProject(t, "version: 1\nallow:\n  - wrapper: [nice, -n, \"19\"]\n  - wrapper: [/opt/bin/timeout, \"5\"]\n")
	rs := loadProject(t, root).Ruleset()

	for _, tt := range suggestTests {
		t.Run(tt.Name, func(t *testing.T) {
			res := &model.Resolution{Argv: tt.Argv, Resolved: tt.Path, Canonical: tt.Path}
			got := rs.Suggest(tt.Argv, res)

			if got.Kind != tt.Kind || !slices.Equal(got.Tokens, tt.Want) || got.HeadsWrapper != tt.Heads {
				t.Errorf("Suggest = %+v, want kind %v tokens %v heads %v", got, tt.Kind, tt.Want, tt.Heads)
			}

			near := ""
			if got.NearMiss != nil {
				near = got.NearMiss.String()
			}
			if near != tt.NearMiss {
				t.Errorf("near miss = %q, want %q", near, tt.NearMiss)
			}
		})
	}
}

func TestInnerVerdictLayerCarriesItsPermittingRules(t *testing.T) {
	t.Parallel()

	rs := loadProject(t, writeProject(t, "version: 1\nallow:\n  - wrapper: [/usr/bin/nice]\n")).Ruleset()
	subj := Subject{Argv: []string{"nice", "make"}, Canonical: []string{"/usr/bin/nice", "make"}}

	v := rs.Evaluate(subj, nil, func(argv []string) *model.Resolution {
		return &model.Resolution{Argv: argv, Resolved: "/usr/bin/make", Canonical: "/usr/bin/make"}
	})
	if v.Decision != DecisionPrompt || len(v.Chain) != 2 {
		t.Fatalf("verdict = %+v, want a prompt behind one wrapper", v)
	}

	top := v.Chain[0]
	if top.Step != StepInner || !top.PathForm || len(top.Candidates) != 1 {
		t.Errorf("top layer = step %v pathForm %v candidates %d, want the permitting path-form wrapper rule", top.Step, top.PathForm, len(top.Candidates))
	}
}
