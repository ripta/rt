package cg

import (
	"bytes"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

var finishLineRE = regexp.MustCompile(`Finished (exitcode=\d+|signal=\d+) in [0-9.]+(?:ns|us|µs|ms|s) \(out=(\d+) err=(\d+)\)`)

type exitCodeFromErrorTest struct {
	name string
	err  error
	want int
}

var exitCodeFromErrorTests = []exitCodeFromErrorTest{
	{
		name: "nil error",
		err:  nil,
		want: 0,
	},
	{
		name: "command not found",
		err:  exec.ErrNotFound,
		want: 127,
	},
	{
		name: "exec.Error wrapping ErrNotFound",
		err:  &exec.Error{Name: "nosuchcmd", Err: exec.ErrNotFound},
		want: 127,
	},
	{
		name: "exec.Error wrapping EACCES",
		err:  &exec.Error{Name: "/bin/noperm", Err: syscall.EACCES},
		want: 126,
	},
	{
		name: "generic error",
		err:  errors.New("something broke"),
		want: 1,
	},
}

func TestExitCodeFromError(t *testing.T) {
	t.Parallel()

	for _, tt := range exitCodeFromErrorTests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExitCodeFromError(tt.err)
			if got != tt.want {
				t.Errorf("ExitCodeFromError() = %d, want %d", got, tt.want)
			}
		})
	}
}

type shellQuoteTest struct {
	name  string
	input string
	want  string
}

var shellQuoteTests = []shellQuoteTest{
	{
		name:  "simple word",
		input: "hello",
		want:  "hello",
	},
	{
		name:  "path",
		input: "/usr/bin/echo",
		want:  "/usr/bin/echo",
	},
	{
		name:  "empty string",
		input: "",
		want:  "''",
	},
	{
		name:  "contains space",
		input: "hello world",
		want:  "'hello world'",
	},
	{
		name:  "contains single quote",
		input: "it's",
		want:  `'it'\''s'`,
	},
	{
		name:  "contains semicolon",
		input: "echo;rm",
		want:  "'echo;rm'",
	},
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	for _, tt := range shellQuoteTests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.input)
			if got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

type escapeArgsTest struct {
	name string
	args []string
	want string
}

var escapeArgsTests = []escapeArgsTest{
	{
		name: "simple command",
		args: []string{"echo", "hello"},
		want: "echo hello",
	},
	{
		name: "command with spaces in arg",
		args: []string{"echo", "hello world"},
		want: "echo 'hello world'",
	},
	{
		name: "sh -c with quoted arg",
		args: []string{"sh", "-c", "echo out; echo err >&2"},
		want: "sh -c 'echo out; echo err >&2'",
	},
}

func TestEscapeArgs(t *testing.T) {
	t.Parallel()

	for _, tt := range escapeArgsTests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeArgs(tt.args)
			if got != tt.want {
				t.Errorf("escapeArgs() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIntegrationStdout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	cmd := exec.Command("echo", "hello")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("echo command failed: %v", err)
	}
	if got := string(out); got != "hello\n" {
		t.Errorf("echo output = %q, want %q", got, "hello\n")
	}
}

func TestIntegrationExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	cmd := exec.Command("sh", "-c", "exit 42")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error from exit 42")
	}

	code := ExitCodeFromError(err)
	if code != 42 {
		t.Errorf("ExitCodeFromError() = %d, want 42", code)
	}
}

func TestIntegrationCommandNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	cmd := exec.Command("__nonexistent_command_for_cg_test__")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}

	code := ExitCodeFromError(err)
	if code != 127 {
		t.Errorf("ExitCodeFromError() = %d, want 127", code)
	}
}

