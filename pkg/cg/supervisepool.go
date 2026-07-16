package cg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// PoolCommand is one command in a pool spec. Argv is the full command; Resolved and
// Canonical carry the executable identity the approval gate matched, so member runs
// exec the same file without a fresh PATH lookup.
type PoolCommand struct {
	Argv      []string `json:"argv"`
	Resolved  string   `json:"resolved,omitempty"`
	Canonical string   `json:"canonical,omitempty"`
}

// resolution reconstructs the Resolution the server computed for this command.
func (c *PoolCommand) resolution() *Resolution {
	return &Resolution{Argv: c.Argv, Resolved: c.Resolved, Canonical: c.Canonical}
}

// PoolSpec is the JSON spec the server writes to the pool supervisor's stdin. Env
// holds caller-supplied overrides shared by every member run; it rides the pipe,
// is forwarded over each member supervisor's spec pipe, and never lands in argv
// or on disk. Zero Repeat and Parallelism normalize to 1; an empty OnError
// normalizes to continue.
type PoolSpec struct {
	Commands    []PoolCommand     `json:"commands"`
	Repeat      int               `json:"repeat"`
	Parallelism int               `json:"parallelism"`
	OnError     string            `json:"on_error"`
	Cwd         string            `json:"cwd,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

// normalize applies spec defaults and rejects malformed input. The MCP layer
// validates user input against the tool bounds; this guards the supervisor
// against a malformed pipe payload.
func (s *PoolSpec) normalize() error {
	if len(s.Commands) == 0 {
		return fmt.Errorf("commands is empty")
	}
	for i, c := range s.Commands {
		if len(c.Argv) == 0 {
			return fmt.Errorf("command %d is empty", i)
		}
	}

	switch s.Repeat {
	case 0:
		s.Repeat = 1
	default:
		if s.Repeat < 0 {
			return fmt.Errorf("repeat must be positive, got %d", s.Repeat)
		}
	}

	switch s.Parallelism {
	case 0:
		s.Parallelism = 1
	default:
		if s.Parallelism < 0 {
			return fmt.Errorf("parallelism must be positive, got %d", s.Parallelism)
		}
	}

	switch s.OnError {
	case "":
		s.OnError = OnErrorContinue
	case OnErrorContinue, OnErrorStop, OnErrorKill:
	default:
		return fmt.Errorf("unknown on_error: %q", s.OnError)
	}
	return nil
}

// NewSupervisePoolCommand creates the hidden `cg supervise-pool` subcommand, the
// re-exec entry point the MCP server spawns once per pool. Not for human use.
func NewSupervisePoolCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "supervise-pool <pool-dir>",
		Short:  "Supervise a pool of capture runs (internal; spawned by cg mcp)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),

		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: func(cmd *cobra.Command, args []string) error {
			// A dead server closing the status pipe must not kill the pool
			// supervisor on the ack write; see NewSuperviseRunCommand.
			signal.Ignore(syscall.SIGPIPE)
			return supervisePool(args[0], cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

// supervisePool is the pool supervisor body: lock the pool dir, read the spec,
// write the initial manifest, ack, schedule member runs, and write the final
// manifest. It returns nil once the final manifest is written, regardless of
// member outcomes; those live in pool.json.
//
// The signal protocol: SIGTERM stops scheduling and lets in-flight members
// finish; SIGINT additionally cancels in-flight members via their process
// groups.
func supervisePool(dir string, in io.Reader, out io.Writer) error {
	lock, err := acquireRunLock(dir)
	if err != nil {
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return err
	}
	defer lock.Close()

	data, err := io.ReadAll(in)
	if err != nil {
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return fmt.Errorf("reading pool spec: %w", err)
	}

	var spec PoolSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		err = fmt.Errorf("decoding pool spec: %w", err)
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return err
	}
	if err := spec.normalize(); err != nil {
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return err
	}

	// Install the signal protocol before acking, so a cancel arriving the
	// moment the server sees the ack is never lost.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	manifest := newPoolManifest(filepath.Base(dir), &spec)
	if err := WritePoolManifest(dir, manifest); err != nil {
		writeAck(out, SuperviseAck{StartError: err.Error()})
		return err
	}

	// The pid names this supervisor, not a child pgid: the pool supervisor is
	// its own session, so signalling it reaches nothing else.
	_ = WritePidFile(dir, os.Getpid())

	writeAck(out, SuperviseAck{Started: true, Pid: os.Getpid()})

	schedulePool(dir, &spec, manifest, sigCh)

	// Skipped runs must be visible, or a stopped pool looks identical to a
	// shorter pool.
	for i := range manifest.Runs {
		if manifest.Runs[i].Status == PoolRunPending {
			manifest.Runs[i].Status = PoolRunSkipped
		}
	}

	now := time.Now().UTC()
	manifest.FinishedAt = &now
	if err := WritePoolManifest(dir, manifest); err != nil {
		return err
	}

	RemovePidFile(dir)
	return nil
}

// newPoolManifest builds the initial manifest: the spec echo minus env, and one
// pending record per scheduled run, commands in listed order with repeats
// consecutive.
func newPoolManifest(id string, spec *PoolSpec) *PoolManifest {
	m := &PoolManifest{
		ID:          id,
		Commands:    make([][]string, len(spec.Commands)),
		Repeat:      spec.Repeat,
		Parallelism: spec.Parallelism,
		OnError:     spec.OnError,
		Cwd:         effectiveCwd(spec.Cwd),
		StartedAt:   time.Now().UTC(),
		Runs:        make([]PoolRunRecord, 0, len(spec.Commands)*spec.Repeat),
	}
	for i, c := range spec.Commands {
		m.Commands[i] = c.Argv
		for r := 0; r < spec.Repeat; r++ {
			m.Runs = append(m.Runs, PoolRunRecord{Command: i, Status: PoolRunPending})
		}
	}
	return m
}

// poolResult reports a finished member back to the scheduler loop.
type poolResult struct {
	job int
	dir string
}

// schedulePool runs the manifest's jobs through per-run supervisors, at most
// spec.Parallelism in flight, rewriting the manifest on every state change.
// It returns once every started member has finished; the caller marks jobs
// never started as skipped.
func schedulePool(dir string, spec *PoolSpec, manifest *PoolManifest, sigCh <-chan os.Signal) {
	total := len(manifest.Runs)
	workers := min(spec.Parallelism, total)

	results := make(chan poolResult)
	inFlight := make(map[int]string, workers)

	next := 0
	stopped := false

	// Mid-flight manifest writes are best-effort: a headless supervisor has
	// nowhere to report a transient write failure, and the final write in
	// supervisePool surfaces persistent ones.
	persist := func() { _ = WritePoolManifest(dir, manifest) }

	cancelInFlight := func() {
		for _, runID := range inFlight {
			_, _ = CancelRun(context.Background(), runID, CancelOptions{})
		}
	}

	noteFailure := func() {
		switch spec.OnError {
		case OnErrorStop:
			stopped = true
		case OnErrorKill:
			stopped = true
			cancelInFlight()
		}
	}

	spawnNext := func() {
		job := next
		next++
		rec := &manifest.Runs[job]
		cmd := &spec.Commands[rec.Command]

		run, err := RunSupervised(cmd.Argv, SuperviseOptions{
			Resolved: cmd.resolution(),
			Cwd:      spec.Cwd,
			Env:      spec.Env,
			Pool:     manifest.ID,
		})
		if err != nil {
			var sf *StartFailure
			if errors.As(err, &sf) {
				rec.RunID = sf.RunID
			}
			rec.Status = PoolRunStartError
			rec.StartError = err.Error()
			noteFailure()
			return
		}

		rec.RunID = run.ID
		rec.Status = PoolRunRunning
		inFlight[job] = run.ID

		go func(job int, run *CaptureRun) {
			<-run.Done
			results <- poolResult{job: job, dir: run.Dir}
		}(job, run)
	}

	for {
		for !stopped && next < total && len(inFlight) < workers {
			spawnNext()
			persist()
		}

		if len(inFlight) == 0 {
			return
		}

		select {
		case res := <-results:
			runID := inFlight[res.job]
			delete(inFlight, res.job)
			if finishPoolRun(&manifest.Runs[res.job], runID, res.dir) {
				noteFailure()
			}
			persist()
		case sig := <-sigCh:
			stopped = true
			if sig == syscall.SIGINT {
				cancelInFlight()
			}
		}
	}
}

// finishPoolRun records a finished member's outcome from its run directory and
// reports whether the run counts as a failure: a start failure, a non-zero
// exit, or a signal.
func finishPoolRun(rec *PoolRunRecord, runID, runDir string) bool {
	if meta, err := ReadMeta(runDir); err == nil {
		rec.Status = PoolRunFinished
		exitCode := meta.ExitCode
		rec.ExitCode = &exitCode
		rec.Signal = meta.Signal
		return meta.ExitCode != 0 || meta.Signal != nil
	}

	if dbg, err := ReadStartDebug(runDir); err == nil {
		rec.Status = PoolRunStartError
		rec.StartError = dbg.StartError
		return true
	}

	// No meta and no debug: the member's supervisor died without recording an
	// outcome, the abandoned-run state. Count it as a failure.
	rec.Status = PoolRunStartError
	rec.StartError = fmt.Sprintf("run %s abandoned: no outcome recorded", runID)
	return true
}
