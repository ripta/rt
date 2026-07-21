package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// DefaultWaitTimeout is how long `cg wait` blocks before reporting
// finished=false when no --timeout is given.
const DefaultWaitTimeout = 60 * time.Second

// WaitResult is the `cg wait` output. Finished is always present; the embedded
// meta fields are populated only when Finished is true.
type WaitResult struct {
	ID       string `json:"id"`
	Finished bool   `json:"finished"`
	metaFields
}

// WaitRun blocks until run id finishes, timeout elapses, or ctx is cancelled.
// It returns Finished=true with meta fields on completion and Finished=false on
// timeout. A run that failed to start counts as finished but carries no meta.
// An unknown ID surfaces as ErrUnknownRunID.
func WaitRun(ctx context.Context, id string, timeout time.Duration) (WaitResult, error) {
	dir, err := LookupRunDir(id)
	switch {
	case errors.Is(err, ErrUnknownRunID):
		return WaitResult{}, err
	case err == nil:
		return finishedWaitResult(id, dir)
	case errors.Is(err, ErrFailedRun):
		return WaitResult{ID: id, Finished: true}, nil
	case !errors.Is(err, ErrIncompleteRun):
		return WaitResult{}, err
	}

	finished, err := awaitFinish(ctx, id, timeout)
	if err != nil {
		return WaitResult{}, err
	}
	if !finished {
		return WaitResult{ID: id, Finished: false}, nil
	}

	dir, err = LookupRunDir(id)
	if errors.Is(err, ErrFailedRun) {
		return WaitResult{ID: id, Finished: true}, nil
	}
	if err != nil {
		return WaitResult{}, err
	}
	return finishedWaitResult(id, dir)
}

// finishedWaitResult reads meta.json from dir and builds the populated wait
// result. A read error surfaces rather than a finished=false response: the run
// clearly transitioned to finished.
func finishedWaitResult(id, dir string) (WaitResult, error) {
	m, err := ReadMeta(dir)
	if err != nil {
		return WaitResult{}, fmt.Errorf("reading meta.json for %s: %w", id, err)
	}
	return WaitResult{ID: m.ID, Finished: true, metaFields: metaFieldsFrom(m)}, nil
}

// waitCmdOptions holds flags for the `cg wait` subcommand.
type waitCmdOptions struct {
	Timeout time.Duration
}

// NewWaitCommand returns the `cg wait <ID>` subcommand. It blocks until the run
// finishes or --timeout elapses, then prints the result as indented JSON.
func NewWaitCommand() *cobra.Command {
	opts := &waitCmdOptions{}
	c := &cobra.Command{
		Use:           "wait <ID>",
		Short:         "Block until a run finishes or a timeout elapses, then print JSON",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := WaitRun(cmd.Context(), args[0], opts.Timeout)
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
	c.Flags().DurationVar(&opts.Timeout, "timeout", DefaultWaitTimeout, "how long to block before reporting finished=false")
	return c
}
