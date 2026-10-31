package calc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ripta/reals/pkg/constructive"

	"github.com/ripta/rt/pkg/calc/parser"
)

type nextPromptTest struct {
	Name         string
	Lines        []string
	WantErrLines []int
	WantCount    int
}

var nextPromptTests = []nextPromptTest{
	{
		Name:      "forward jump leaves a gap",
		Lines:     []string{".next 20"},
		WantCount: 20,
	},
	{
		Name:      "a later line is numbered from the target",
		Lines:     []string{"1", "2", ".next 20", "3"},
		WantCount: 21,
	},
	{
		Name:         "the next number itself is rejected",
		Lines:        []string{"1", ".next 1"},
		WantErrLines: []int{1},
		WantCount:    2,
	},
	{
		Name:         "backward is rejected",
		Lines:        []string{"1", "2", "3", ".next 1"},
		WantErrLines: []int{3},
		WantCount:    4,
	},
	{
		Name:         "repeating the last target is rejected",
		Lines:        []string{".next 5", ".next 5"},
		WantErrLines: []int{1},
		WantCount:    6,
	},
	{
		Name:         "a missing number is rejected",
		Lines:        []string{".next"},
		WantErrLines: []int{0},
		WantCount:    1,
	},
	{
		Name:         "a non-numeric number is rejected",
		Lines:        []string{".next soon"},
		WantErrLines: []int{0},
		WantCount:    1,
	},
}

func TestNextAtPrompt(t *testing.T) {
	for _, test := range nextPromptTests {
		t.Run(test.Name, func(t *testing.T) {
			c := &Calculator{DecimalPlaces: 30}

			var errLines []int
			captureStderr(t, func() {
				captureStdout(t, func() {
					for i, line := range test.Lines {
						if err := c.processLine(line, ModeREPL, 0); err != nil {
							errLines = append(errLines, i)
						}
					}
				})
			})

			if fmt.Sprint(errLines) != fmt.Sprint(test.WantErrLines) {
				t.Errorf("lines with errors = %v, want %v", errLines, test.WantErrLines)
			}

			if c.count != test.WantCount {
				t.Errorf("count = %d, want %d", c.count, test.WantCount)
			}
		})
	}
}

func TestNextBindsAtTarget(t *testing.T) {
	c := &Calculator{DecimalPlaces: 30}

	captureStdout(t, func() {
		c.processLine(".next 20", ModeREPL, 0)
		c.processLine("6*7", ModeREPL, 0)
	})

	if got := resultText(t, c, "$20"); got != "42" {
		t.Errorf("$20 = %s, want 42", got)
	}
}

type loadNumberingTest struct {
	Name string

	// Before runs at the prompt first, so the load happens in a session with existing results.
	Before []string

	// File is the loaded file. "{path}" and "{dir}" expand to its path and its directory.
	File string

	// Files are extra files in the same directory, keyed by name.
	Files map[string]string

	WantErr     string
	WantStdout  string
	WantStderr  string
	WantValues  map[string]string
	WantMissing []string
	WantHistory []historyEntry
}

