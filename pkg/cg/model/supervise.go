package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// SuperviseSpec is the JSON spawn spec the server writes to the supervisor's stdin.
// Argv is the full original command; index 0 stays the child's argv[0]. Resolved and
// Canonical carry the executable identity the approval gate matched, so the supervisor
// execs the same file without a fresh PATH lookup. Env holds caller-supplied overrides;
// it rides the pipe and never appears in argv or on disk. Pool names the pool this run
// is a member of, so the run's on-disk records carry it. SessionID names the cg mcp
// server that spawned the run, carried the same way.
type SuperviseSpec struct {
	Argv      []string          `json:"argv"`
	Resolved  string            `json:"resolved,omitempty"`
	Canonical string            `json:"canonical,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Pool      string            `json:"pool,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
}

// resolution reconstructs the Resolution the server computed, for ExecPath and
// start-failure diagnostics.
func (s *SuperviseSpec) resolution() *Resolution {
	return &Resolution{Argv: s.Argv, Resolved: s.Resolved, Canonical: s.Canonical}
}

// SuperviseAck is the single JSON line the supervisor writes on stdout after the
// child-start attempt, or on any earlier failure. Exactly one ack is ever written;
// stdout is silent afterwards, so EOF means the supervisor exited after meta.json
// was written.
type SuperviseAck struct {
	Started    bool   `json:"started"`
	Pid        int    `json:"pid,omitempty"`
	StartError string `json:"start_error,omitempty"`
}

// NewSuperviseRunCommand creates the hidden `cg supervise-run` subcommand, the re-exec
// entry point the MCP server spawns once per run. Not for human use. The `supervise`
// alias keeps a still-running old server working after the binary on disk is replaced.
func NewSuperviseRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "supervise-run <run-dir>",
		Aliases: []string{"supervise"},
		Short:   "Supervise a capture run (internal; spawned by cg mcp)",
		Hidden:  true,
		Args:    cobra.ExactArgs(1),

		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: func(cmd *cobra.Command, args []string) error {
			// A dead server closing the status pipe is the failure mode the supervisor
			// exists to survive. Without this, Go's default SIGPIPE disposition for
			// fd 1 would kill the supervisor on the ack write.
			signal.Ignore(syscall.SIGPIPE)
			return superviseRun(args[0], cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

// superviseRun is the supervisor body: lock the run dir, read the spec, start the
// child, ack, wait, account, and write meta.json. It returns nil once meta.json is
// written, regardless of the child's exit code; the child's outcome lives in
// meta.json.
func superviseRun(dir string, in io.Reader, out io.Writer) error {
	lock, err := acquireRunLock(dir)
	if err != nil {
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return err
	}
	defer lock.Close()

	data, err := io.ReadAll(in)
	if err != nil {
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return fmt.Errorf("reading spawn spec: %w", err)
	}

	var spec SuperviseSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		err = fmt.Errorf("decoding spawn spec: %w", err)
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return err
	}
	if len(spec.Argv) == 0 {
		err := fmt.Errorf("command is empty")
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return err
	}

	id := filepath.Base(dir)
	cwd := effectiveCwd(spec.Cwd)
	resolved := spec.resolution()

	// The server pre-created the capture files, so open without O_CREATE: a
	// mis-pointed run dir fails loudly instead of minting stray files.
	stdout, err := os.OpenFile(filepath.Join(dir, "stdout"), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		err = fmt.Errorf("opening stdout capture file: %w", err)
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return err
	}
	stderr, err := os.OpenFile(filepath.Join(dir, "stderr"), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		stdout.Close()
		err = fmt.Errorf("opening stderr capture file: %w", err)
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return err
	}

	child := exec.Command(resolved.ExecPath(), spec.Argv[1:]...)
	child.Args[0] = spec.Argv[0]
	child.Dir = cwd
	child.Stdout = stdout
	child.Stderr = stderr
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if len(spec.Env) > 0 {
		child.Env = mergeEnv(os.Environ(), spec.Env)
	}

	cgc := prepareCgroup(child)

	start := time.Now()
	if err := child.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		if cgc != nil {
			cgc.close()
		}
		info := RunInfo{ID: id, Command: spec.Argv, Cwd: cwd, Pool: spec.Pool, SessionID: spec.SessionID, StartedAt: start.UTC()}
		_ = WriteStartDebug(dir, buildStartDebug(info, spec.Env, resolved, err))
		err = fmt.Errorf("starting child: %w", err)
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return err
	}

	_ = WritePidFile(dir, child.Process.Pid)
	_ = WriteStartInfo(dir, &StartInfo{RunInfo: RunInfo{ID: id, Command: spec.Argv, Cwd: cwd, Pool: spec.Pool, SessionID: spec.SessionID, StartedAt: start.UTC()}})

	writeAck(out, SuperviseAck{Started: true, Pid: child.Process.Pid})

	waitErr := child.Wait()
	elapsed := time.Since(start)
	stdout.Close()
	stderr.Close()

	outLines, _ := countLines(filepath.Join(dir, "stdout"))
	errLines, _ := countLines(filepath.Join(dir, "stderr"))

	usage := resolveUsage(cgc, child)
	if cgc != nil {
		cgc.close()
	}

	meta := &Meta{
		RunInfo:     RunInfo{ID: id, Command: spec.Argv, Cwd: cwd, Pool: spec.Pool, SessionID: spec.SessionID, StartedAt: start.UTC()},
		FinishedAt:  start.Add(elapsed).UTC(),
		DurationMs:  elapsed.Milliseconds(),
		ExitCode:    ExitCodeFromError(waitErr),
		StdoutLines: outLines,
		StderrLines: errLines,
		Usage:       &usage,
	}
	if ws := exitStatus(child); ws != nil && ws.Signaled() {
		sig := int(ws.Signal())
		meta.Signal = &sig
	}

	if err := WriteMeta(dir, meta); err != nil {
		return err
	}

	RemovePidFile(dir)
	RemoveStartInfo(dir)
	return nil
}

// writeAck emits the single-line JSON ack. Best-effort: with SIGPIPE ignored, a dead
// server turns the write into an EPIPE, and the supervisor's job of parenting the
// child and writing meta.json continues regardless.
func writeAck(out io.Writer, ack SuperviseAck) {
	data, err := json.Marshal(ack)
	if err != nil {
		return
	}
	_, _ = out.Write(append(data, '\n'))
}

// countLines counts '\n' bytes in the file at path, the same definition the in-process
// streaming counter used, so post-hoc counts match historical ones.
func countLines(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var n int64
	buf := make([]byte, 64*1024)
	for {
		read, err := f.Read(buf)
		n += int64(bytes.Count(buf[:read], []byte{'\n'}))
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
	}
}
