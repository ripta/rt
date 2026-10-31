package calc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/parser"
)

type handleMetaCommandTest struct {
	name    string
	cmd     string
	wantErr bool
}

var handleMetaCommandTests = []handleMetaCommandTest{
	{
		name:    "help command",
		cmd:     ".help",
		wantErr: false,
	},
	{
		name:    "show command",
		cmd:     ".show",
		wantErr: false,
	},
	{
		name:    "unknown command",
		cmd:     ".unknown",
		wantErr: true,
	},
	{
		name:    "empty command",
		cmd:     ".",
		wantErr: true,
	},
	{
		name:    ".s is ambiguous",
		cmd:     ".s",
		wantErr: true,
	},
	{
		name:    ".se alias for .set",
		cmd:     ".se",
		wantErr: true, // will error because no setting args provided
	},
	{
		name:    ".h alias for .help",
		cmd:     ".h",
		wantErr: false,
	},
	{
		name:    ".he alias for .help",
		cmd:     ".he",
		wantErr: false,
	},
	{
		name:    ".sh alias for .show",
		cmd:     ".sh",
		wantErr: false,
	},
	{
		name:    ".sho alias for .show",
		cmd:     ".sho",
		wantErr: false,
	},
	{
		name:    ".show functions",
		cmd:     ".show functions",
		wantErr: false,
	},
	{
		name:    ".sh functions prefix",
		cmd:     ".sh functions",
		wantErr: false,
	},
	{
		name:    ".sh f resolves functions topic by prefix",
		cmd:     ".sh f",
		wantErr: false,
	},
	{
		name:    ".show unknown topic",
		cmd:     ".show bogus",
		wantErr: true,
	},
	{
		name:    ".e alias for .examples",
		cmd:     ".e",
		wantErr: false,
	},
	{
		name:    ".next moves forward",
		cmd:     ".next 5",
		wantErr: false,
	},
	{
		name:    ".n alias for .next",
		cmd:     ".n 5",
		wantErr: false,
	},
	{
		name:    ".next to the current line",
		cmd:     ".next 0",
		wantErr: true,
	},
	{
		name:    ".next without a number",
		cmd:     ".next",
		wantErr: true,
	},
}