// runCgCommand executes the cg cobra command with the given args and returns
// the captured output and error.
func runCgCommand(args ...string) (string, error) {
	var buf bytes.Buffer
	cmd := NewCommand()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestCommandLifecycleMessagesVerbose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("-v", "--format", "T ", "--", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d: %q", len(lines), out)
	}

	// First line: version info
	if !strings.HasPrefix(lines[0], "T I: cg ") {
		t.Errorf("line 0 = %q, want prefix %q", lines[0], "T I: cg ")
	}

	// Second line: prefix info
	if !strings.HasPrefix(lines[1], "T I: prefix=") {
		t.Errorf("line 1 = %q, want prefix %q", lines[1], "T I: prefix=")
	}
	if !strings.Contains(lines[1], `"T "`) {
		t.Errorf("line 1 = %q, want to contain %q", lines[1], `"T "`)
	}

	// Third line: Started
	if !strings.HasPrefix(lines[2], "T I: Started echo hello") {
		t.Errorf("line 2 = %q, want prefix %q", lines[2], "T I: Started echo hello")
	}

	// Fourth line: child output
	if lines[3] != "T O: hello" {
		t.Errorf("line 3 = %q, want %q", lines[3], "T O: hello")
	}

	// Last line: Finished
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "T I: ") {
		t.Errorf("last line = %q, want %q prefix", last, "T I: ")
	}
	m := finishLineRE.FindStringSubmatch(last)
	if m == nil {
		t.Fatalf("last line %q does not match finish line format", last)
	}
	if m[1] != "exitcode=0" {
		t.Errorf("finish line head = %q, want %q", m[1], "exitcode=0")
	}
	if m[2] != "1" {
		t.Errorf("finish line out= = %q, want %q", m[2], "1")
	}
	if m[3] != "0" {
		t.Errorf("finish line err= = %q, want %q", m[3], "0")
	}
}

func TestCommandBriefDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	if lines[0] != "O: hello" {
		t.Errorf("line 0 = %q, want %q", lines[0], "O: hello")
	}
	if !strings.HasPrefix(lines[1], "I: ") {
		t.Errorf("line 1 = %q, want %q prefix", lines[1], "I: ")
	}
	m := finishLineRE.FindStringSubmatch(lines[1])
	if m == nil {
		t.Fatalf("line 1 %q does not match finish line format", lines[1])
	}
	if m[1] != "exitcode=0" {
		t.Errorf("finish head = %q, want %q", m[1], "exitcode=0")
	}
	if m[2] != "1" || m[3] != "0" {
		t.Errorf("finish counts = (out=%s err=%s), want (out=1 err=0)", m[2], m[3])
	}
}

func TestCommandStderrOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--", "sh", "-c", "echo out; echo err >&2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "O: out\n") {
		t.Errorf("output missing stdout line, got: %q", out)
	}
	if !strings.Contains(out, "E: err\n") {
		t.Errorf("output missing stderr line, got: %q", out)
	}
}

func TestCommandExitCodePropagation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--", "sh", "-c", "exit 42")
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

	if !strings.Contains(out, "Finished exitcode=42 in ") {
		t.Errorf("output missing finish message, got: %q", out)
	}
}

func TestCommandPartialLine(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--", "sh", "-c", `printf "no newline"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "O: no newline") {
		t.Errorf("output missing partial line, got: %q", out)
	}
}

func TestCommandCustomFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("-v", "--format", "2006-01-02 ", "--", "echo", "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The prefix should be a date like "2026-02-22 "
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for _, line := range lines {
		// Each line should start with a date pattern
		if len(line) < 11 {
			t.Errorf("line too short: %q", line)
			continue
		}
		// Rough check: starts with 4 digits
		if line[0] < '0' || line[0] > '9' {
			t.Errorf("line does not start with date: %q", line)
		}
	}
}

func TestCommandSignalForwarding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Run a child that traps SIGTERM and echoes it, then exits
	script := `trap 'echo got_sigterm; exit 0' TERM; echo ready; sleep 10`

	var buf bytes.Buffer
	cmd := NewCommand()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--", "sh", "-c", script})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	// Wait for the child to be ready
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for child to become ready")
		case err := <-done:
			t.Fatalf("command finished before signal: %v, output: %q", err, buf.String())
		default:
		}
		if strings.Contains(buf.String(), "O: ready") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Send SIGTERM to our own process group; the child should receive it via
	// the signal forwarding goroutine
	syscall.Kill(syscall.Getpid(), syscall.SIGTERM)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for command to finish after signal")
	}

	out := buf.String()
	if !strings.Contains(out, "O: got_sigterm") {
		t.Errorf("child did not receive signal, output: %q", out)
	}

	// The shell traps SIGTERM and exits 0, so the wait status is not signaled.
	// The finish line should still show counts that include both child stdout
	// lines ("ready" and "got_sigterm").
	m := finishLineRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("output missing finish line: %q", out)
	}
	if m[2] != "2" {
		t.Errorf("finish line out= = %q, want %q", m[2], "2")
	}
}

