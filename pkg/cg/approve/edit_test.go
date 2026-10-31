package approve

import (
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg/model"
)

const editRules = "version: 1\nallow:\n  - wrapper: [nice, -n, \"19\"]\n  - wrapper: [/opt/bin/timeout, \"5\"]\n  - wrapper: [./bin/run]\n"

type editTest struct {
	Name      string
	Argv      []string
	Resolved  string
	Canonical string
	Tokens    []string
	ExtraArgs bool

	WantKind   RuleKind
	WantReason EditReason
	WantReject bool
}

var editTests = []editTest{
	{Name: "an unchanged field is exact", Argv: []string{"make", "-j8", "test"}, Resolved: "/usr/bin/make", Canonical: "/usr/bin/make", Tokens: []string{"/usr/bin/make", "-j8", "test"}, WantKind: KindExact},
	{Name: "the box widens an unchanged field to a prefix", Argv: []string{"make", "-j8", "test"}, Resolved: "/usr/bin/make", Canonical: "/usr/bin/make", Tokens: []string{"/usr/bin/make", "-j8", "test"}, ExtraArgs: true, WantKind: KindPrefix},
	{Name: "a leading part is a prefix", Argv: []string{"make", "-j8", "test"}, Resolved: "/usr/bin/make", Canonical: "/usr/bin/make", Tokens: []string{"/usr/bin/make", "-j8"}, WantKind: KindPrefix},
	{Name: "a trimmed field is a prefix whatever the box says", Argv: []string{"make", "-j8", "test"}, Resolved: "/usr/bin/make", Canonical: "/usr/bin/make", Tokens: []string{"/usr/bin/make"}, ExtraArgs: true, WantKind: KindPrefix},
	{Name: "a bare name counts as a leading part", Argv: []string{"make", "-j8", "test"}, Resolved: "/opt/homebrew/bin/make", Canonical: "/opt/homebrew/bin/make", Tokens: []string{"make", "-j8"}, WantKind: KindPrefix},
	{Name: "a bare name with every token is exact", Argv: []string{"make", "-j8", "test"}, Resolved: "/usr/bin/make", Canonical: "/usr/bin/make", Tokens: []string{"make", "-j8", "test"}, WantKind: KindExact},
	{Name: "the resolved path of a symlink counts", Argv: []string{"cc", "x.c"}, Resolved: "/usr/bin/cc", Canonical: "/usr/bin/clang", Tokens: []string{"/usr/bin/cc", "x.c"}, WantKind: KindExact},
	{Name: "a changed tail token misses", Argv: []string{"make", "-j8", "test"}, Resolved: "/usr/bin/make", Canonical: "/usr/bin/make", Tokens: []string{"/usr/bin/make", "-j4", "test"}, WantReject: true, WantReason: EditMismatch},
	{Name: "a longer rule misses", Argv: []string{"make", "test"}, Resolved: "/usr/bin/make", Canonical: "/usr/bin/make", Tokens: []string{"/usr/bin/make", "test", "install"}, WantReject: true, WantReason: EditMismatch},
	{Name: "another program misses", Argv: []string{"make", "test"}, Resolved: "/usr/bin/make", Canonical: "/usr/bin/make", Tokens: []string{"cmake"}, WantReject: true, WantReason: EditMismatch},
	{Name: "another path misses", Argv: []string{"make", "test"}, Resolved: "/usr/bin/make", Canonical: "/usr/bin/make", Tokens: []string{"/opt/bin/make", "test"}, WantReject: true, WantReason: EditMismatch},
	{Name: "a path misses when the executable has no canonical path", Argv: []string{"./tool", "x"}, Resolved: "/work/tool", Tokens: []string{"/work/tool", "x"}, WantReject: true, WantReason: EditMismatch},
	{Name: "the bare name matches when the executable has no canonical path", Argv: []string{"./tool", "x"}, Resolved: "/work/tool", Tokens: []string{"tool", "x"}, WantKind: KindExact},
	{Name: "a project-relative first token matches after joining the root", Argv: []string{"./bin/tool", "x"}, Resolved: "ROOT/bin/tool", Canonical: "ROOT/bin/tool", Tokens: []string{"./bin/tool", "x"}, WantKind: KindExact},
	{Name: "a wrapper program unchanged is exact", Argv: []string{"nice", "-n", "10", "make"}, Resolved: "/usr/bin/nice", Canonical: "/usr/bin/nice", Tokens: []string{"/usr/bin/nice", "-n", "10", "make"}, WantKind: KindExact},
	{Name: "a trimmed wrapper program is rejected", Argv: []string{"nice", "-n", "10", "make"}, Resolved: "/usr/bin/nice", Canonical: "/usr/bin/nice", Tokens: []string{"/usr/bin/nice", "-n", "10"}, WantReject: true, WantReason: EditWrapperTrim},
	{Name: "a wrapper program with the box is rejected", Argv: []string{"nice", "-n", "10", "make"}, Resolved: "/usr/bin/nice", Canonical: "/usr/bin/nice", Tokens: []string{"/usr/bin/nice", "-n", "10", "make"}, ExtraArgs: true, WantReject: true, WantReason: EditWrapperBox},
	{Name: "a wrapper program respelled to its bare name stays exact", Argv: []string{"nice", "-n", "10", "make"}, Resolved: "/usr/bin/nice", Canonical: "/usr/bin/nice", Tokens: []string{"nice", "-n", "10", "make"}, WantKind: KindExact},
	{Name: "a symlink to a wrapper program, respelled to the canonical path and trimmed, is rejected", Argv: []string{"n", "-n", "10", "make"}, Resolved: "/tmp/links/n", Canonical: "/usr/bin/nice", Tokens: []string{"/usr/bin/nice", "-n"}, WantReject: true, WantReason: EditWrapperTrim},
	{Name: "a symlink to a wrapper program, respelled to the canonical path with the box, is rejected", Argv: []string{"n", "-n", "10", "make"}, Resolved: "/tmp/links/n", Canonical: "/usr/bin/nice", Tokens: []string{"/usr/bin/nice", "-n", "10", "make"}, ExtraArgs: true, WantReject: true, WantReason: EditWrapperBox},
	{Name: "a path-form wrapper program respelled to a bare name and trimmed is rejected", Argv: []string{"timeout", "5", "make"}, Resolved: "/opt/bin/timeout", Canonical: "/opt/bin/timeout", Tokens: []string{"timeout", "5"}, WantReject: true, WantReason: EditWrapperTrim},
	{Name: "another file of a path-form wrapper's name, respelled bare and trimmed, is rejected", Argv: []string{"timeout", "5", "make"}, Resolved: "/usr/bin/timeout", Canonical: "/usr/bin/timeout", Tokens: []string{"timeout", "5"}, WantReject: true, WantReason: EditWrapperTrim},
	{Name: "a project-relative wrapper program trimmed is rejected", Argv: []string{"run", "a", "b"}, Resolved: "ROOT/bin/run", Canonical: "ROOT/bin/run", Tokens: []string{"./bin/run", "a"}, WantReject: true, WantReason: EditWrapperTrim},
}

