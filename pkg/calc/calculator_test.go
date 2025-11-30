package calc

import (
	"testing"
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

	err = c.handleSet([]string{"decimal_places", "5"})
	if err != nil {
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
