package cg

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// maxSignalNumber bounds numeric signal inputs. Real signal numbers fit well
// under this; the cap rejects obvious garbage without enumerating every
// platform's signal table.
const maxSignalNumber = 64

// ParseSignal maps a signal name or numeric string onto a syscall.Signal. An
// empty input returns def. Accepted names are SIGTERM, SIGINT, and SIGKILL;
// numeric values in (0, maxSignalNumber] are accepted directly, which covers
// signals like SIGQUIT without enumerating every name.
func ParseSignal(name string, def syscall.Signal) (syscall.Signal, error) {
	s := strings.TrimSpace(name)
	if s == "" {
		return def, nil
	}
	switch strings.ToUpper(s) {
	case "SIGTERM":
		return syscall.SIGTERM, nil
	case "SIGINT":
		return syscall.SIGINT, nil
	case "SIGKILL":
		return syscall.SIGKILL, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 || n > maxSignalNumber {
			return 0, fmt.Errorf("numeric signal out of range: %d", n)
		}
		return syscall.Signal(n), nil
	}
	return 0, fmt.Errorf("unsupported signal: %q (want SIGTERM, SIGINT, SIGKILL, or a number)", name)
}

// CancelOptions configures a CancelRun. Signal defaults to SIGTERM when empty.
// EscalateAfter > 0 waits that long for the child to exit, then sends
// EscalateSignal (default SIGKILL) if it is still running.
type CancelOptions struct {
	Signal         string
	EscalateAfter  time.Duration
	EscalateSignal string
}

// CancelResult is the `cg cancel` output. EscalateSignal is present only when
// an escalation signal was actually sent.
type CancelResult struct {
	ID             string `json:"id"`
	Signaled       bool   `json:"signaled"`
	Signal         int    `json:"signal"`
	Escalated      bool   `json:"escalated"`
	EscalateSignal int    `json:"escalate_signal,omitempty"`
	Finished       bool   `json:"finished"`
}

// CancelRun signals the process group of run id. An already-finished or
// already-gone run returns {signaled: false, finished: true} without error. An
// unknown ID surfaces as ErrUnknownRunID. When EscalateAfter > 0, it waits up
// to that long for the child to exit and sends EscalateSignal if it is still
// running.
func CancelRun(ctx context.Context, id string, opts CancelOptions) (CancelResult, error) {
	sig, err := ParseSignal(opts.Signal, syscall.SIGTERM)
	if err != nil {
		return CancelResult{}, fmt.Errorf("signal: %w", err)
	}
	escSig, err := ParseSignal(opts.EscalateSignal, syscall.SIGKILL)
	if err != nil {
		return CancelResult{}, fmt.Errorf("escalate_signal: %w", err)
	}

	out := CancelResult{ID: id, Signal: int(sig)}

	dir, lerr := LookupRunDir(id)
	switch {
	case errors.Is(lerr, ErrUnknownRunID):
		return CancelResult{}, lerr
	case lerr == nil:
		// Finished run: the thing you wanted dead is already dead.
		out.Finished = true
		return out, nil
	case errors.Is(lerr, ErrFailedRun):
		// Child never started; nothing to cancel.
		out.Finished = true
		return out, nil
	case !errors.Is(lerr, ErrIncompleteRun):
		return CancelResult{}, lerr
	}

	pid, perr := ReadPidFile(dir)
	if perr != nil {
		return CancelResult{}, fmt.Errorf("cannot cancel %s: no pid recorded for this run: %w", id, perr)
	}

	if kerr := syscall.Kill(-pid, sig); kerr != nil {
		if errors.Is(kerr, syscall.ESRCH) {
			out.Finished = true
			return out, nil
		}
		return CancelResult{}, fmt.Errorf("signalling %s: %w", id, kerr)
	}
	out.Signaled = true

	if opts.EscalateAfter <= 0 {
		return out, nil
	}

	finished, werr := awaitFinish(ctx, id, opts.EscalateAfter)
	if werr != nil {
		return CancelResult{}, werr
	}
	if finished {
		out.Finished = true
		return out, nil
	}

	if kerr := syscall.Kill(-pid, escSig); kerr != nil && !errors.Is(kerr, syscall.ESRCH) {
		return CancelResult{}, fmt.Errorf("escalating %s: %w", id, kerr)
	}
	out.Escalated = true
	out.EscalateSignal = int(escSig)
	return out, nil
}

// cancelCmdOptions holds flags for the `cg cancel` subcommand.
type cancelCmdOptions struct {
	Signal         string
	EscalateAfter  time.Duration
	EscalateSignal string
}

// NewCancelCommand returns the `cg cancel <ID>` subcommand. It signals a run's
// process group and prints the outcome as indented JSON.
func NewCancelCommand() *cobra.Command {
	opts := &cancelCmdOptions{}
	c := &cobra.Command{
		Use:           "cancel <ID>",
		Short:         "Signal a run's process group and print the outcome as JSON",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := CancelRun(cmd.Context(), args[0], CancelOptions{
				Signal:         opts.Signal,
				EscalateAfter:  opts.EscalateAfter,
				EscalateSignal: opts.EscalateSignal,
			})
			if errors.Is(err, ErrUnknownRunID) {
				fmt.Fprintf(cmd.ErrOrStderr(), "unknown run id: %s\n", args[0])
				return &ExitError{Code: 1}
			}
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), res)
		},
	}
	c.Flags().StringVar(&opts.Signal, "signal", "", "signal to send to the run's process group (SIGTERM default, SIGINT, SIGKILL, or a number)")
	c.Flags().DurationVar(&opts.EscalateAfter, "escalate-after", 0, "wait this long for the child to exit, then send --escalate-signal if still running")
	c.Flags().StringVar(&opts.EscalateSignal, "escalate-signal", "", "signal to send if the child is still running after --escalate-after (SIGKILL default)")
	return c
}
