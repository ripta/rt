package cg

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type writeLinesBufferTest struct {
	name      string
	indicator Indicator
	input     string
	wantCount int
	wantLines []bufferedLine
}

var writeLinesBufferTests = []writeLinesBufferTest{
	{
		name:      "single line",
		indicator: IndicatorOut,
		input:     "hello\n",
		wantCount: 1,
		wantLines: []bufferedLine{
			{prefix: "P ", line: "hello"},
		},
	},
	{
		name:      "multiple lines",
		indicator: IndicatorOut,
		input:     "line1\nline2\nline3\n",
		wantCount: 3,
		wantLines: []bufferedLine{
			{prefix: "P ", line: "line1"},
			{prefix: "P ", line: "line2"},
			{prefix: "P ", line: "line3"},
		},
	},
	{
		name:      "partial final line",
		indicator: IndicatorErr,
		input:     "hello\npartial",
		wantCount: 2,
		wantLines: []bufferedLine{
			{prefix: "P ", line: "hello"},
			{prefix: "P ", line: "partial", partial: true},
		},
	},
	{
		name:      "empty input",
		indicator: IndicatorOut,
		input:     "",
		wantCount: 0,
	},
}

func TestLineBufferWriteLines(t *testing.T) {
	t.Parallel()

	for _, tt := range writeLinesBufferTests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewLineBuffer(func() string { return "P " })

			err := buf.WriteLines(strings.NewReader(tt.input), tt.indicator)
			if err != nil {
				t.Fatalf("WriteLines() error = %v", err)
			}

			got := buf.streams[tt.indicator]
			if len(got) != tt.wantCount {
				t.Fatalf("WriteLines() stored %d lines, want %d", len(got), tt.wantCount)
			}

			for i, want := range tt.wantLines {
				if got[i].prefix != want.prefix {
					t.Errorf("line[%d].prefix = %q, want %q", i, got[i].prefix, want.prefix)
				}
				if got[i].line != want.line {
					t.Errorf("line[%d].line = %q, want %q", i, got[i].line, want.line)
				}
				if got[i].partial != want.partial {
					t.Errorf("line[%d].partial = %v, want %v", i, got[i].partial, want.partial)
				}
			}
		})
	}
}

type flushTest struct {
	name    string
	streams map[Indicator][]bufferedLine
	want    string
}

var flushTests = []flushTest{
	{
		name: "stdout only",
		streams: map[Indicator][]bufferedLine{
			IndicatorOut: {
				{prefix: "T1 ", line: "out1"},
				{prefix: "T2 ", line: "out2"},
			},
		},
		want: "F I: --- stdout ---\nT1 O: out1\nT2 O: out2\n",
	},
	{
		name: "stderr only",
		streams: map[Indicator][]bufferedLine{
			IndicatorErr: {
				{prefix: "T1 ", line: "err1"},
			},
		},
		want: "F I: --- stderr ---\nT1 E: err1\n",
	},
	{
		name: "both streams",
		streams: map[Indicator][]bufferedLine{
			IndicatorOut: {
				{prefix: "T1 ", line: "out1"},
			},
			IndicatorErr: {
				{prefix: "T2 ", line: "err1"},
			},
		},
		want: "F I: --- stdout ---\nT1 O: out1\nF I: --- stderr ---\nT2 E: err1\n",
	},
	{
		name:    "empty streams",
		streams: map[Indicator][]bufferedLine{},
		want:    "",
	},
	{
		name: "partial final line",
		streams: map[Indicator][]bufferedLine{
			IndicatorOut: {
				{prefix: "T1 ", line: "full"},
				{prefix: "T2 ", line: "partial", partial: true},
			},
		},
		want: "F I: --- stdout ---\nT1 O: full\nT2 O: partial",
	},
	{
		name: "prefix preservation",
		streams: map[Indicator][]bufferedLine{
			IndicatorOut: {
				{prefix: "00:00:01 ", line: "first"},
				{prefix: "00:00:02 ", line: "second"},
				{prefix: "00:00:03 ", line: "third"},
			},
		},
		want: "F I: --- stdout ---\n00:00:01 O: first\n00:00:02 O: second\n00:00:03 O: third\n",
	},
}

