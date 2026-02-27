package cg

import (
	"bytes"
	"strings"
	"testing"
)

type writeLineTest struct {
	name      string
	indicator Indicator
	line      string
	want      string
}

var writeLineTests = []writeLineTest{
	{
		name:      "stdout line",
		indicator: IndicatorOut,
		line:      "hello world",
		want:      "PFX O: hello world\n",
	},
	{
		name:      "stderr line",
		indicator: IndicatorErr,
		line:      "an error",
		want:      "PFX E: an error\n",
	},
	{
		name:      "info line",
		indicator: IndicatorInfo,
		line:      "cg v1.0",
		want:      "PFX I: cg v1.0\n",
	},
	{
		name:      "empty line",
		indicator: IndicatorOut,
		line:      "",
		want:      "PFX O: \n",
	},
}

func TestWriteLine(t *testing.T) {
	t.Parallel()

	for _, tt := range writeLineTests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewAnnotatedWriter(&buf, func() string { return "PFX " })

			if err := w.WriteLine(tt.indicator, tt.line); err != nil {
				t.Fatalf("WriteLine() error = %v", err)
			}

			if got := buf.String(); got != tt.want {
				t.Errorf("WriteLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

type writeLinesTest struct {
	name string
	// indicator is the stream indicator to use.
	indicator Indicator
	// input is the data to feed to WriteLines.
	input string
	// want is the expected annotated output.
	want string
}

var writeLinesTests = []writeLinesTest{
	{
		name:      "single line with newline",
		indicator: IndicatorOut,
		input:     "hello\n",
		want:      "T O: hello\n",
	},
	{
		name:      "multiple lines",
		indicator: IndicatorOut,
		input:     "line1\nline2\nline3\n",
		want:      "T O: line1\nT O: line2\nT O: line3\n",
	},
	{
		name:      "partial final line",
		indicator: IndicatorOut,
		input:     "hello\npartial",
		want:      "T O: hello\nT O: partial",
	},
	{
		name:      "single partial line no newline",
		indicator: IndicatorErr,
		input:     "no newline",
		want:      "T E: no newline",
	},
	{
		name:      "empty input",
		indicator: IndicatorOut,
		input:     "",
		want:      "",
	},
	{
		name:      "stderr multi-line",
		indicator: IndicatorErr,
		input:     "err1\nerr2\n",
		want:      "T E: err1\nT E: err2\n",
	},
	{
		name:      "only newline",
		indicator: IndicatorOut,
		input:     "\n",
		want:      "T O: \n",
	},
}

type writeLineWithPrefixTest struct {
	name      string
	prefix    string
	indicator Indicator
	line      string
	want      string
}

var writeLineWithPrefixTests = []writeLineWithPrefixTest{
	{
		name:      "stdout line with custom prefix",
		prefix:    "12:00:00 ",
		indicator: IndicatorOut,
		line:      "hello world",
		want:      "12:00:00 O: hello world\n",
	},
	{
		name:      "stderr line with custom prefix",
		prefix:    "12:00:01 ",
		indicator: IndicatorErr,
		line:      "an error",
		want:      "12:00:01 E: an error\n",
	},
	{
		name:      "info line with custom prefix",
		prefix:    "12:00:02 ",
		indicator: IndicatorInfo,
		line:      "lifecycle",
		want:      "12:00:02 I: lifecycle\n",
	},
	{
		name:      "empty line with custom prefix",
		prefix:    "T ",
		indicator: IndicatorOut,
		line:      "",
		want:      "T O: \n",
	},
}

func TestWriteLineWithPrefix(t *testing.T) {
	t.Parallel()

	for _, tt := range writeLineWithPrefixTests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewAnnotatedWriter(&buf, func() string { return "UNUSED " })

			if err := w.WriteLineWithPrefix(tt.prefix, tt.indicator, tt.line); err != nil {
				t.Fatalf("WriteLineWithPrefix() error = %v", err)
			}

			if got := buf.String(); got != tt.want {
				t.Errorf("WriteLineWithPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

type writePartialLineWithPrefixTest struct {
	name      string
	prefix    string
	indicator Indicator
	line      string
	want      string
}

var writePartialLineWithPrefixTests = []writePartialLineWithPrefixTest{
	{
		name:      "stdout partial with custom prefix",
		prefix:    "12:00:00 ",
		indicator: IndicatorOut,
		line:      "partial",
		want:      "12:00:00 O: partial",
	},
	{
		name:      "stderr partial with custom prefix",
		prefix:    "12:00:01 ",
		indicator: IndicatorErr,
		line:      "err partial",
		want:      "12:00:01 E: err partial",
	},
}

func TestWritePartialLineWithPrefix(t *testing.T) {
	t.Parallel()

	for _, tt := range writePartialLineWithPrefixTests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewAnnotatedWriter(&buf, func() string { return "UNUSED " })

			if err := w.WritePartialLineWithPrefix(tt.prefix, tt.indicator, tt.line); err != nil {
				t.Fatalf("WritePartialLineWithPrefix() error = %v", err)
			}

			if got := buf.String(); got != tt.want {
				t.Errorf("WritePartialLineWithPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteLines(t *testing.T) {
	t.Parallel()

	for _, tt := range writeLinesTests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewAnnotatedWriter(&buf, func() string { return "T " })

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
