package cg

import (
	"errors"
	"strings"
	"testing"
)

type parseLogfmtTest struct {
	name string
	line string
	want map[string]any
}

var parseLogfmtTests = []parseLogfmtTest{
	{
		name: "simple pairs",
		line: `level=info message=hello`,
		want: map[string]any{"level": "info", "message": "hello"},
	},
	{
		name: "quoted value",
		line: `message="hello world" level=info`,
		want: map[string]any{"message": "hello world", "level": "info"},
	},
	{
		name: "escaped quote in value",
		line: `message="say \"hi\"" level=info`,
		want: map[string]any{"message": `say "hi"`, "level": "info"},
	},
	{
		name: "escaped backslash in value",
		line: `path="C:\\Users\\foo" level=info`,
		want: map[string]any{"path": `C:\Users\foo`, "level": "info"},
	},
	{
		name: "empty value",
		line: `key= other=val`,
		want: map[string]any{"key": "", "other": "val"},
	},
	{
		name: "bare key",
		line: `debug level=info`,
		want: map[string]any{"debug": true, "level": "info"},
	},
	{
		name: "empty line",
		line: "",
		want: nil,
	},
	{
		name: "whitespace only",
		line: "   ",
		want: nil,
	},
	{
		name: "mixed quoting",
		line: `level=info message="hello world" host=srv1`,
		want: map[string]any{"level": "info", "message": "hello world", "host": "srv1"},
	},
	{
		name: "unicode value",
		line: `message="こんにちは" level=info`,
		want: map[string]any{"message": "こんにちは", "level": "info"},
	},
	{
		name: "trailing whitespace",
		line: `level=info message=hello   `,
		want: map[string]any{"level": "info", "message": "hello"},
	},
	{
		name: "leading whitespace",
		line: `   level=info message=hello`,
		want: map[string]any{"level": "info", "message": "hello"},
	},
	{
		name: "multiple spaces between pairs",
		line: `level=info    message=hello    host=srv1`,
		want: map[string]any{"level": "info", "message": "hello", "host": "srv1"},
	},
	{
		name: "empty quoted value",
		line: `message="" level=info`,
		want: map[string]any{"message": "", "level": "info"},
	},
	{
		name: "bare key at end",
		line: `level=info debug`,
		want: map[string]any{"level": "info", "debug": true},
	},
	{
		name: "multiple bare keys",
		line: `debug verbose`,
		want: map[string]any{"debug": true, "verbose": true},
	},
}

func TestParseLogfmt(t *testing.T) {
	t.Parallel()

	for _, tt := range parseLogfmtTests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLogfmt(tt.line)
			if tt.want == nil {
				if got != nil {
					t.Errorf("parseLogfmt(%q) = %v, want nil", tt.line, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("parseLogfmt(%q) = nil, want %v", tt.line, tt.want)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseLogfmt(%q) has %d keys, want %d: got=%v", tt.line, len(got), len(tt.want), got)
			}
			for k, wantV := range tt.want {
				gotV, ok := got[k]
				if !ok {
					t.Errorf("parseLogfmt(%q) missing key %q", tt.line, k)
					continue
				}
				if gotV != wantV {
					t.Errorf("parseLogfmt(%q)[%q] = %v (%T), want %v (%T)", tt.line, k, gotV, gotV, wantV, wantV)
				}
			}
		})
	}
}

type logfmtProcessorTest struct {
	name string
	opts LogfmtProcessorOptions
	line string
	want *ProcessedLine
}

var logfmtProcessorTests = []logfmtProcessorTest{
	{
		name: "message extraction",
		opts: LogfmtProcessorOptions{MessageKey: "message"},
		line: `message=hello level=info`,
		want: &ProcessedLine{Line: "hello"},
	},
	{
		name: "quoted message extraction",
		opts: LogfmtProcessorOptions{MessageKey: "message"},
		line: `message="hello world" level=info`,
		want: &ProcessedLine{Line: "hello world"},
	},
	{
		name: "missing message key",
		opts: LogfmtProcessorOptions{MessageKey: "message"},
		line: `msg=hello level=info`,
		want: nil,
	},
	{
		name: "non-logfmt passthrough",
		opts: LogfmtProcessorOptions{MessageKey: "message"},
		line: "this is just plain text without any key=value pairs... or is it?",
		want: nil,
	},
	{
		name: "custom message key",
		opts: LogfmtProcessorOptions{MessageKey: "msg"},
		line: `msg="custom key"`,
		want: &ProcessedLine{Line: "custom key"},
	},
	{
		name: "field selection",
		opts: LogfmtProcessorOptions{
			MessageKey: "message",
			Fields:     []string{"level"},
		},
		line: `message=hello level=info host=srv1`,
		want: &ProcessedLine{Line: "hello level=info"},
	},
	{
		name: "wildcard fields",
		opts: LogfmtProcessorOptions{
			MessageKey: "message",
			Fields:     []string{"*"},
		},
		line: `message=hello level=info host=srv1`,
		want: &ProcessedLine{Line: "hello host=srv1 level=info"},
	},
	{
		name: "timestamp extraction",
		opts: LogfmtProcessorOptions{
			MessageKey:   "message",
			TimestampKey: "timestamp",
			Format:       "15:04:05 ",
		},
		line: `message=hello timestamp=2024-01-15T10:30:00Z`,
		want: &ProcessedLine{Prefix: "10:30:00 ", Line: "hello"},
	},
	{
		name: "timestamp with fields",
		opts: LogfmtProcessorOptions{
			MessageKey:   "message",
			TimestampKey: "ts",
			Format:       "15:04:05 ",
			Fields:       []string{"level"},
		},
		line: `message=hello ts=2024-01-15T10:30:00Z level=warn`,
		want: &ProcessedLine{Prefix: "10:30:00 ", Line: "hello level=warn"},
	},
	{
		name: "empty line passthrough",
		opts: LogfmtProcessorOptions{MessageKey: "message"},
		line: "",
		want: nil,
	},
}

func TestNewLogfmtProcessor(t *testing.T) {
	t.Parallel()

	for _, tt := range logfmtProcessorTests {
		t.Run(tt.name, func(t *testing.T) {
			proc := NewLogfmtProcessor(tt.opts)
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

func TestCommandLogParseLogfmt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand(
		"--format", "T ",
		"--log-parse", "logfmt",
		"--",
		"sh", "-c", `echo 'message=hello level=info'`,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "O: hello\n") {
		t.Errorf("output missing parsed message, got: %q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "O: ") && strings.Contains(line, "level=info") && !strings.Contains(line, "hello") {
			t.Errorf("output line should not contain raw logfmt: %q", line)
		}
	}
}

func TestCommandLogParseLogfmtWithFields(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand(
		"--format", "T ",
		"--log-parse", "logfmt",
		"--log-fields", "level",
		"--",
		"sh", "-c", `echo 'message=hello level=info'`,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "O: hello level=info") {
		t.Errorf("output missing parsed message with fields, got: %q", out)
	}
}

func TestCommandLogParseLogfmtBuffered(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand(
		"--format", "T ",
		"--log-parse", "logfmt",
		"--buffered",
		"--",
		"sh", "-c", `echo 'message="buffered msg" level=info'`,
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

func TestCommandLogParseLogfmtInvalidValue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, err := runCgCommand(
		"--format", "T ",
		"--log-parse", "yaml",
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