func TestLineBufferFlush(t *testing.T) {
	t.Parallel()

	for _, tt := range flushTests {
		t.Run(tt.name, func(t *testing.T) {
			lb := &LineBuffer{
				streams: tt.streams,
			}

			var out bytes.Buffer
			w := NewAnnotatedWriter(&out, func() string { return "F " })

			if err := lb.Flush(w); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}

			if got := out.String(); got != tt.want {
				t.Errorf("Flush() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLineBufferConcurrentWrite(t *testing.T) {
	t.Parallel()

	buf := NewLineBuffer(func() string { return "P " })

	const linesPerWriter = 100
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		var sb strings.Builder
		for i := 0; i < linesPerWriter; i++ {
			fmt.Fprintf(&sb, "stdout-%d\n", i)
		}
		_ = buf.WriteLines(strings.NewReader(sb.String()), IndicatorOut)
	}()

	go func() {
		defer wg.Done()
		var sb strings.Builder
		for i := 0; i < linesPerWriter; i++ {
			fmt.Fprintf(&sb, "stderr-%d\n", i)
		}
		_ = buf.WriteLines(strings.NewReader(sb.String()), IndicatorErr)
	}()

	wg.Wait()

	if got := len(buf.streams[IndicatorOut]); got != linesPerWriter {
		t.Errorf("stdout line count = %d, want %d", got, linesPerWriter)
	}
	if got := len(buf.streams[IndicatorErr]); got != linesPerWriter {
		t.Errorf("stderr line count = %d, want %d", got, linesPerWriter)
	}
}

func TestCommandBufferedGroupsOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--format", "T ", "--buffered", "--", "sh", "-c", "echo out; echo err >&2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	// Find section headers and content
	stdoutHeaderIdx := -1
	stderrHeaderIdx := -1
	stdoutLineIdx := -1
	stderrLineIdx := -1

	for i, line := range lines {
		switch {
		case strings.Contains(line, "I: --- stdout ---"):
			stdoutHeaderIdx = i
		case strings.Contains(line, "I: --- stderr ---"):
			stderrHeaderIdx = i
		case strings.Contains(line, "O: out"):
			stdoutLineIdx = i
		case strings.Contains(line, "E: err"):
			stderrLineIdx = i
		}
	}

	if stdoutHeaderIdx == -1 {
		t.Fatalf("missing stdout header in output: %q", out)
	}
	if stderrHeaderIdx == -1 {
		t.Fatalf("missing stderr header in output: %q", out)
	}
	if stdoutLineIdx == -1 {
		t.Fatalf("missing stdout content in output: %q", out)
	}
	if stderrLineIdx == -1 {
		t.Fatalf("missing stderr content in output: %q", out)
	}

	// Stdout section comes before stderr section
	if stdoutHeaderIdx >= stderrHeaderIdx {
		t.Errorf("stdout header (%d) should come before stderr header (%d)", stdoutHeaderIdx, stderrHeaderIdx)
	}
	// Stdout content follows stdout header
	if stdoutLineIdx <= stdoutHeaderIdx {
		t.Errorf("stdout content (%d) should follow stdout header (%d)", stdoutLineIdx, stdoutHeaderIdx)
	}
	// Stderr content follows stderr header
	if stderrLineIdx <= stderrHeaderIdx {
		t.Errorf("stderr content (%d) should follow stderr header (%d)", stderrLineIdx, stderrHeaderIdx)
	}

	// Buffered mode notice present
	if !strings.Contains(out, "I: buffered mode, output deferred") {
		t.Errorf("missing buffered mode notice in output: %q", out)
	}
}

func TestCommandBufferedEmptyStream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--format", "T ", "--buffered", "--", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "I: --- stdout ---") {
		t.Errorf("missing stdout header in output: %q", out)
	}
	if strings.Contains(out, "I: --- stderr ---") {
		t.Errorf("unexpected stderr header in output: %q", out)
	}
	if !strings.Contains(out, "O: hello") {
		t.Errorf("missing stdout content in output: %q", out)
	}
}

func TestCommandBufferedNoOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--format", "T ", "--buffered", "--", "true")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(out, "--- stdout ---") {
		t.Errorf("unexpected stdout header for silent command: %q", out)
	}
	if strings.Contains(out, "--- stderr ---") {
		t.Errorf("unexpected stderr header for silent command: %q", out)
	}
	if !strings.Contains(out, "Finished with exitcode 0") {
		t.Errorf("missing finish message: %q", out)
	}
}

func TestCommandBufferedExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--format", "T ", "--buffered", "--", "sh", "-c", "echo before_exit; exit 42")
	if err == nil {
		t.Fatal("expected error from exit 42")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != 42 {
		t.Errorf("exit code = %d, want 42", exitErr.Code)
	}

	if !strings.Contains(out, "O: before_exit") {
		t.Errorf("buffered output should still be flushed, got: %q", out)
	}
	if !strings.Contains(out, "Finished with exitcode 42") {
		t.Errorf("missing finish message, got: %q", out)
	}
}

func TestCommandBufferedWithCapture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--format", "T ", "--buffered", "--capture", "--", "sh", "-c", "echo out; echo err >&2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanupCaptureFiles(t, out)

	// Capture files should have raw output
	stdoutPath := extractCapturePath(out, "capture.stdout=")
	stderrPath := extractCapturePath(out, "capture.stderr=")
	if stdoutPath == "" || stderrPath == "" {
		t.Fatalf("missing capture paths in output: %q", out)
	}

	stdoutData, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("reading stdout capture: %v", err)
	}
	if got := string(stdoutData); got != "out\n" {
		t.Errorf("stdout capture = %q, want %q", got, "out\n")
	}

	stderrData, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("reading stderr capture: %v", err)
	}
	if got := string(stderrData); got != "err\n" {
		t.Errorf("stderr capture = %q, want %q", got, "err\n")
	}

	// Terminal output should have grouped sections
	if !strings.Contains(out, "I: --- stdout ---") {
		t.Errorf("missing stdout header in terminal output: %q", out)
	}
	if !strings.Contains(out, "O: out") {
		t.Errorf("missing stdout content in terminal output: %q", out)
	}
	if !strings.Contains(out, "I: --- stderr ---") {
		t.Errorf("missing stderr header in terminal output: %q", out)
	}
	if !strings.Contains(out, "E: err") {
		t.Errorf("missing stderr content in terminal output: %q", out)
	}
}

func TestCommandBufferedSignalFlush(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Use a temp file for the child to signal readiness, since buffered mode
	// defers output and we cannot poll stdout.
	readyFile := filepath.Join(t.TempDir(), "ready")

	script := fmt.Sprintf(
		`trap 'echo got_sigterm; exit 0' TERM; touch %s; sleep 10`,
		readyFile,
	)

	var outBuf bytes.Buffer
	cmd := NewCommand()
	cmd.SetOut(&outBuf)
	cmd.SetErr(&outBuf)
	cmd.SetArgs([]string{"--format", "T ", "--buffered", "--", "sh", "-c", script})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	// Poll for the ready file
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for child to become ready")
		case err := <-done:
			t.Fatalf("command finished before signal: %v, output: %q", err, outBuf.String())
		default:
		}
		if _, err := os.Stat(readyFile); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Small delay to let the child settle into sleep
	time.Sleep(50 * time.Millisecond)

	// Send SIGTERM to our own process; the signal forwarding goroutine in
	// the runner will relay it to the child process group.
	syscall.Kill(syscall.Getpid(), syscall.SIGTERM)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for command to finish after signal")
	}

	out := outBuf.String()
	if !strings.Contains(out, "O: got_sigterm") {
		t.Errorf("child did not flush signal response, output: %q", out)
	}
	if !strings.Contains(out, "I: --- stdout ---") {
		t.Errorf("missing stdout header after signal, output: %q", out)
	}
}
