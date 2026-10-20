package calc

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/unified"
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

func TestSTDINDoesNotStoreHistory(t *testing.T) {
	c := &Calculator{DecimalPlaces: 30}

	if err := c.processLine("42", ModeSTDIN, 0); err != nil {
		t.Fatalf("processLine: %v", err)
	}

	_, err := c.Evaluate("$0")
	if err == nil {
		t.Fatalf("expected error for $0 in STDIN mode")
	}
	if !strings.Contains(err.Error(), "no result for line 0") {
		t.Fatalf("error mismatch: got %v, want substring %q", err, "no result for line 0")
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

func TestPositionalArgsDoNotStoreHistory(t *testing.T) {
	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"2 + 3", "$0"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected $0 to be undefined after a positional expression")
	}
	if !strings.Contains(err.Error(), "no result for line 0") {
		t.Fatalf("error mismatch: got %v, want substring %q", err, "no result for line 0")
	}
}

func approxFloat(t *testing.T, res *unified.Real) float64 {
	t.Helper()
	cons := res.Constructive()
	text := constructive.Text(cons, 30, 10)
	var f float64
	fmt.Sscanf(text, "%f", &f)
	return f
}
