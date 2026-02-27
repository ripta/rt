package cg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewCapture(t *testing.T) {
	prefix := func() string { return "T " }
	pid := os.Getpid()

	cap, err := NewCapture(pid, prefix)
	if err != nil {
		t.Fatalf("NewCapture() error = %v", err)
	}
	defer cap.Close()
	defer os.Remove(cap.Stdout.Name())
	defer os.Remove(cap.Stderr.Name())
	defer os.Remove(cap.Lifecycle.Name())

	dir := os.TempDir()
	wantStdout := filepath.Join(dir, fmt.Sprintf("cg-%d-stdout", pid))
	wantStderr := filepath.Join(dir, fmt.Sprintf("cg-%d-stderr", pid))
	wantLifecycle := filepath.Join(dir, fmt.Sprintf("cg-%d-lifecycle", pid))

	if cap.Stdout.Name() != wantStdout {
		t.Errorf("Stdout.Name() = %q, want %q", cap.Stdout.Name(), wantStdout)
	}
	if cap.Stderr.Name() != wantStderr {
		t.Errorf("Stderr.Name() = %q, want %q", cap.Stderr.Name(), wantStderr)
	}
	if cap.Lifecycle.Name() != wantLifecycle {
		t.Errorf("Lifecycle.Name() = %q, want %q", cap.Lifecycle.Name(), wantLifecycle)
	}

	for _, f := range []*os.File{cap.Stdout, cap.Stderr, cap.Lifecycle} {
		if _, err := os.Stat(f.Name()); err != nil {
			t.Errorf("file %s does not exist: %v", f.Name(), err)
		}
	}
}

func TestCaptureWriteLifecycle(t *testing.T) {
	prefix := func() string { return "T " }
	pid := os.Getpid() + 10000 // offset to avoid collision with other tests

	cap, err := NewCapture(pid, prefix)
	if err != nil {
		t.Fatalf("NewCapture() error = %v", err)
	}
	defer os.Remove(cap.Stdout.Name())
	defer os.Remove(cap.Stderr.Name())
	defer os.Remove(cap.Lifecycle.Name())

	if err := cap.WriteLifecycle("Started echo hello"); err != nil {
		t.Fatalf("WriteLifecycle() error = %v", err)
	}
	if err := cap.WriteLifecycle("Finished with exitcode 0"); err != nil {
		t.Fatalf("WriteLifecycle() error = %v", err)
	}
	cap.Close()

	data, err := os.ReadFile(cap.Lifecycle.Name())
	if err != nil {
		t.Fatalf("reading lifecycle file: %v", err)
	}

	want := "T I: Started echo hello\nT I: Finished with exitcode 0\n"
	if got := string(data); got != want {
		t.Errorf("lifecycle file = %q, want %q", got, want)
	}
}

// extractCapturePath finds a capture path announcement in cg output.
func extractCapturePath(output, key string) string {
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, key)
		if idx >= 0 {
			return line[idx+len(key):]
		}
	}
	return ""
}

// cleanupCaptureFiles removes capture files found in cg output.
func cleanupCaptureFiles(t *testing.T, output string) {
	t.Helper()
	for _, key := range []string{"capture.stdout=", "capture.stderr=", "capture.lifecycle="} {
		path := extractCapturePath(output, key)
		if path != "" {
			os.Remove(path)
		}
	}
}

func TestCommandCaptureCreatesFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--format", "T ", "--capture", "--", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanupCaptureFiles(t, out)

	stdoutPath := extractCapturePath(out, "capture.stdout=")
	stderrPath := extractCapturePath(out, "capture.stderr=")
	lifecyclePath := extractCapturePath(out, "capture.lifecycle=")

	if stdoutPath == "" {
		t.Fatal("missing capture.stdout path in output")
	}
	if stderrPath == "" {
		t.Fatal("missing capture.stderr path in output")
	}
	if lifecyclePath == "" {
		t.Fatal("missing capture.lifecycle path in output")
	}

	// Stdout file contains raw output
	data, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("reading stdout capture: %v", err)
	}
	if got := string(data); got != "hello\n" {
		t.Errorf("stdout capture = %q, want %q", got, "hello\n")
	}

	// Stderr file exists but is empty
	data, err = os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("reading stderr capture: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("stderr capture = %q, want empty", string(data))
	}

	// Lifecycle file contains Started and Finished
	data, err = os.ReadFile(lifecyclePath)
	if err != nil {
		t.Fatalf("reading lifecycle capture: %v", err)
	}
	lifecycle := string(data)
	if !strings.Contains(lifecycle, "Started echo hello") {
		t.Errorf("lifecycle missing Started message: %q", lifecycle)
	}
	if !strings.Contains(lifecycle, "Finished with exitcode 0") {
		t.Errorf("lifecycle missing Finished message: %q", lifecycle)
	}
}

func TestCommandCaptureSuppressesChildOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--format", "T ", "--capture", "--", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanupCaptureFiles(t, out)

	// Child output must not appear annotated on cg's stdout
	if strings.Contains(out, "O: hello") {
		t.Errorf("child stdout should not appear on cg output in capture mode, got: %q", out)
	}

	// Lifecycle messages must appear
	if !strings.Contains(out, "I: Started echo hello") {
		t.Errorf("lifecycle messages should appear on stdout, got: %q", out)
	}
	if !strings.Contains(out, "I: Finished with exitcode 0") {
		t.Errorf("finished message should appear on stdout, got: %q", out)
	}
}

func TestCommandCaptureSeparateStreams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--format", "T ", "--capture", "--", "sh", "-c", "echo out; echo err >&2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanupCaptureFiles(t, out)

	stdoutPath := extractCapturePath(out, "capture.stdout=")
	stderrPath := extractCapturePath(out, "capture.stderr=")

	data, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("reading stdout capture: %v", err)
	}
	if got := string(data); got != "out\n" {
		t.Errorf("stdout capture = %q, want %q", got, "out\n")
	}

	data, err = os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("reading stderr capture: %v", err)
	}
	if got := string(data); got != "err\n" {
		t.Errorf("stderr capture = %q, want %q", got, "err\n")
	}
}

func TestCommandCaptureLifecycleFileContents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--format", "T ", "--capture", "--", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanupCaptureFiles(t, out)

	lifecyclePath := extractCapturePath(out, "capture.lifecycle=")
	data, err := os.ReadFile(lifecyclePath)
	if err != nil {
		t.Fatalf("reading lifecycle capture: %v", err)
	}
	lifecycle := string(data)

	// Should contain version info
	if !strings.Contains(lifecycle, "I: cg ") {
		t.Errorf("lifecycle missing version info: %q", lifecycle)
	}

	// Should contain prefix info
	if !strings.Contains(lifecycle, "I: prefix=") {
		t.Errorf("lifecycle missing prefix info: %q", lifecycle)
	}

	// Should contain capture path announcements
	if !strings.Contains(lifecycle, "I: capture.stdout=") {
		t.Errorf("lifecycle missing capture.stdout path: %q", lifecycle)
	}

	// Should contain Started and Finished
	if !strings.Contains(lifecycle, "I: Started echo hello") {
		t.Errorf("lifecycle missing Started: %q", lifecycle)
	}
	if !strings.Contains(lifecycle, "I: Finished with exitcode 0") {
		t.Errorf("lifecycle missing Finished: %q", lifecycle)
	}

	// Should NOT contain child output
	if strings.Contains(lifecycle, "O: hello") {
		t.Errorf("lifecycle should not contain child output: %q", lifecycle)
	}
}

func TestCommandCaptureEmptyStreams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--format", "T ", "--capture", "--", "true")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanupCaptureFiles(t, out)

	stdoutPath := extractCapturePath(out, "capture.stdout=")
	stderrPath := extractCapturePath(out, "capture.stderr=")
	lifecyclePath := extractCapturePath(out, "capture.lifecycle=")

	// All three files should exist
	for _, path := range []string{stdoutPath, stderrPath, lifecyclePath} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("file %s should exist: %v", path, err)
		}
	}

	// Stdout and stderr should be empty
	for _, path := range []string{stdoutPath, stderrPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if len(data) != 0 {
			t.Errorf("%s should be empty, got %q", path, string(data))
		}
	}

	// Lifecycle should still have content
	data, err := os.ReadFile(lifecyclePath)
	if err != nil {
		t.Fatalf("reading lifecycle: %v", err)
	}
	if !strings.Contains(string(data), "Finished with exitcode 0") {
		t.Errorf("lifecycle missing Finished message: %q", string(data))
	}
}
