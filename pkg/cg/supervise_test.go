package cg

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newSuperviseDir simulates the server side of a supervised run: allocate the run
// directory and capture files via NewCapture, then close the handles, since the
// supervisor reopens them by path.
func newSuperviseDir(t *testing.T) *Capture {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())

	cap, err := NewCapture()
	if err != nil {
		t.Fatalf("NewCapture: %v", err)
	}
	if err := cap.Close(); err != nil {
		t.Fatalf("closing capture files: %v", err)
	}
	return cap
}

// superviseSpec builds a spec for argv with the executable identity resolved the way
// the server resolves it.
func superviseSpec(t *testing.T, argv []string, cwd string, env map[string]string) *SuperviseSpec {
	t.Helper()

	resolved, _ := ResolveCommand(argv, cwd)
	return &SuperviseSpec{
		Argv:      argv,
		Resolved:  resolved.Resolved,
		Canonical: resolved.Canonical,
		Cwd:       cwd,
		Env:       env,
	}
}

// runSupervise drives superviseRun with the marshaled spec on stdin and decodes the
// ack from stdout. It returns the ack, everything written to stdout, and the error.
func runSupervise(t *testing.T, dir string, spec *SuperviseSpec) (SuperviseAck, string, error) {
	t.Helper()

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshaling spec: %v", err)
	}

	var out bytes.Buffer
	runErr := superviseRun(dir, bytes.NewReader(data), &out)

	var ack SuperviseAck
	if err := json.Unmarshal(out.Bytes(), &ack); err != nil {
		t.Fatalf("decoding ack from %q: %v", out.String(), err)
	}
	return ack, out.String(), runErr
}

