package parser

import (
	"bytes"
	"strings"
	"testing"
)

type commentTraceTest struct {
	name            string
	input           string
	traceEnabled    bool
	wantResult      float64
	wantTraceOutput []string
}

var commentTraceTests = []commentTraceTest{
	{
		name:            "trace enabled with multiple comments",
		input:           `"first" 3 + "second" 4`,
		traceEnabled:    true,
		wantResult:      7,
		wantTraceOutput: []string{"# first", "# second"},
	},
	{
		name:            "trace disabled",
		input:           `"note" 3 + 4`,
		traceEnabled:    false,
		wantResult:      7,
		wantTraceOutput: nil, // expect empty output
	},
	{
		name:            "nested comments",
		input:           `"outer" "inner" 5`,
		traceEnabled:    true,
		wantResult:      5,
		wantTraceOutput: []string{"# outer", "# inner"},
	},
	{
		name:            "unicode comment",
		input:           `"コメント" 42 "논평"`,
		traceEnabled:    true,
		wantResult:      42,
		wantTraceOutput: []string{"# コメント", "# 논평"},
	},
	{
		name:            "empty comment",
		input:           `"" 10`,
		traceEnabled:    true,
		wantResult:      10,
		wantTraceOutput: []string{"# "},
	},
	{
		name:            "raw string comment",
		input:           "`backtick comment` 20",
		traceEnabled:    true,
		wantResult:      20,
		wantTraceOutput: []string{"# backtick comment"},
	},
	{
		name:            "comment with special characters",
		input:           `"hello world! @#$%" 15`,
		traceEnabled:    true,
		wantResult:      15,
		wantTraceOutput: []string{"# hello world! @#$%"},
	},
}

func TestCommentTrace(t *testing.T) {
	t.Parallel()

	for _, tt := range commentTraceTests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			env := NewEnv()
			env.SetTrace(tt.traceEnabled)
			env.SetTraceOutput(&buf)

			node, err := Parse("test", tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			result, err := node.Eval(env)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}

			output := buf.String()
			if tt.wantTraceOutput == nil {
				if output != "" {
					t.Errorf("expected no trace output, got: %q", output)
				}
			} else {
				for _, want := range tt.wantTraceOutput {
					if !strings.Contains(output, want) {
						t.Errorf("missing %q in trace output: %q", want, output)
					}
				}
			}

			got := realToFloat(t, result)
			if got != tt.wantResult {
				t.Errorf("result mismatch: got %v, want %v", got, tt.wantResult)
			}
		})
	}
}