func TestCheckEdit(t *testing.T) {
	t.Parallel()

	root := writeProject(t, editRules)
	s := loadProject(t, root)

	for _, tt := range editTests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			res := &model.Resolution{Argv: tt.Argv, Resolved: strings.ReplaceAll(tt.Resolved, "ROOT", root), Canonical: strings.ReplaceAll(tt.Canonical, "ROOT", root)}
			targets := []EditTarget{{Argv: tt.Argv, Resolution: res}}

			kind, rejection := s.CheckEdit(tt.Tokens, tt.ExtraArgs, targets)
			if tt.WantReject {
				if rejection == nil || rejection.Reason != tt.WantReason {
					t.Fatalf("CheckEdit = %v, %+v; want reason %v", kind, rejection, tt.WantReason)
				}

				return
			}

			if rejection != nil || kind != tt.WantKind {
				t.Errorf("CheckEdit = %v, %+v; want kind %v", kind, rejection, tt.WantKind)
			}
		})
	}
}

func TestCheckEditMustMatchEveryTarget(t *testing.T) {
	t.Parallel()

	s := loadProject(t, writeProject(t, "version: 1\n"))
	targets := []EditTarget{
		{Argv: []string{"echo", "a"}, Resolution: &model.Resolution{Resolved: "/bin/echo", Canonical: "/bin/echo"}},
		{Argv: []string{"echo", "a"}, Resolution: &model.Resolution{Resolved: "/usr/bin/echo", Canonical: "/usr/bin/echo"}},
	}

	_, rejection := s.CheckEdit([]string{"/bin/echo", "a"}, false, targets)
	if rejection == nil || rejection.Reason != EditMismatch || rejection.Target != 1 {
		t.Errorf("rejection = %+v, want a mismatch on target 1", rejection)
	}

	if _, rejection := s.CheckEdit([]string{"echo", "a"}, false, targets); rejection != nil {
		t.Errorf("the bare name names both programs, got %+v", rejection)
	}
}

type collidesTest struct {
	Name  string
	First string
	Want  bool
}

var collidesTests = []collidesTest{
	{Name: "a bare token equal to a bare head", First: "nice", Want: true},
	{Name: "a path token whose basename is a bare head", First: "/usr/bin/nice", Want: true},
	{Name: "a bare token equal to a path head's basename", First: "timeout", Want: true},
	{Name: "a path token equal to a path head", First: "/opt/bin/timeout", Want: true},
	{Name: "another file of a path head's name", First: "/usr/bin/timeout", Want: false},
	{Name: "a project-relative token joins the root", First: "./bin/run", Want: true},
	{Name: "a project-relative token without a dot joins the root", First: "bin/run", Want: true},
	{Name: "another project-relative path", First: "./bin/other", Want: false},
	{Name: "an unrelated program", First: "make", Want: false},
	{Name: "a longer name is not a basename match", First: "nice2", Want: false},
	{Name: "an empty token", First: "", Want: false},
}

func TestCollidesWithWrapper(t *testing.T) {
	t.Parallel()

	s := loadProject(t, writeProject(t, editRules))

	for _, tt := range collidesTests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			if got := s.CollidesWithWrapper(tt.First); got != tt.Want {
				t.Errorf("CollidesWithWrapper(%q) = %v, want %v", tt.First, got, tt.Want)
			}
		})
	}

}