func TestCommandFinishLineSignaled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Child that does not trap SIGTERM, so the wait status reports the signal.
	script := `echo ready; sleep 10`

	var buf bytes.Buffer
	cmd := NewCommand()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--", "sh", "-c", script})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for child to become ready")
		case err := <-done:
			t.Fatalf("command finished before signal: %v, output: %q", err, buf.String())
		default:
		}
		if strings.Contains(buf.String(), "O: ready") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	syscall.Kill(syscall.Getpid(), syscall.SIGTERM)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for command to finish after signal")
	}

	out := buf.String()
	m := finishLineRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("output missing finish line: %q", out)
	}
	if m[1] != "signal=15" {
		t.Errorf("finish head = %q, want %q", m[1], "signal=15")
	}
	if m[2] != "1" {
		t.Errorf("finish out= = %q, want %q", m[2], "1")
	}
}

type formatDurationTest struct {
	name string
	in   time.Duration
	want string
}

var formatDurationTests = []formatDurationTest{
	{name: "sub-microsecond rounds to zero ms", in: 500 * time.Nanosecond, want: "0s"},
	{name: "sub-second rounds to ms", in: 12*time.Millisecond + 345*time.Microsecond, want: "12ms"},
	{name: "sub-minute rounds to 10ms", in: 1234 * time.Millisecond, want: "1.23s"},
	{name: "minute-plus rounds to seconds", in: 90*time.Second + 400*time.Millisecond, want: "1m30s"},
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	for _, tt := range formatDurationTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDuration(tt.in); got != tt.want {
				t.Errorf("formatDuration(%s) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

type formatFinishTest struct {
	name     string
	code     int
	signaled bool
	sig      int
	dur      time.Duration
	outLines int64
	errLines int64
	id       string
	want     string
}

var formatFinishTests = []formatFinishTest{
	{
		name: "exit code 0",
		code: 0, signaled: false, sig: 0,
		dur: 12 * time.Millisecond, outLines: 1, errLines: 0,
		want: "Finished exitcode=0 in 12ms (out=1 err=0)",
	},
	{
		name: "exit code non-zero",
		code: 42, signaled: false, sig: 0,
		dur: 1234 * time.Millisecond, outLines: 0, errLines: 0,
		want: "Finished exitcode=42 in 1.23s (out=0 err=0)",
	},
	{
		name: "signaled",
		code: -1, signaled: true, sig: 15,
		dur: 5 * time.Millisecond, outLines: 1, errLines: 0,
		want: "Finished signal=15 in 5ms (out=1 err=0)",
	},
	{
		name: "with id",
		code: 0, signaled: false, sig: 0,
		dur: 12 * time.Millisecond, outLines: 1, errLines: 0, id: "Q3F9K2",
		want: "Finished exitcode=0 in 12ms (out=1 err=0) id=Q3F9K2",
	},
	{
		name: "signaled with id",
		code: -1, signaled: true, sig: 15,
		dur: 5 * time.Millisecond, outLines: 1, errLines: 0, id: "ABC123",
		want: "Finished signal=15 in 5ms (out=1 err=0) id=ABC123",
	},
}

func TestFormatFinish(t *testing.T) {
	t.Parallel()

	for _, tt := range formatFinishTests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFinish(tt.code, tt.signaled, tt.sig, tt.dur, tt.outLines, tt.errLines, tt.id)
			if got != tt.want {
				t.Errorf("formatFinish() = %q, want %q", got, tt.want)
			}
		})
	}
}