func TestSuperviseEcho(t *testing.T) {
	cap := newSuperviseDir(t)

	ack, out, err := runSupervise(t, cap.Dir, superviseSpec(t, []string{"echo", "hello"}, "", nil))
	if err != nil {
		t.Fatalf("superviseRun: %v", err)
	}
	if !ack.Started || ack.Pid <= 0 {
		t.Errorf("ack = %+v, want started with a pid", ack)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("stdout = %q, want a single ack line", out)
	}

	stdout, err := os.ReadFile(filepath.Join(cap.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if string(stdout) != "hello\n" {
		t.Errorf("stdout = %q, want %q", stdout, "hello\n")
	}

	meta, err := ReadMeta(cap.Dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if meta.ID != cap.ID {
		t.Errorf("meta.ID = %q, want %q", meta.ID, cap.ID)
	}
	if meta.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", meta.ExitCode)
	}
	if meta.StdoutLines != 1 || meta.StderrLines != 0 {
		t.Errorf("lines: out=%d err=%d, want out=1 err=0", meta.StdoutLines, meta.StderrLines)
	}
	if meta.Signal != nil {
		t.Errorf("Signal = %v, want nil", *meta.Signal)
	}
	if meta.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want >= 0", meta.DurationMs)
	}
	if meta.FinishedAt.Before(meta.StartedAt) {
		t.Errorf("FinishedAt %v before StartedAt %v", meta.FinishedAt, meta.StartedAt)
	}

	if _, err := ReadPidFile(cap.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadPidFile after finish: err = %v, want ErrNotExist", err)
	}
	if _, err := ReadStartInfo(cap.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadStartInfo after finish: err = %v, want ErrNotExist", err)
	}
	if _, err := os.Stat(filepath.Join(cap.Dir, LockFilename)); err != nil {
		t.Errorf("lock file: %v", err)
	}
}

func TestSuperviseStderrAndExit(t *testing.T) {
	cap := newSuperviseDir(t)

	ack, _, err := runSupervise(t, cap.Dir, superviseSpec(t, []string{"sh", "-c", "echo only-err >&2; exit 3"}, "", nil))
	if err != nil {
		t.Fatalf("superviseRun: %v", err)
	}
	if !ack.Started {
		t.Errorf("ack = %+v, want started", ack)
	}

	stderr, err := os.ReadFile(filepath.Join(cap.Dir, "stderr"))
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if string(stderr) != "only-err\n" {
		t.Errorf("stderr = %q, want %q", stderr, "only-err\n")
	}

	meta, err := ReadMeta(cap.Dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if meta.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", meta.ExitCode)
	}
	if meta.StderrLines != 1 || meta.StdoutLines != 0 {
		t.Errorf("lines: out=%d err=%d, want out=0 err=1", meta.StdoutLines, meta.StderrLines)
	}
}

func TestSuperviseSignal(t *testing.T) {
	cap := newSuperviseDir(t)

	_, _, err := runSupervise(t, cap.Dir, superviseSpec(t, []string{"sh", "-c", "kill -TERM $$"}, "", nil))
	if err != nil {
		t.Fatalf("superviseRun: %v", err)
	}

	meta, err := ReadMeta(cap.Dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if meta.Signal == nil || *meta.Signal != 15 {
		t.Errorf("Signal = %v, want 15", meta.Signal)
	}
}

func TestSuperviseEnvOverride(t *testing.T) {
	t.Setenv("CG_OVERRIDE_ME", "parent-value")
	cap := newSuperviseDir(t)

	env := map[string]string{"CG_OVERRIDE_ME": "child-value"}
	_, _, err := runSupervise(t, cap.Dir, superviseSpec(t, []string{"sh", "-c", "echo $CG_OVERRIDE_ME"}, "", env))
	if err != nil {
		t.Fatalf("superviseRun: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(cap.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if strings.TrimSpace(string(out)) != "child-value" {
		t.Errorf("stdout = %q, want %q", out, "child-value\n")
	}
}

func TestSuperviseCwd(t *testing.T) {
	cap := newSuperviseDir(t)

	dir := t.TempDir()
	_, _, err := runSupervise(t, cap.Dir, superviseSpec(t, []string{"pwd"}, dir, nil))
	if err != nil {
		t.Fatalf("superviseRun: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(cap.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	got, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("EvalSymlinks(stdout): %v", err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(want): %v", err)
	}
	if got != want {
		t.Errorf("pwd = %q, want %q", got, want)
	}
}

func TestSuperviseStartError(t *testing.T) {
	cap := newSuperviseDir(t)

	ack, _, err := runSupervise(t, cap.Dir, superviseSpec(t, []string{"this-binary-does-not-exist-zzzz"}, "", nil))
	if err == nil {
		t.Fatal("superviseRun: expected error, got nil")
	}
	if ack.Started || ack.StartError == "" {
		t.Errorf("ack = %+v, want a start error", ack)
	}

	dbg, err := ReadStartDebug(cap.Dir)
	if err != nil {
		t.Fatalf("ReadStartDebug: %v", err)
	}
	if dbg.StartError == "" {
		t.Error("StartDebug.StartError is empty")
	}

	if _, err := ReadMeta(cap.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadMeta: err = %v, want ErrNotExist", err)
	}
	if _, err := ReadPidFile(cap.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadPidFile: err = %v, want ErrNotExist", err)
	}
}

func TestSuperviseBadSpec(t *testing.T) {
	cap := newSuperviseDir(t)

	var out bytes.Buffer
	err := superviseRun(cap.Dir, strings.NewReader("not json"), &out)
	if err == nil {
		t.Fatal("superviseRun: expected error, got nil")
	}

	var ack SuperviseAck
	if err := json.Unmarshal(out.Bytes(), &ack); err != nil {
		t.Fatalf("decoding ack from %q: %v", out.String(), err)
	}
	if ack.Started || ack.StartError == "" {
		t.Errorf("ack = %+v, want a start error", ack)
	}

	if _, err := ReadStartDebug(cap.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadStartDebug: err = %v, want ErrNotExist", err)
	}
	if _, err := ReadMeta(cap.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadMeta: err = %v, want ErrNotExist", err)
	}
}

func TestSuperviseEmptyArgv(t *testing.T) {
	cap := newSuperviseDir(t)

	ack, _, err := runSupervise(t, cap.Dir, &SuperviseSpec{})
	if err == nil {
		t.Fatal("superviseRun: expected error, got nil")
	}
	if ack.Started || ack.StartError == "" {
		t.Errorf("ack = %+v, want a start error", ack)
	}
}

func TestSuperviseLockHeld(t *testing.T) {
	cap := newSuperviseDir(t)

	// flock conflicts across open file descriptions even within one process, so
	// pre-acquiring here stands in for a competing supervisor.
	lock, err := acquireRunLock(cap.Dir)
	if err != nil {
		t.Fatalf("acquireRunLock: %v", err)
	}
	defer lock.Close()

	ack, _, err := runSupervise(t, cap.Dir, superviseSpec(t, []string{"echo", "hello"}, "", nil))
	if !errors.Is(err, errRunLockHeld) {
		t.Fatalf("superviseRun: err = %v, want errRunLockHeld", err)
	}
	if ack.Started || ack.StartError == "" {
		t.Errorf("ack = %+v, want a start error", ack)
	}

	// The dir belongs to the lock holder; the loser must not touch it.
	stdout, err := os.ReadFile(filepath.Join(cap.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if len(stdout) != 0 {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if _, err := ReadMeta(cap.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadMeta: err = %v, want ErrNotExist", err)
	}
	if _, err := ReadStartDebug(cap.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadStartDebug: err = %v, want ErrNotExist", err)
	}
}

func TestSuperviseMissingRunDir(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	dir := filepath.Join(CaptureRoot(), "ZZZZZZ")
	ack, _, err := runSupervise(t, dir, superviseSpec(t, []string{"echo", "hello"}, "", nil))
	if err == nil {
		t.Fatal("superviseRun: expected error, got nil")
	}
	if ack.Started || ack.StartError == "" {
		t.Errorf("ack = %+v, want a start error", ack)
	}
}

func TestSuperviseMissingCaptureFiles(t *testing.T) {
	dir := t.TempDir()

	ack, _, err := runSupervise(t, dir, superviseSpec(t, []string{"echo", "hello"}, "", nil))
	if err == nil {
		t.Fatal("superviseRun: expected error, got nil")
	}
	if ack.Started || ack.StartError == "" {
		t.Errorf("ack = %+v, want a start error", ack)
	}
}

type countLinesTest struct {
	Name    string
	Payload string
	Want    int64
}

var countLinesTests = []countLinesTest{
	{Name: "empty", Payload: "", Want: 0},
	{Name: "single line", Payload: "hello\n", Want: 1},
	{Name: "no trailing newline", Payload: "hello\nworld", Want: 1},
	{Name: "blank lines", Payload: "\n\n\n", Want: 3},
	{Name: "crlf", Payload: "a\r\nb\r\n", Want: 2},
	{Name: "larger than read buffer", Payload: strings.Repeat(strings.Repeat("x", 1023)+"\n", 100), Want: 100},
}

func TestCountLines(t *testing.T) {
	for _, test := range countLinesTests {
		t.Run(test.Name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "payload")
			if err := os.WriteFile(path, []byte(test.Payload), 0o644); err != nil {
				t.Fatalf("writing payload file: %v", err)
			}

			got, err := countLines(path)
			if err != nil {
				t.Fatalf("countLines: %v", err)
			}
			if got != test.Want {
				t.Errorf("countLines = %d, want %d", got, test.Want)
			}
		})
	}
}

func TestCountLinesMissingFile(t *testing.T) {
	n, err := countLines(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("countLines: expected error, got nil")
	}
	if n != 0 {
		t.Errorf("countLines = %d, want 0", n)
	}
}

func TestSuperviseCommandWiring(t *testing.T) {
	cap := newSuperviseDir(t)

	spec, err := json.Marshal(superviseSpec(t, []string{"echo", "wired"}, "", nil))
	if err != nil {
		t.Fatalf("marshaling spec: %v", err)
	}

	var out bytes.Buffer
	root := NewCommand()
	root.SetArgs([]string{"supervise", cap.Dir})
	root.SetIn(bytes.NewReader(spec))
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var ack SuperviseAck
	if err := json.Unmarshal(out.Bytes(), &ack); err != nil {
		t.Fatalf("decoding ack from %q: %v", out.String(), err)
	}
	if !ack.Started {
		t.Errorf("ack = %+v, want started", ack)
	}

	if _, err := ReadMeta(cap.Dir); err != nil {
		t.Errorf("ReadMeta: %v", err)
	}

	found := false
	for _, sub := range root.Commands() {
		if sub.Name() == "supervise" {
			found = true
			if !sub.Hidden {
				t.Error("supervise command is not hidden")
			}
		}
	}
	if !found {
		t.Error("supervise command not registered")
	}
}