var loadNumberingTests = []loadNumberingTest{
	{
		Name:   "header is unnumbered and a set after it is numbered",
		Before: []string{"100", "200", "300"},
		File: strings.Join([]string{
			".format version=2 saved=2026-10-19T00:00:00Z",
			".set decimal_places 10",
			"2+3",
			".next 5",
			"$0*2",
			".set decimal_places 12",
			"$5+1",
		}, "\n"),
		WantStdout: "Loaded from {path}: results $4-$8",
		WantValues: map[string]string{"$0": "100", "$2": "300", "$4": "5", "$6": "10", "$8": "11"},
		WantMissing: []string{
			"$5", "$7",
		},
		WantHistory: []historyEntry{
			{Expr: "2+3", Line: 4},
			{Expr: "$4*2", Line: 6},
			{Expr: "$6+1", Line: 8},
		},
	},
	{
		Name:   "a dollar inside a string is not a reference",
		Before: []string{"1", "1", "1"},
		File: strings.Join([]string{
			".format version=2",
			`"price $0" 7`,
			"$0+1",
		}, "\n"),
		WantValues: map[string]string{"$4": "7", "$5": "8"},
		WantHistory: []historyEntry{
			{Expr: `"price $0" 7`, Line: 4},
			{Expr: "$4+1", Line: 5},
		},
	},
	{
		Name:   "reference columns are byte offsets in multi-byte text",
		Before: []string{"1", "1", "1"},
		File: strings.Join([]string{
			".format version=2",
			`"héllo wörld ✓" 7`,
			`"日本語" $0 + 1`,
		}, "\n"),
		WantValues: map[string]string{"$4": "7", "$5": "8"},
		WantHistory: []historyEntry{
			{Expr: `"héllo wörld ✓" 7`, Line: 4},
			{Expr: `"日本語" $4 + 1`, Line: 5},
		},
	},
	{
		Name:       "a version 1 file fails every reference without using existing bindings",
		Before:     []string{"100"},
		File:       "2+3\n$0*2\n3",
		WantStderr: "$0: no result for line 0",
		WantStdout: "Loaded from {path}: results $2-$4",
		WantValues: map[string]string{"$0": "100", "$2": "5", "$4": "3"},
		WantMissing: []string{
			"$3",
		},
		WantHistory: []historyEntry{
			{Expr: "2+3", Line: 2},
			{Expr: "3", Line: 4},
		},
	},
	{
		Name:       "an unmapped reference in a version 2 file does not use an existing binding",
		Before:     []string{"1", "2", "3"},
		File:       ".format version=2\n7\n$1+1\n$0+1",
		WantStderr: "no result for line 1",
		WantValues: map[string]string{"$1": "2", "$4": "7", "$6": "8"},
		WantMissing: []string{
			"$5",
		},
		WantHistory: []historyEntry{
			{Expr: "7", Line: 4},
			{Expr: "$4+1", Line: 6},
		},
	},
	{
		Name:       "a reference to a failed line is unmapped",
		File:       ".format version=2\n1/0\n$0+1",
		WantStderr: "no result for line 0",
		WantStdout: "no results bound",
	},
	{
		Name:        "a backward next stops the load and keeps earlier results",
		File:        ".format version=2\n1+1\n2+2\n.next 1\n3+3",
		WantErr:     "invalid .next",
		WantValues:  map[string]string{"$1": "2", "$2": "4"},
		WantMissing: []string{"$3", "$4"},
		WantHistory: []historyEntry{
			{Expr: "1+1", Line: 1},
			{Expr: "2+2", Line: 2},
		},
	},
	{
		Name:        "a repeated next stops the load",
		File:        ".format version=2\n1\n1\n.next 2\n5",
		WantErr:     "is not after the current line 2",
		WantValues:  map[string]string{"$1": "1", "$2": "1"},
		WantMissing: []string{"$3", "$4"},
	},
	{
		Name:        "an unsupported format version stops the load before any result",
		File:        ".format version=3\n1+1",
		WantErr:     "newer than the supported version",
		WantMissing: []string{"$1"},
	},
	{
		Name:        "a malformed format line stops the load",
		File:        ".format version=2\n1+1\n.format version=two\n2+2",
		WantErr:     "invalid .format",
		WantValues:  map[string]string{"$1": "2"},
		WantMissing: []string{"$3"},
	},
	{
		Name:       "a load that binds nothing says so",
		File:       ".format version=2\n.set decimal_places 5",
		WantStdout: "Loaded from {path}: no results bound",
	},
	{
		Name:       "a single result is named without a range",
		File:       ".format version=2\n9",
		WantStdout: "Loaded from {path}: result $1",
	},
	{
		Name: "nested loads number their own files",
		File: ".format version=2\n10\n.load {dir}/inner.calc\n$0+1",
		Files: map[string]string{
			"inner.calc": ".format version=2\n5\n$0*3",
		},
		WantValues: map[string]string{"$1": "10", "$3": "5", "$4": "15", "$5": "11"},
		WantHistory: []historyEntry{
			{Expr: "10", Line: 1},
			{Expr: "5", Line: 3},
			{Expr: "$3*3", Line: 4},
			{Expr: "$1+1", Line: 5},
		},
	},
	{
		Name: "an inner structural error does not stop the outer load",
		File: ".format version=2\n10\n.load {dir}/inner.calc\n$0+1",
		Files: map[string]string{
			"inner.calc": ".format version=2\n5\n.next 0\n6",
		},
		WantValues:  map[string]string{"$1": "10", "$3": "5", "$5": "11"},
		WantMissing: []string{"$4"},
	},
}