func TestHandleMetaCommand(t *testing.T) {
	t.Parallel()

	for _, tt := range handleMetaCommandTests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Calculator{
				DecimalPlaces: 30,
			}

			err := c.handleMetaCommand(tt.cmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleMetaCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleShowTopics(t *testing.T) {
	t.Parallel()

	c := &Calculator{DecimalPlaces: 30}

	if err := c.handleShow(nil); err != nil {
		t.Errorf("handleShow(nil) = %v, want nil", err)
	}
	if err := c.handleShow([]string{"settings"}); err != nil {
		t.Errorf("handleShow(settings) = %v, want nil", err)
	}
	if err := c.handleShow([]string{"functions"}); err != nil {
		t.Errorf("handleShow(functions) = %v, want nil", err)
	}
	if err := c.handleShow([]string{"bogus"}); err == nil {
		t.Error("handleShow(bogus) = nil, want error")
	}
}

func TestExamplesEvaluate(t *testing.T) {
	t.Parallel()

	c := &Calculator{DecimalPlaces: 30}
	for _, ex := range examples {
		if err := c.processLine(ex.Expr, ModeREPL, 0); err != nil {
			t.Errorf("example %q: %v", ex.Expr, err)
		}
	}
}

type parseBoolTest struct {
	name    string
	input   string
	want    bool
	wantErr bool
}

var parseBoolTests = []parseBoolTest{
	{"on", "on", true, false},
	{"off", "off", false, false},
	{"true", "true", true, false},
	{"false", "false", false, false},
	{"yes", "yes", true, false},
	{"no", "no", false, false},
	{"1", "1", true, false},
	{"0", "0", false, false},
	{"ON uppercase", "ON", true, false},
	{"OFF uppercase", "OFF", false, false},
	{"True mixed case", "True", true, false},
	{"False mixed case", "False", false, false},
	{"invalid", "maybe", false, true},
	{"empty", "", false, true},
}

func TestParseBool(t *testing.T) {
	t.Parallel()

	for _, tt := range parseBoolTests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBool(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseBool() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

type formatBoolTest struct {
	name  string
	input bool
	want  string
}

var formatBoolTests = []formatBoolTest{
	{"true", true, "on"},
	{"false", false, "off"},
}

func TestFormatBool(t *testing.T) {
	t.Parallel()

	for _, tt := range formatBoolTests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBool(tt.input)
			if got != tt.want {
				t.Errorf("formatBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMetaCommandPersistence verifies that settings persist across evaluations
func TestMetaCommandPersistence(t *testing.T) {
	c := &Calculator{
		DecimalPlaces: 30,
	}

	err := c.handleSet([]string{"trace", "on"})
	if err != nil {
		t.Fatalf("Failed to set trace: %v", err)
	}

	if !c.Trace {
		t.Error("Trace setting did not persist")
	}

	if err := c.handleSet([]string{"decimal_places", "5"}); err != nil {
		t.Fatalf("Failed to set decimal_places: %v", err)
	}

	if !c.Trace {
		t.Error("Trace setting was lost")
	}
	if c.DecimalPlaces != 5 {
		t.Errorf("DecimalPlaces = %d, want 5", c.DecimalPlaces)
	}
}

// TestIntegrationWithEvaluation tests meta-commands with actual expression evaluation
func TestIntegrationWithEvaluation(t *testing.T) {
	c := &Calculator{
		DecimalPlaces: 30,
	}

	if err := c.handleSet([]string{"decimal_places", "3"}); err != nil {
		t.Fatalf("Failed to set decimal_places: %v", err)
	}

	if c.DecimalPlaces != 3 {
		t.Errorf("DecimalPlaces = %d, want 3", c.DecimalPlaces)
	}

	if err := c.handleSet([]string{"verbose", "on"}); err != nil {
		t.Fatalf("Failed to set verbose: %v", err)
	}

	if !c.Verbose {
		t.Error("Verbose should be enabled")
	}
}

type toggleTest struct {
	name        string
	setting     string
	initialVal  bool
	expectedVal bool
	wantErr     bool
}

var toggleTests = []toggleTest{
	{
		name:        "toggle trace from off to on",
		setting:     "trace",
		initialVal:  false,
		expectedVal: true,
		wantErr:     false,
	},
	{
		name:        "toggle verbose from on to off",
		setting:     "verbose",
		initialVal:  true,
		expectedVal: false,
		wantErr:     false,
	},
	{
		name:        "toggle keep_trailing_zeros",
		setting:     "keep_trailing_zeros",
		initialVal:  false,
		expectedVal: true,
		wantErr:     false,
	},
}

// TestToggle verifies that the toggle command works correctly
func TestToggle(t *testing.T) {
	t.Parallel()

	for _, tt := range toggleTests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Calculator{
				DecimalPlaces: 30,
			}

			setting, _ := findByPrefix(tt.setting, settingsRegistry)
			setting.SetBool(c, tt.initialVal)

			if err := c.handleToggle([]string{tt.setting}); (err != nil) != tt.wantErr {
				t.Errorf("handleToggle() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				got := setting.GetBool(c)
				if got != tt.expectedVal {
					t.Errorf("After toggle, %s = %v, want %v", tt.setting, got, tt.expectedVal)
				}
			}
		})
	}
}

// TestToggleNonBoolean verifies that toggle fails on non-boolean settings
func TestToggleNonBoolean(t *testing.T) {
	c := &Calculator{
		DecimalPlaces: 30,
	}

	err := c.handleToggle([]string{"decimal_places"})
	if err == nil {
		t.Error("Expected error when toggling non-boolean setting, got nil")
	}
}

type toggleInvalidUsageTest struct {
	name string
	args []string
}

var toggleInvalidUsageTests = []toggleInvalidUsageTest{
	{
		name: "no arguments",
		args: []string{},
	},
	{
		name: "too many arguments",
		args: []string{"trace", "extra"},
	},
	{
		name: "unknown setting",
		args: []string{"nonexistent"},
	},
}

// TestToggleInvalidUsage tests error cases for toggle
func TestToggleInvalidUsage(t *testing.T) {
	t.Parallel()
	for _, tt := range toggleInvalidUsageTests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Calculator{
				DecimalPlaces: 30,
			}

			err := c.handleToggle(tt.args)
			if err == nil {
				t.Error("Expected error, got nil")
			}
		})
	}
}

func TestREPLStoresResultHistory(t *testing.T) {
	c := &Calculator{DecimalPlaces: 30}

	if err := c.processLine("2 + 3", ModeREPL, 0); err != nil {
		t.Fatalf("processLine: %v", err)
	}

	res, err := c.Evaluate("$0")
	if err != nil {
		t.Fatalf("Evaluate($0): %v", err)
	}

	got := approxFloat(t, res)
	if diff := math.Abs(got - 5); diff > 1e-9 {
		t.Fatalf("$0 = %v, want 5", got)
	}
}

func TestREPLCrossReference(t *testing.T) {
	c := &Calculator{DecimalPlaces: 30}

	if err := c.processLine("5", ModeREPL, 0); err != nil {
		t.Fatalf("processLine(5): %v", err)
	}
	if err := c.processLine("$0 * 2", ModeREPL, 0); err != nil {
		t.Fatalf("processLine($0 * 2): %v", err)
	}

	res, err := c.Evaluate("$1")
	if err != nil {
		t.Fatalf("Evaluate($1): %v", err)
	}

	got := approxFloat(t, res)
	if diff := math.Abs(got - 10); diff > 1e-9 {
		t.Fatalf("$1 = %v, want 10", got)
	}
}

type stdinBindingTest struct {
	name  string
	lines []string
	ref   string
	want  string
}

var stdinBindingTests = []stdinBindingTest{
	{
		name:  "first line binds $0",
		lines: []string{"42", "$0 + 1"},
		ref:   "$1",
		want:  "43",
	},
	{
		name:  "failed line still takes a number",
		lines: []string{"1/0", "5", "$1 * 2"},
		ref:   "$2",
		want:  "10",
	},
}

func TestSTDINBindsResults(t *testing.T) {
	for _, test := range stdinBindingTests {
		t.Run(test.name, func(t *testing.T) {
			c := &Calculator{DecimalPlaces: 30}

			captureStdout(t, func() {
				for _, line := range test.lines {
					// Some lines fail on purpose. Only the resulting bindings matter.
					_ = c.processLine(line, ModeSTDIN, 0)
				}
			})

			res, err := c.Evaluate(test.ref)
			if err != nil {
				t.Fatalf("Evaluate(%s): %v", test.ref, err)
			}
			got, err := constructive.Text(context.Background(), res.Constructive(), 0, 10)
			if err != nil {
				t.Fatalf("Text(%s): %v", test.ref, err)
			}
			if got != test.want {
				t.Fatalf("%s mismatch: got %s, want %s", test.ref, got, test.want)
			}
		})
	}
}

func TestREPLResultHistoryImmutable(t *testing.T) {
	c := &Calculator{DecimalPlaces: 30}

	if err := c.processLine("7", ModeREPL, 0); err != nil {
		t.Fatalf("processLine(7): %v", err)
	}

	err := c.processLine("$0 = 99", ModeREPL, 0)
	if err == nil {
		t.Fatalf("expected error when assigning to $0")
	}
	if !strings.Contains(err.Error(), "cannot assign to constant") {
		t.Fatalf("error mismatch: got %v, want substring %q", err, "cannot assign to constant")
	}
}

func TestREPLResultHistorySurvivesErrors(t *testing.T) {
	c := &Calculator{DecimalPlaces: 30}

	if err := c.processLine("10", ModeREPL, 0); err != nil {
		t.Fatalf("processLine(10): %v", err)
	}

	if err := c.processLine("1/0", ModeREPL, 0); err == nil {
		t.Fatalf("expected division by zero error")
	}

	if err := c.processLine("20", ModeREPL, 0); err != nil {
		t.Fatalf("processLine(20): %v", err)
	}

	res, err := c.Evaluate("$0")
	if err != nil {
		t.Fatalf("Evaluate($0): %v", err)
	}
	if got := approxFloat(t, res); math.Abs(got-10) > 1e-9 {
		t.Fatalf("$0 = %v, want 10", got)
	}

	if _, err := c.Evaluate("$1"); err == nil {
		t.Fatalf("expected error for $1 after failed line")
	} else if !strings.Contains(err.Error(), "no result for line 1") {
		t.Fatalf("error mismatch: got %v, want substring %q", err, "no result for line 1")
	}

	res, err = c.Evaluate("$2")
	if err != nil {
		t.Fatalf("Evaluate($2): %v", err)
	}
	if got := approxFloat(t, res); math.Abs(got-20) > 1e-9 {
		t.Fatalf("$2 = %v, want 20", got)
	}
}

func TestREPLResultAssignRejected(t *testing.T) {
	c := &Calculator{DecimalPlaces: 30}

	if err := c.processLine("$3 = 1", ModeREPL, 0); err == nil {
		t.Fatalf("expected error assigning to undefined $3")
	} else if !strings.Contains(err.Error(), "cannot assign to constant") {
		t.Fatalf("error mismatch: got %v, want substring %q", err, "cannot assign to constant")
	}

	if _, err := c.Evaluate("$3"); err == nil || !strings.Contains(err.Error(), "no result for line 3") {
		t.Fatalf("Evaluate($3) error = %v, want substring %q", err, "no result for line 3")
	}
}

func TestLoadKeepsResultHistory(t *testing.T) {
	c := &Calculator{DecimalPlaces: 30}

	if err := c.processLine("5", ModeREPL, 0); err != nil {
		t.Fatalf("processLine(5): %v", err)
	}
	if err := c.processLine("x = 9", ModeREPL, 0); err != nil {
		t.Fatalf("processLine(x = 9): %v", err)
	}

	path := filepath.Join(t.TempDir(), "session.calc")
	if err := os.WriteFile(path, []byte("y = 2\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := c.handleLoad([]string{path}); err != nil {
		t.Fatalf("handleLoad: %v", err)
	}

	res, err := c.Evaluate("$0")
	if err != nil {
		t.Fatalf("Evaluate($0) after load: %v", err)
	}
	if got := approxFloat(t, res); math.Abs(got-5) > 1e-9 {
		t.Fatalf("$0 = %v, want 5", got)
	}

	if err := c.processLine("$0 = 1", ModeREPL, 0); err == nil {
		t.Fatalf("expected $0 to stay immutable after load")
	}

	if _, err := c.Evaluate("x"); err == nil {
		t.Fatalf("expected user variable x to be cleared by load")
	}
	if _, err := c.Evaluate("y"); err != nil {
		t.Fatalf("Evaluate(y) from loaded file: %v", err)
	}
}

type positionalBindingTest struct {
	name    string
	args    []string
	wantOut string
	wantErr string
}

var positionalBindingTests = []positionalBindingTest{
	{
		name:    "later argument references earlier",
		args:    []string{"2+3", "$0*2"},
		wantOut: "5\n10\n",
	},
	{
		name:    "reference to a later index fails",
		args:    []string{"$1", "2"},
		wantErr: "no result for line 1",
	},
	{
		name:    "stops at a result that cannot render",
		args:    []string{"1", "√(-4)", "7"},
		wantOut: "1\n",
		wantErr: "formatting result:",
	},
	{
		name:    "stops at the first error",
		args:    []string{"1", "$5", "7"},
		wantOut: "1\n",
		wantErr: "no result for line 5",
	},
}

func TestPositionalArgsBindResults(t *testing.T) {
	for _, test := range positionalBindingTests {
		t.Run(test.name, func(t *testing.T) {
			cmd := NewCommand()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(test.args)

			var err error
			out := captureStdout(t, func() { err = cmd.Execute() })
			if out != test.wantOut {
				t.Fatalf("stdout mismatch: got %q, want %q", out, test.wantOut)
			}

			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error mismatch: got %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

// captureStdout returns what fn wrote to os.Stdout. Results are printed there, not to the
// command's writer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()
	w.Close()

	return <-done
}

type loadCycleTest struct {
	name string

	// files maps a file name to its contents. "{dir}" in the contents expands to the test's
	// temp dir, so a file can .load another by absolute path.
	files    map[string]string
	symlinks map[string]string
	start    string
	wantVars []string

	// wantCount pins how many lines ran, which an unguarded recursion would blow past before
	// running out of file descriptors.
	wantCount int
}

var loadCycleTests = []loadCycleTest{
	{
		name:      "self",
		files:     map[string]string{"a.calc": ".load {dir}/a.calc\nx = 1\n"},
		start:     "a.calc",
		wantVars:  []string{"x"},
		wantCount: 2,
	},
	{
		name: "two files",
		files: map[string]string{
			"a.calc": ".load {dir}/b.calc\nx = 1\n",
			"b.calc": ".load {dir}/a.calc\ny = 2\n",
		},
		start:     "a.calc",
		wantVars:  []string{"x", "y"},
		wantCount: 4,
	},
	{
		name:      "through symlink",
		files:     map[string]string{"a.calc": ".load {dir}/link.calc\nx = 1\n"},
		symlinks:  map[string]string{"link.calc": "a.calc"},
		start:     "a.calc",
		wantVars:  []string{"x"},
		wantCount: 2,
	},
}

func TestLoadStopsCycles(t *testing.T) {
	for _, test := range loadCycleTests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()

			for name, contents := range test.files {
				contents = strings.ReplaceAll(contents, "{dir}", dir)
				if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
					t.Fatalf("WriteFile(%s): %v", name, err)
				}
			}

			for name, target := range test.symlinks {
				if err := os.Symlink(filepath.Join(dir, target), filepath.Join(dir, name)); err != nil {
					t.Fatalf("Symlink(%s): %v", name, err)
				}
			}

			c := &Calculator{DecimalPlaces: 30}
			if err := c.handleLoad([]string{filepath.Join(dir, test.start)}); err != nil {
				t.Fatalf("handleLoad: %v", err)
			}

			if len(c.loading) != 0 {
				t.Fatalf("loading stack not unwound: %v", c.loading)
			}
			if c.count != test.wantCount {
				t.Fatalf("ran %d lines, want %d", c.count, test.wantCount)
			}

			for _, name := range test.wantVars {
				if _, err := c.Evaluate(name); err != nil {
					t.Fatalf("Evaluate(%s): %v", name, err)
				}
			}
		})
	}
}

func TestLoadRecursiveError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.calc")
	link := filepath.Join(dir, "link.calc")

	if err := os.WriteFile(path, []byte("x = 1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(path, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	c := &Calculator{DecimalPlaces: 30}
	c.loading = []loadingFile{{name: path, info: info}}

	err = c.handleLoad([]string{link})
	if !errors.Is(err, ErrRecursiveLoad) {
		t.Fatalf("handleLoad error = %v, want %v", err, ErrRecursiveLoad)
	}

	want := path + " -> " + link
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not name the cycle %q", err, want)
	}

	if len(c.loading) != 1 {
		t.Fatalf("rejected load changed the loading stack: %v", c.loading)
	}
}

func approxFloat(t *testing.T, res *unified.Real) float64 {
	t.Helper()
	cons := res.Constructive()
	text, err := constructive.Text(context.Background(), cons, 30, 10)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	var f float64
	fmt.Sscanf(text, "%f", &f)
	return f
}

type renderFailureTest struct {
	name        string
	lines       []string
	wantErrLine int
	wantCount   int
	wantBound   map[string]string
	wantUnbound []string
}

var renderFailureTests = []renderFailureTest{
	{
		name:        "failed render leaves its number unbound",
		lines:       []string{"1", "√(-4)", "3"},
		wantErrLine: 1,
		wantCount:   3,
		wantBound:   map[string]string{"$0": "1", "$2": "3"},
		wantUnbound: []string{"$1"},
	},
	{
		name:        "failed render on the first line",
		lines:       []string{"√(-4)", "$0"},
		wantErrLine: 0,
		wantCount:   2,
		wantUnbound: []string{"$0", "$1"},
	},
}

func TestProcessLineRenderFailure(t *testing.T) {
	for _, test := range renderFailureTests {
		t.Run(test.name, func(t *testing.T) {
			c := &Calculator{DecimalPlaces: 30}

			var errs []error
			stderr := captureStderr(t, func() {
				captureStdout(t, func() {
					for _, line := range test.lines {
						errs = append(errs, c.processLine(line, ModeREPL, 0))
					}
				})
			})

			if err := errs[test.wantErrLine]; err == nil || !strings.Contains(err.Error(), "formatting result:") {
				t.Fatalf("line %d error = %v, want substring %q", test.wantErrLine, err, "formatting result:")
			}
			if !strings.Contains(stderr, "formatting result:") {
				t.Fatalf("stderr = %q, want substring %q", stderr, "formatting result:")
			}

			if c.count != test.wantCount {
				t.Fatalf("count = %d, want %d", c.count, test.wantCount)
			}
			if len(c.history) != len(test.lines) {
				t.Fatalf("history length = %d, want %d", len(c.history), len(test.lines))
			}

			for ref, want := range test.wantBound {
				res, err := c.Evaluate(ref)
				if err != nil {
					t.Fatalf("Evaluate(%s): %v", ref, err)
				}
				got, err := constructive.Text(context.Background(), res.Constructive(), 0, 10)
				if err != nil {
					t.Fatalf("Text(%s): %v", ref, err)
				}
				if got != want {
					t.Fatalf("%s = %s, want %s", ref, got, want)
				}
			}

			for _, ref := range test.wantUnbound {
				if _, err := c.Evaluate(ref); err == nil || !strings.Contains(err.Error(), "no result for line") {
					t.Fatalf("Evaluate(%s) error = %v, want an unbound result", ref, err)
				}
			}
		})
	}
}

func TestDisplayResultRenderFailure(t *testing.T) {
	c := &Calculator{DecimalPlaces: 30}

	res, err := c.Evaluate("√(-4)")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	var got error
	out := captureStdout(t, func() { got = c.DisplayResult(context.Background(), res) })
	if got == nil || !strings.Contains(got.Error(), "formatting result:") {
		t.Fatalf("DisplayResult error = %v, want substring %q", got, "formatting result:")
	}
	if out != "" {
		t.Fatalf("stdout = %q, want nothing printed", out)
	}
}

func TestDisplayResultApproximationCallback(t *testing.T) {
	c := &Calculator{DecimalPlaces: 30}

	want, err := c.Evaluate("√2")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	inner := want.Constructive()
	calls := 0
	cb := constructive.FromApproximation("callback-sqrt2", func(ctx context.Context, p int) (*big.Int, error) {
		calls++
		return constructive.Approximate(ctx, inner, p)
	})
	got := unified.New(cb, nil)

	wantOut := captureStdout(t, func() {
		if err := c.DisplayResult(context.Background(), want); err != nil {
			t.Fatalf("DisplayResult(sqrt): %v", err)
		}
	})
	gotOut := captureStdout(t, func() {
		if err := c.DisplayResult(context.Background(), got); err != nil {
			t.Fatalf("DisplayResult(callback): %v", err)
		}
	})

	if gotOut != wantOut {
		t.Fatalf("callback rendered %q, want %q", gotOut, wantOut)
	}
	if calls == 0 {
		t.Fatal("callback was never called")
	}
}

func TestShowFunctionsListsBindersGroup(t *testing.T) {
	// Not parallel: it swaps os.Stdout.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	(&Calculator{DecimalPlaces: 30}).showFunctions()
	os.Stdout = old
	w.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "Binders:\n  sum(expr, i = a..b)") {
		t.Errorf("listing lacks a Binders group:\n%s", out)
	}
}

func TestSolveResultSharpens(t *testing.T) {
	t.Parallel()

	c := &Calculator{DecimalPlaces: 20}
	res, err := c.Evaluate("solve(x**2 - 2, x = 0..2)")
	if err != nil {
		t.Fatal(err)
	}

	ctx := parser.WithBudget(context.Background())
	want, err := constructive.Text(ctx, unified.Sqrt2().Constructive(), 90, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, places := range []int{20, 90} {
		got, err := constructive.Text(ctx, res.Constructive(), places, 10)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(want, got[:len(got)-1]) {
			t.Errorf("%d places: got %s, want prefix of %s", places, got, want)
		}
	}
}
