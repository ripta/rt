package cg

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type stringifyTest struct {
	name string
	val  any
	want string
}

var stringifyTests = []stringifyTest{
	{
		name: "string value",
		val:  "hello",
		want: "hello",
	},
	{
		name: "float64 integer",
		val:  float64(42),
		want: "42",
	},
	{
		name: "float64 fraction",
		val:  float64(3.14),
		want: "3.14",
	},
	{
		name: "bool true",
		val:  true,
		want: "true",
	},
	{
		name: "bool false",
		val:  false,
		want: "false",
	},
	{
		name: "nil",
		val:  nil,
		want: "null",
	},
	{
		name: "slice",
		val:  []any{"a", "b"},
		want: `["a","b"]`,
	},
	{
		name: "map",
		val:  map[string]any{"k": "v"},
		want: `{"k":"v"}`,
	},
}

func TestStringify(t *testing.T) {
	t.Parallel()

	for _, tt := range stringifyTests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringify(tt.val)
			if got != tt.want {
				t.Errorf("stringify(%v) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

type parseFieldsFlagTest struct {
	name  string
	input string
	want  []string
}

var parseFieldsFlagTests = []parseFieldsFlagTest{
	{
		name:  "empty string",
		input: "",
		want:  nil,
	},
	{
		name:  "single field",
		input: "level",
		want:  []string{"level"},
	},
	{
		name:  "multiple fields",
		input: "level,host,pid",
		want:  []string{"level", "host", "pid"},
	},
	{
		name:  "wildcard",
		input: "*",
		want:  []string{"*"},
	},
}

func TestParseFieldsFlag(t *testing.T) {
	t.Parallel()

	for _, tt := range parseFieldsFlagTests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFieldsFlag(tt.input)
			if tt.want == nil {
				if got != nil {
					t.Errorf("parseFieldsFlag(%q) = %v, want nil", tt.input, got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseFieldsFlag(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseFieldsFlag(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

type jsonProcessorTest struct {
	name string
	opts JSONProcessorOptions
	line string
	// want is the expected result, nil means pass through
	want *ProcessedLine
}

var jsonProcessorTests = []jsonProcessorTest{
	{
		name: "valid JSON with message",
		opts: JSONProcessorOptions{MessageKey: "message"},
		line: `{"message":"hello world","level":"info"}`,
		want: &ProcessedLine{Line: "hello world"},
	},
	{
		name: "missing message key",
		opts: JSONProcessorOptions{MessageKey: "message"},
		line: `{"msg":"hello world","level":"info"}`,
		want: nil,
	},
	{
		name: "non-JSON line",
		opts: JSONProcessorOptions{MessageKey: "message"},
		line: "this is not json",
		want: nil,
	},
	{
		name: "non-string message",
		opts: JSONProcessorOptions{MessageKey: "message"},
		line: `{"message":42,"level":"info"}`,
		want: &ProcessedLine{Line: "42"},
	},
	{
		name: "boolean message",
		opts: JSONProcessorOptions{MessageKey: "message"},
		line: `{"message":true}`,
		want: &ProcessedLine{Line: "true"},
	},
	{
		name: "null message",
		opts: JSONProcessorOptions{MessageKey: "message"},
		line: `{"message":null}`,
		want: &ProcessedLine{Line: "null"},
	},
	{
		name: "custom message key",
		opts: JSONProcessorOptions{MessageKey: "msg"},
		line: `{"msg":"custom key"}`,
		want: &ProcessedLine{Line: "custom key"},
	},
	{
		name: "empty JSON object",
		opts: JSONProcessorOptions{MessageKey: "message"},
		line: `{}`,
		want: nil,
	},
	{
		name: "with fields",
		opts: JSONProcessorOptions{
			MessageKey: "message",
			Fields:     []string{"level"},
		},
		line: `{"message":"hello","level":"info"}`,
		want: &ProcessedLine{Line: "hello level=info"},
	},
	{
		name: "with wildcard fields",
		opts: JSONProcessorOptions{
			MessageKey: "message",
			Fields:     []string{"*"},
		},
		line: `{"message":"hello","level":"info","host":"srv1"}`,
		want: &ProcessedLine{Line: "hello host=srv1 level=info"},
	},
	{
		name: "with timestamp",
		opts: JSONProcessorOptions{
			MessageKey:   "message",
			TimestampKey: "timestamp",
			Format:       "15:04:05 ",
		},
		line: `{"message":"hello","timestamp":"2024-01-15T10:30:00Z"}`,
		want: &ProcessedLine{Prefix: "10:30:00 ", Line: "hello"},
	},
	{
		name: "with timestamp and fields",
		opts: JSONProcessorOptions{
			MessageKey:   "message",
			TimestampKey: "ts",
			Format:       "15:04:05 ",
			Fields:       []string{"level"},
		},
		line: `{"message":"hello","ts":"2024-01-15T10:30:00Z","level":"warn"}`,
		want: &ProcessedLine{Prefix: "10:30:00 ", Line: "hello level=warn"},
	},
}

func TestNewJSONProcessor(t *testing.T) {
	t.Parallel()

	for _, tt := range jsonProcessorTests {
		t.Run(tt.name, func(t *testing.T) {
			proc := NewJSONProcessor(tt.opts)
			got := proc(tt.line)

			if tt.want == nil {
				if got != nil {
					t.Errorf("processor returned %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("processor returned nil, want %+v", tt.want)
			}
			if got.Line != tt.want.Line {
				t.Errorf("Line = %q, want %q", got.Line, tt.want.Line)
			}
			if got.Prefix != tt.want.Prefix {
				t.Errorf("Prefix = %q, want %q", got.Prefix, tt.want.Prefix)
			}
		})
	}
}

type formatFieldsTest struct {
	name        string
	obj         map[string]any
	fields      []string
	excludeKeys []string
	want        string
}

var formatFieldsTests = []formatFieldsTest{
	{
		name:   "single field",
		obj:    map[string]any{"level": "info", "message": "hi"},
		fields: []string{"level"},
		want:   " level=info",
	},
	{
		name:   "multiple fields in order",
		obj:    map[string]any{"level": "info", "host": "srv1", "message": "hi"},
		fields: []string{"host", "level"},
		want:   " host=srv1 level=info",
	},
	{
		name:   "missing field ignored",
		obj:    map[string]any{"level": "info", "message": "hi"},
		fields: []string{"level", "missing"},
		want:   " level=info",
	},
	{
		name:   "wildcard all fields",
		obj:    map[string]any{"level": "info", "host": "srv1"},
		fields: []string{"*"},
		want:   " host=srv1 level=info",
	},
	{
		name:        "wildcard excludes keys",
		obj:         map[string]any{"level": "info", "message": "hi", "timestamp": "now"},
		fields:      []string{"*"},
		excludeKeys: []string{"message", "timestamp"},
		want:        " level=info",
	},
	{
		name:   "value with spaces is quoted",
		obj:    map[string]any{"msg": "hello world"},
		fields: []string{"msg"},
		want:   ` msg="hello world"`,
	},
	{
		name:   "numeric value",
		obj:    map[string]any{"pid": float64(1234)},
		fields: []string{"pid"},
		want:   " pid=1234",
	},
	{
		name:   "boolean value",
		obj:    map[string]any{"debug": true},
		fields: []string{"debug"},
		want:   " debug=true",
	},
	{
		name:   "empty fields",
		obj:    map[string]any{"level": "info"},
		fields: []string{},
		want:   "",
	},
	{
		name:        "excluded field not shown in explicit list",
		obj:         map[string]any{"level": "info", "message": "hi"},
		fields:      []string{"level", "message"},
		excludeKeys: []string{"message"},
		want:        " level=info",
	},
}

func TestFormatFields(t *testing.T) {
	t.Parallel()

	for _, tt := range formatFieldsTests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFields(tt.obj, tt.fields, tt.excludeKeys...)
			if got != tt.want {
				t.Errorf("formatFields() = %q, want %q", got, tt.want)
			}
		})
	}
}

type writeLinesProcessorTest struct {
	name      string
	proc      LineProcessor
	indicator Indicator
	input     string
	want      string
}

var writeLinesProcessorTests = []writeLinesProcessorTest{
	{
		name: "JSON lines processed",
		proc: NewJSONProcessor(JSONProcessorOptions{MessageKey: "message"}),
		indicator: IndicatorOut,
		input:     "{\"message\":\"hello\"}\n{\"message\":\"world\"}\n",
		want:      "T O: hello\nT O: world\n",
	},
	{
		name: "non-JSON passes through",
		proc: NewJSONProcessor(JSONProcessorOptions{MessageKey: "message"}),
		indicator: IndicatorOut,
		input:     "plain text\n",
		want:      "T O: plain text\n",
	},
	{
		name: "mixed JSON and non-JSON",
		proc: NewJSONProcessor(JSONProcessorOptions{MessageKey: "message"}),
		indicator: IndicatorOut,
		input:     "{\"message\":\"parsed\"}\nnot json\n",
		want:      "T O: parsed\nT O: not json\n",
	},
}

func TestWriteLinesWithProcessor(t *testing.T) {
	t.Parallel()

	for _, tt := range writeLinesProcessorTests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewAnnotatedWriter(&buf, func() string { return "T " })
			w.SetProcessor(tt.proc)

			err := w.WriteLines(strings.NewReader(tt.input), tt.indicator)
			if err != nil {
				t.Fatalf("WriteLines() error = %v", err)
			}

			if got := buf.String(); got != tt.want {
				t.Errorf("WriteLines() = %q, want %q", got, tt.want)
			}
		})
	}
}

type bufferProcessorTest struct {
	name      string
	proc      LineProcessor
	indicator Indicator
	input     string
	wantLines []bufferedLine
}

var bufferProcessorTests = []bufferProcessorTest{
	{
		name:      "JSON lines processed in buffer",
		proc:      NewJSONProcessor(JSONProcessorOptions{MessageKey: "message"}),
		indicator: IndicatorOut,
		input:     "{\"message\":\"hello\"}\n{\"message\":\"world\"}\n",
		wantLines: []bufferedLine{
			{prefix: "P ", line: "hello"},
			{prefix: "P ", line: "world"},
		},
	},
	{
		name:      "non-JSON passes through in buffer",
		proc:      NewJSONProcessor(JSONProcessorOptions{MessageKey: "message"}),
		indicator: IndicatorOut,
		input:     "plain text\n",
		wantLines: []bufferedLine{
			{prefix: "P ", line: "plain text"},
		},
	},
}

func TestLineBufferWriteLinesWithProcessor(t *testing.T) {
	t.Parallel()

	for _, tt := range bufferProcessorTests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewLineBuffer(func() string { return "P " })
			buf.SetProcessor(tt.proc)

			err := buf.WriteLines(strings.NewReader(tt.input), tt.indicator)
			if err != nil {
				t.Fatalf("WriteLines() error = %v", err)
			}

			got := buf.streams[tt.indicator]
			if len(got) != len(tt.wantLines) {
				t.Fatalf("stored %d lines, want %d", len(got), len(tt.wantLines))
			}

			for i, want := range tt.wantLines {
				if got[i].prefix != want.prefix {
					t.Errorf("line[%d].prefix = %q, want %q", i, got[i].prefix, want.prefix)
				}
				if got[i].line != want.line {
					t.Errorf("line[%d].line = %q, want %q", i, got[i].line, want.line)
				}
			}
		})
	}
}

func TestCommandLogParseJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand(
		"--format", "T ",
		"--log-parse", "json",
		"--",
		"sh", "-c", `echo '{"message":"hello","level":"info"}'`,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "O: hello\n") {
		t.Errorf("output missing parsed message, got: %q", out)
	}
	// The output line should be just the message, not the full JSON
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "O: ") && strings.Contains(line, `"level"`) {
			t.Errorf("output line should not contain raw JSON: %q", line)
		}
	}
}

func TestCommandLogParseNonJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand(
		"--format", "T ",
		"--log-parse", "json",
		"--",
		"echo", "plain text",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "O: plain text") {
		t.Errorf("non-JSON should pass through, got: %q", out)
	}
}

func TestCommandLogFieldsWithoutParse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, err := runCgCommand(
		"--format", "T ",
		"--log-fields", "level",
		"--",
		"echo", "hi",
	)
	if err == nil {
		t.Fatal("expected error when --log-fields used without --log-parse")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != 2 {
		t.Errorf("exit code = %d, want 2", exitErr.Code)
	}
}

func TestCommandLogParseInvalid(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, err := runCgCommand(
		"--format", "T ",
		"--log-parse", "xml",
		"--",
		"echo", "hi",
	)
	if err == nil {
		t.Fatal("expected error for unsupported --log-parse value")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != 2 {
		t.Errorf("exit code = %d, want 2", exitErr.Code)
	}
}

func TestCommandLogParseWithFields(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand(
		"--format", "T ",
		"--log-parse", "json",
		"--log-fields", "level",
		"--",
		"sh", "-c", `echo '{"message":"hello","level":"info"}'`,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "O: hello level=info") {
		t.Errorf("output missing parsed message with fields, got: %q", out)
	}
}

func TestCommandLogParseBuffered(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand(
		"--format", "T ",
		"--log-parse", "json",
		"--buffered",
		"--",
		"sh", "-c", `echo '{"message":"buffered msg","level":"info"}'`,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "O: buffered msg") {
		t.Errorf("output missing parsed message in buffered mode, got: %q", out)
	}
	if !strings.Contains(out, "I: --- stdout ---") {
		t.Errorf("missing stdout header in buffered mode, got: %q", out)
	}
}