func TestLoadNumbering(t *testing.T) {
	for _, test := range loadNumberingTests {
		t.Run(test.Name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "session.calc")
			expand := func(s string) string {
				return strings.NewReplacer("{path}", path, "{dir}", dir).Replace(s)
			}

			writeFile(t, path, expand(test.File)+"\n")
			for name, contents := range test.Files {
				writeFile(t, filepath.Join(dir, name), expand(contents)+"\n")
			}

			c := &Calculator{DecimalPlaces: 30}

			var loadErr error
			var stdout string
			stderr := captureStderr(t, func() {
				stdout = captureStdout(t, func() {
					for _, line := range test.Before {
						c.processLine(line, ModeREPL, 0)
					}

					loadErr = c.processLine(".load "+path, ModeREPL, 0)
				})
			})

			if test.WantErr == "" && loadErr != nil {
				t.Errorf(".load error = %v, want none", loadErr)
			}

			if test.WantErr != "" && (loadErr == nil || !strings.Contains(loadErr.Error(), test.WantErr)) {
				t.Errorf(".load error = %v, want substring %q", loadErr, test.WantErr)
			}

			if want := expand(test.WantStdout); !strings.Contains(stdout, want) {
				t.Errorf("stdout = %q, want substring %q", stdout, want)
			}

			if !strings.Contains(stderr, test.WantStderr) {
				t.Errorf("stderr = %q, want substring %q", stderr, test.WantStderr)
			}

			for ref, want := range test.WantValues {
				if got := resultText(t, c, ref); got != want {
					t.Errorf("%s = %s, want %s", ref, got, want)
				}
			}

			for _, ref := range test.WantMissing {
				if _, err := c.Evaluate(ref); err == nil {
					t.Errorf("%s is bound, want no result", ref)
				}
			}

			if test.WantHistory != nil && fmt.Sprint(c.history) != fmt.Sprint(test.WantHistory) {
				t.Errorf("history = %v, want %v", c.history, test.WantHistory)
			}

			if len(c.loading) != 0 {
				t.Errorf("loading stack not unwound: %v", c.loading)
			}
		})
	}
}

func TestLoadThroughSTDIN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.calc")
	writeFile(t, path, ".format version=2\n6\n$0+1\n")

	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	os.Stdin = r

	fmt.Fprintf(w, "1\n.load %s\n$3*2\n", path)
	w.Close()

	c := &Calculator{DecimalPlaces: 30}

	var stdout string
	captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			if err := c.ProcessSTDIN(); err != nil {
				t.Errorf("ProcessSTDIN() = %v", err)
			}
		})
	})

	if want := "Loaded from " + path + ": results $2-$3"; !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q, want substring %q", stdout, want)
	}

	for ref, want := range map[string]string{"$0": "1", "$2": "6", "$3": "7", "$4": "14"} {
		if got := resultText(t, c, ref); got != want {
			t.Errorf("%s = %s, want %s", ref, got, want)
		}
	}
}

type roundTripTest struct {
	Name  string
	Lines []string

	// WantValues is the result of each expression that succeeded, in order.
	WantValues []string

	// WantFailed is an expression that fails. It is saved, and it is not recorded when loaded.
	WantFailed string
}

var roundTripTests = []roundTripTest{
	{
		Name:       "consecutive lines",
		Lines:      []string{"2+3", "$0*2", "$1+1"},
		WantValues: []string{"5", "10", "11"},
	},
	{
		Name:       "gaps from meta-commands",
		Lines:      []string{"2+3", ".show", "$0*2", ".show", ".show", "$2+1"},
		WantValues: []string{"5", "10", "11"},
	},
	{
		Name:       "a failed line",
		Lines:      []string{"2+3", ".show", "$0*2", "1/0", "$0+$2", ".show", ".show", "$2+1"},
		WantValues: []string{"5", "10", "15", "11"},
		WantFailed: "1/0",
	},
}

