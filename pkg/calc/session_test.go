package calc

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

type parseFormatTest struct {
	Name        string
	Args        []string
	WantVersion int
	WantErr     bool
}

var parseFormatTests = []parseFormatTest{
	{Name: "version 2", Args: []string{"version=2"}, WantVersion: 2},
	{Name: "version 1", Args: []string{"version=1"}, WantVersion: 1},
	{Name: "saved timestamp is ignored", Args: []string{"version=2", "saved=2026-10-19T12:00:00Z"}, WantVersion: 2},
	{Name: "unknown keys are ignored", Args: []string{"flavor=mint", "version=2", "x="}, WantVersion: 2},
	{Name: "newer version", Args: []string{"version=3"}, WantErr: true},
	{Name: "missing version", Args: []string{"saved=2026-10-19T12:00:00Z"}, WantErr: true},
	{Name: "no arguments", Args: nil, WantErr: true},
	{Name: "non-numeric version", Args: []string{"version=two"}, WantErr: true},
	{Name: "empty version", Args: []string{"version="}, WantErr: true},
	{Name: "zero version", Args: []string{"version=0"}, WantErr: true},
	{Name: "negative version", Args: []string{"version=-1"}, WantErr: true},
	{Name: "argument without equals", Args: []string{"version"}, WantErr: true},
	{Name: "unknown argument without equals", Args: []string{"version=2", "bogus"}, WantErr: true},
}

func TestParseFormat(t *testing.T) {
	t.Parallel()

	for _, test := range parseFormatTests {
		t.Run(test.Name, func(t *testing.T) {
			got, err := parseFormat(test.Args)
			if test.WantErr {
				if !errors.Is(err, ErrInvalidFormat) {
					t.Fatalf("parseFormat(%q) error = %v, want ErrInvalidFormat", test.Args, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseFormat(%q) error = %v", test.Args, err)
			}

			if got.Version != test.WantVersion {
				t.Errorf("parseFormat(%q) version = %d, want %d", test.Args, got.Version, test.WantVersion)
			}
		})
	}
}

type saveHistoryTest struct {
	Name  string
	Lines []string
	Want  []string
}

var saveHistoryTests = []saveHistoryTest{
	{
		Name:  "consecutive entries from 0 carry no markers",
		Lines: []string{"2+3", "$0*2", "$1+1"},
		Want:  []string{"2+3", "$0*2", "$1+1"},
	},
	{
		Name:  "a meta-command leaves a gap",
		Lines: []string{"2+3", ".show", "$0*2"},
		Want:  []string{"2+3", ".next 2", "$0*2"},
	},
	{
		Name:  "a failed meta-command leaves a gap",
		Lines: []string{"2+3", ".bogus", "$0*2"},
		Want:  []string{"2+3", ".next 2", "$0*2"},
	},
	{
		Name:  "a failed expression stays in the run",
		Lines: []string{"2+3", "$9", "$0*2"},
		Want:  []string{"2+3", "$9", "$0*2"},
	},
	{
		Name:  "the first entry not at line 0 gets a marker",
		Lines: []string{".show", "1+1"},
		Want:  []string{".next 1", "1+1"},
	},
	{
		Name:  "each run after a gap gets one marker",
		Lines: []string{".show", "1+1", "2+2", ".show", ".show", "3+3", "4+4"},
		Want:  []string{".next 1", "1+1", "2+2", ".next 5", "3+3", "4+4"},
	},
	{
		Name:  "no entries writes no history",
		Lines: []string{".show"},
		Want:  nil,
	},
}

func TestSaveHistoryMarkers(t *testing.T) {
	for _, test := range saveHistoryTests {
		t.Run(test.Name, func(t *testing.T) {
			got := saveSession(t, test.Lines)

			if !strings.HasPrefix(got[0], ".format version=2 saved=2026-10-19T12:00:00Z") {
				t.Errorf("first line = %q, want a .format line with the injected timestamp", got[0])
			}

			var history []string
			for _, line := range got[1:] {
				if line == "" || strings.HasPrefix(line, ".set ") {
					continue
				}

				history = append(history, line)
			}

			if strings.Join(history, "\n") != strings.Join(test.Want, "\n") {
				t.Errorf("history lines = %q, want %q", history, test.Want)
			}
		})
	}
}

func TestSaveSettingsSorted(t *testing.T) {
	got := saveSession(t, []string{"1+1"})

	var names []string
	for _, line := range got {
		if strings.HasPrefix(line, ".set ") {
			names = append(names, strings.Fields(line)[1])
		}
	}

	if len(names) == 0 {
		t.Fatal("saved file has no .set lines")
	}

	if !sort.StringsAreSorted(names) {
		t.Errorf(".set lines are not sorted by name: %q", names)
	}
}

func TestSaveHistoryRecordsLineNumbers(t *testing.T) {
	c := &Calculator{DecimalPlaces: 30}

	captureStdout(t, func() {
		c.processLine("2+3", ModeREPL, 0)
		c.processLine(".show", ModeREPL, 0)
		c.processLine("$0*2", ModeREPL, 0)
	})

	want := []historyEntry{{Expr: "2+3", Line: 0}, {Expr: "$0*2", Line: 2}}
	if len(c.history) != len(want) {
		t.Fatalf("history = %v, want %v", c.history, want)
	}

	for i := range want {
		if c.history[i] != want[i] {
			t.Errorf("history[%d] = %v, want %v", i, c.history[i], want[i])
		}
	}
}

func TestFormatMetaCommand(t *testing.T) {
	t.Parallel()

	for _, cmd := range []string{".format version=2", ".format version=1 saved=x", ".fo version=2", ".f version=2"} {
		c := &Calculator{DecimalPlaces: 30}
		if err := c.handleMetaCommand(cmd); err != nil {
			t.Errorf("handleMetaCommand(%q) = %v, want nil", cmd, err)
		}
	}

	for _, cmd := range []string{".format", ".format version=3", ".f version=x"} {
		c := &Calculator{DecimalPlaces: 30}
		if err := c.handleMetaCommand(cmd); err == nil {
			t.Errorf("handleMetaCommand(%q) = nil, want error", cmd)
		}
	}
}

// saveSession runs lines at the prompt, saves the session, and returns the saved file's lines.
func saveSession(t *testing.T, lines []string) []string {
	t.Helper()

	c := &Calculator{
		DecimalPlaces: 30,
		now: func() time.Time {
			return time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
		},
	}

	path := filepath.Join(t.TempDir(), "session.txt")

	captureStdout(t, func() {
		for _, line := range lines {
			c.processLine(line, ModeREPL, 0)
		}

		if err := c.handleSave([]string{path}); err != nil {
			t.Errorf("handleSave() = %v", err)
		}
	})

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading saved file: %v", err)
	}

	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}
