package cg

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runIDLineRE matches the `id=<ID>` suffix on a capture-mode finish line.
var runIDLineRE = regexp.MustCompile(`id=([` + runIDAlphabet + `]{6})\b`)

// extractRunID pulls the run ID from cg's annotated output by scanning the
// finish line.
func extractRunID(output string) string {
	m := runIDLineRE.FindStringSubmatch(output)
	if m == nil {
		return ""
	}
	return m[1]
}

// cleanupRunDir removes the per-run capture directory for the given ID. Tests
// that drop $TMPDIR override via testscript don't need this; tests that hit
// the real $TMPDIR/cg do.
func cleanupRunDir(t *testing.T, id string) {
	t.Helper()
	if id == "" {
		return
	}
	os.RemoveAll(filepath.Join(CaptureRoot(), id))
}

func TestNewCapture(t *testing.T) {
	t.Parallel()

	cap, err := NewCapture()
	if err != nil {
		t.Fatalf("NewCapture() error = %v", err)
	}
	defer cap.Close()
	defer os.RemoveAll(cap.Dir)

	if len(cap.ID) != runIDLen {
		t.Errorf("len(ID) = %d, want %d (id=%q)", len(cap.ID), runIDLen, cap.ID)
	}
	for _, r := range cap.ID {
		if !strings.ContainsRune(runIDAlphabet, r) {
			t.Errorf("ID %q contains %q, not in Crockford alphabet", cap.ID, r)
		}
	}

	wantDir := filepath.Join(CaptureRoot(), cap.ID)
	if cap.Dir != wantDir {
		t.Errorf("Dir = %q, want %q", cap.Dir, wantDir)
	}
	if cap.Stdout.Name() != filepath.Join(cap.Dir, "stdout") {
		t.Errorf("Stdout.Name() = %q, want %q", cap.Stdout.Name(), filepath.Join(cap.Dir, "stdout"))
	}
	if cap.Stderr.Name() != filepath.Join(cap.Dir, "stderr") {
		t.Errorf("Stderr.Name() = %q, want %q", cap.Stderr.Name(), filepath.Join(cap.Dir, "stderr"))
	}
	for _, f := range []*os.File{cap.Stdout, cap.Stderr} {
		if _, err := os.Stat(f.Name()); err != nil {
			t.Errorf("file %s does not exist: %v", f.Name(), err)
		}
	}
}

func TestCommandCaptureCreatesFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--capture", "--", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id := extractRunID(out)
	if id == "" {
		t.Fatalf("missing id= in output: %q", out)
	}
	defer cleanupRunDir(t, id)

	dir := filepath.Join(CaptureRoot(), id)

	data, err := os.ReadFile(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatalf("reading stdout capture: %v", err)
	}
	if got := string(data); got != "hello\n" {
		t.Errorf("stdout capture = %q, want %q", got, "hello\n")
	}

	data, err = os.ReadFile(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatalf("reading stderr capture: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("stderr capture = %q, want empty", string(data))
	}

	if _, err := os.Stat(filepath.Join(dir, MetaFilename)); err != nil {
		t.Errorf("meta.json missing: %v", err)
	}

	// Lifecycle file must not exist anywhere under the run dir or under
	// $TMPDIR with the old PID-based name.
	if _, err := os.Stat(filepath.Join(dir, "lifecycle")); !os.IsNotExist(err) {
		t.Errorf("unexpected lifecycle file: %v", err)
	}
}

func TestCommandCaptureBriefOmitsLifecycleAndPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--capture", "--", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id := extractRunID(out)
	if id == "" {
		t.Fatalf("missing id= in output: %q", out)
	}
	defer cleanupRunDir(t, id)

	if strings.Contains(out, "capture.stdout=") {
		t.Errorf("brief mode should not emit capture.stdout= path, got: %q", out)
	}
	if strings.Contains(out, "capture.stderr=") {
		t.Errorf("brief mode should not emit capture.stderr= path, got: %q", out)
	}
	if strings.Contains(out, "capture.lifecycle=") {
		t.Errorf("lifecycle path line should be gone, got: %q", out)
	}
	if strings.Contains(out, "O: hello") {
		t.Errorf("child stdout should not appear on cg output in capture mode, got: %q", out)
	}
}

func TestCommandCaptureVerboseEmitsPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("-v", "--format", "T ", "--capture", "--", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id := extractRunID(out)
	if id == "" {
		t.Fatalf("missing id= in output: %q", out)
	}
	defer cleanupRunDir(t, id)

	if !strings.Contains(out, "T I: capture.stdout=") {
		t.Errorf("verbose output missing capture.stdout= line, got: %q", out)
	}
	if !strings.Contains(out, "T I: capture.stderr=") {
		t.Errorf("verbose output missing capture.stderr= line, got: %q", out)
	}
	if strings.Contains(out, "capture.lifecycle=") {
		t.Errorf("lifecycle path line should be gone in verbose too, got: %q", out)
	}
}

func TestCommandCaptureShortFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("-c", "--", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id := extractRunID(out)
	if id == "" {
		t.Fatalf("missing id= in output: %q", out)
	}
	defer cleanupRunDir(t, id)

	dir := filepath.Join(CaptureRoot(), id)
	data, err := os.ReadFile(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatalf("reading stdout capture: %v", err)
	}
	if got := string(data); got != "hello\n" {
		t.Errorf("stdout capture = %q, want %q", got, "hello\n")
	}
}

func TestCommandCaptureSeparateStreams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--capture", "--", "sh", "-c", "echo out; echo err >&2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id := extractRunID(out)
	if id == "" {
		t.Fatalf("missing id= in output: %q", out)
	}
	defer cleanupRunDir(t, id)

	dir := filepath.Join(CaptureRoot(), id)

	data, err := os.ReadFile(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatalf("reading stdout capture: %v", err)
	}
	if got := string(data); got != "out\n" {
		t.Errorf("stdout capture = %q, want %q", got, "out\n")
	}

	data, err = os.ReadFile(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatalf("reading stderr capture: %v", err)
	}
	if got := string(data); got != "err\n" {
		t.Errorf("stderr capture = %q, want %q", got, "err\n")
	}
}

func TestCommandCaptureWritesMeta(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--capture", "--", "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id := extractRunID(out)
	if id == "" {
		t.Fatalf("missing id= in output: %q", out)
	}
	defer cleanupRunDir(t, id)

	meta, err := ReadMeta(filepath.Join(CaptureRoot(), id))
	if err != nil {
		t.Fatalf("ReadMeta() error = %v", err)
	}
	if meta.ID != id {
		t.Errorf("meta.ID = %q, want %q", meta.ID, id)
	}
	if len(meta.Command) != 2 || meta.Command[0] != "echo" || meta.Command[1] != "hello" {
		t.Errorf("meta.Command = %v, want [echo hello]", meta.Command)
	}
	if meta.ExitCode != 0 {
		t.Errorf("meta.ExitCode = %d, want 0", meta.ExitCode)
	}
	if meta.Signal != nil {
		t.Errorf("meta.Signal = %v, want nil", meta.Signal)
	}
	if meta.StdoutLines != 1 {
		t.Errorf("meta.StdoutLines = %d, want 1", meta.StdoutLines)
	}
	if meta.StderrLines != 0 {
		t.Errorf("meta.StderrLines = %d, want 0", meta.StderrLines)
	}
	if meta.StartedAt.IsZero() || meta.FinishedAt.IsZero() {
		t.Errorf("meta timestamps not populated: started=%v finished=%v",
			meta.StartedAt, meta.FinishedAt)
	}
	if meta.DurationMs < 0 {
		t.Errorf("meta.DurationMs = %d, want >= 0", meta.DurationMs)
	}
}

func TestCommandCaptureEmptyStreams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	out, err := runCgCommand("--capture", "--", "true")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id := extractRunID(out)
	if id == "" {
		t.Fatalf("missing id= in output: %q", out)
	}
	defer cleanupRunDir(t, id)

	dir := filepath.Join(CaptureRoot(), id)
	for _, name := range []string{"stdout", "stderr", MetaFilename} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("file %s should exist: %v", name, err)
		}
	}
	for _, name := range []string{"stdout", "stderr"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if len(data) != 0 {
			t.Errorf("%s should be empty, got %q", name, string(data))
		}
	}
}