func TestSaveLoadRoundTrip(t *testing.T) {
	for _, test := range roundTripTests {
		t.Run(test.Name, func(t *testing.T) {
			dir := t.TempDir()

			saver := newClockCalculator()
			runLines(t, saver, test.Lines)

			if got := historyValues(t, saver); fmt.Sprint(got) != fmt.Sprint(test.WantValues) {
				t.Fatalf("saved session values = %v, want %v", got, test.WantValues)
			}

			saves := []string{saveCalc(t, saver, filepath.Join(dir, "save0.calc"))}

			// A load numbers its lines after the .load line, so a saved file's first numbers shift
			// until the markers settle. After that, loading and saving again changes nothing.
			var reloaded []*Calculator
			for i := range 3 {
				c := newClockCalculator()
				loadCalc(t, c, filepath.Join(dir, fmt.Sprintf("save%d.calc", i)))
				reloaded = append(reloaded, c)

				if got := historyValues(t, c); fmt.Sprint(got) != fmt.Sprint(test.WantValues) {
					t.Errorf("reload %d values = %v, want %v", i+1, got, test.WantValues)
				}

				saves = append(saves, saveCalc(t, c, filepath.Join(dir, fmt.Sprintf("save%d.calc", i+1))))
			}

			if saves[3] != saves[2] {
				t.Errorf("saving again changed the file:\n%s\nwant:\n%s", saves[3], saves[2])
			}

			if fmt.Sprint(reloaded[2].history) != fmt.Sprint(reloaded[1].history) {
				t.Errorf("history = %v, want %v", reloaded[2].history, reloaded[1].history)
			}

			if test.WantFailed != "" {
				if !strings.Contains(saves[0], test.WantFailed) {
					t.Errorf("first save does not hold the failed line %q", test.WantFailed)
				}

				if strings.Contains(saves[1], test.WantFailed) {
					t.Errorf("save after a load holds the failed line %q", test.WantFailed)
				}

				if len(reloaded[0].history) != len(saver.history)-1 {
					t.Errorf("loaded %d entries, want %d", len(reloaded[0].history), len(saver.history)-1)
				}
			}

			existing := newClockCalculator()
			runLines(t, existing, []string{"100", "200", "300", "400"})
			loadCalc(t, existing, filepath.Join(dir, "save0.calc"))

			if got := historyValues(t, existing); fmt.Sprint(got) != fmt.Sprint(test.WantValues) {
				t.Errorf("load into existing results gave %v, want %v", got, test.WantValues)
			}

			if got := resultText(t, existing, "$0"); got != "100" {
				t.Errorf("$0 = %s, want 100", got)
			}
		})
	}
}

func TestSaveLoadNestedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	outer := filepath.Join(dir, "outer.calc")

	writeFile(t, outer, ".format version=2\n10\n.load "+filepath.Join(dir, "inner.calc")+"\n$0+1\n")
	writeFile(t, filepath.Join(dir, "inner.calc"), ".format version=2\n5\n$0*3\n")

	want := []string{"10", "5", "15", "11"}

	first := newClockCalculator()
	loadCalc(t, first, outer)

	if got := historyValues(t, first); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("nested load values = %v, want %v", got, want)
	}

	saves := []string{saveCalc(t, first, filepath.Join(dir, "save0.calc"))}
	for i := range 3 {
		c := newClockCalculator()
		loadCalc(t, c, filepath.Join(dir, fmt.Sprintf("save%d.calc", i)))

		if got := historyValues(t, c); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("reload %d values = %v, want %v", i+1, got, want)
		}

		saves = append(saves, saveCalc(t, c, filepath.Join(dir, fmt.Sprintf("save%d.calc", i+1))))
	}

	if saves[3] != saves[2] {
		t.Errorf("saving again changed the file:\n%s\nwant:\n%s", saves[3], saves[2])
	}
}

func newClockCalculator() *Calculator {
	return &Calculator{
		DecimalPlaces: 30,
		now: func() time.Time {
			return time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
		},
	}
}

// runLines runs lines at the prompt, discarding what they print.
func runLines(t *testing.T, c *Calculator, lines []string) {
	t.Helper()

	captureStderr(t, func() {
		captureStdout(t, func() {
			for _, line := range lines {
				c.processLine(line, ModeREPL, 0)
			}
		})
	})
}

func loadCalc(t *testing.T, c *Calculator, path string) {
	t.Helper()

	runLines(t, c, []string{".load " + path})
}

// saveCalc saves c's session to path and returns the file's text.
func saveCalc(t *testing.T, c *Calculator, path string) string {
	t.Helper()

	runLines(t, c, []string{".save " + path})

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading saved file: %v", err)
	}

	return string(b)
}

// historyValues returns the result bound for each history entry, at the entry's own line. An entry
// that failed has no result and is skipped.
func historyValues(t *testing.T, c *Calculator) []string {
	t.Helper()

	var values []string
	for _, entry := range c.history {
		if _, err := c.Evaluate(fmt.Sprintf("$%d", entry.Line)); err != nil {
			continue
		}

		values = append(values, resultText(t, c, fmt.Sprintf("$%d", entry.Line)))
	}

	return values
}

func resultText(t *testing.T, c *Calculator, ref string) string {
	t.Helper()

	res, err := c.Evaluate(ref)
	if err != nil {
		t.Fatalf("Evaluate(%s): %v", ref, err)
	}

	text, err := constructive.Text(context.Background(), res.Constructive(), 0, 10)
	if err != nil {
		t.Fatalf("Text(%s): %v", ref, err)
	}

	return text
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func TestLoadAbortsOnCancellation(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "inner.calc")
	outer := filepath.Join(dir, "outer.calc")

	writeFile(t, inner, ".format version=2\n5\n6\n")
	writeFile(t, outer, ".format version=2\n1\n.load "+inner+"\n2\n3\n")

	for _, path := range []string{inner, outer} {
		c := newClockCalculator()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c.interrupt = ctx

		var err error
		captureStderr(t, func() {
			captureStdout(t, func() {
				err = c.handleLoad([]string{path})
			})
		})

		if !errors.Is(err, ErrLoadStopped) || !errors.Is(err, context.Canceled) {
			t.Fatalf("load %s: err = %v, want a stopped load wrapping context.Canceled", path, err)
		}
		if !strings.Contains(err.Error(), "interrupted at line") {
			t.Errorf("load %s: err = %q does not name the interrupted line", path, err)
		}
		if len(c.history) != 0 {
			t.Errorf("load %s ran %d lines after cancellation", path, len(c.history))
		}
	}
}

func TestNestedLoadCancellationStopsOuterLoad(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: %w", ErrLoadStopped, parser.ErrCancelled)
	if !isStructural(err) {
		t.Error("a cancelled nested load is not structural, so the outer load would keep going")
	}
	if isStructural(fmt.Errorf("%w at line 1", ErrLoadStopped)) {
		t.Error("a stopped nested load that was not cancelled became structural")
	}
}

func TestExecuteClearsInterrupt(t *testing.T) {
	c := newClockCalculator()

	captureStdout(t, func() {
		c.Execute("1 + 1")
	})

	if c.interrupt != nil {
		t.Error("Execute left its interrupt context installed")
	}
	if got := resultText(t, c, "$0"); got != "2" {
		t.Errorf("$0 = %s, want 2", got)
	}
}

func TestCancelledLineReportsError(t *testing.T) {
	c := newClockCalculator()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.interrupt = ctx

	var err error
	captureStderr(t, func() {
		err = c.processLine("factorial(5)", ModeREPL, 0)
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestProcessInterruptibleClearsInterrupt(t *testing.T) {
	c := newClockCalculator()

	captureStdout(t, func() {
		c.processInterruptible("1 + 1", ModeSTDIN)
	})

	if c.interrupt != nil {
		t.Error("processInterruptible left its interrupt context installed")
	}
}
