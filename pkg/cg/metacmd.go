package cg

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// metaFields holds the meta.json-derived fields shared by the meta and wait
// outputs. Every field is pointer- or slice-typed with omitempty so it
// collapses out of the JSON when the run has not finished.
type metaFields struct {
	Command     []string   `json:"command,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	DurationMs  *int64     `json:"duration_ms,omitempty"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	Signal      *int       `json:"signal,omitempty"`
	StdoutLines *int64     `json:"stdout_lines,omitempty"`
	StderrLines *int64     `json:"stderr_lines,omitempty"`
}

// metaFieldsFrom builds metaFields populated from m.
func metaFieldsFrom(m *Meta) metaFields {
	started := m.StartedAt
	finished := m.FinishedAt
	dur := m.DurationMs
	exit := m.ExitCode
	stdoutLines := m.StdoutLines
	stderrLines := m.StderrLines
	f := metaFields{
		Command:     m.Command,
		StartedAt:   &started,
		FinishedAt:  &finished,
		DurationMs:  &dur,
		ExitCode:    &exit,
		StdoutLines: &stdoutLines,
		StderrLines: &stderrLines,
	}
	if m.Signal != nil {
		sig := *m.Signal
		f.Signal = &sig
	}
	return f
}

// MetaResult is the `cg meta` output. State is always populated; the embedded
// meta fields are populated only for finished runs; Debug is populated only for
// runs that failed to start.
type MetaResult struct {
	ID    string      `json:"id"`
	State string      `json:"state"`
	Debug *StartDebug `json:"debug,omitempty"`
	metaFields
}

// RunMeta resolves the state and meta.json fields for run id. Finished runs
// return state "finished" with all meta fields. In-flight runs return state
// "running". Runs that failed to start return state "failed" with a debug
// payload. An unknown ID surfaces as ErrUnknownRunID.
func RunMeta(id string) (MetaResult, error) {
	dir, err := LookupRunDir(id)
	switch {
	case errors.Is(err, ErrUnknownRunID):
		return MetaResult{}, err
	case errors.Is(err, ErrIncompleteRun):
		return MetaResult{ID: id, State: RunStateRunning}, nil
	case errors.Is(err, ErrFailedRun):
		dbg, _ := ReadStartDebug(dir)
		return MetaResult{ID: id, State: RunStateFailed, Debug: dbg}, nil
	case err != nil:
		return MetaResult{}, err
	}

	m, err := ReadMeta(dir)
	if err != nil {
		return MetaResult{}, fmt.Errorf("reading meta.json for %s: %w", id, err)
	}
	return MetaResult{ID: m.ID, State: RunStateFinished, metaFields: metaFieldsFrom(m)}, nil
}

// NewMetaCommand returns the `cg meta <ID>` subcommand. It prints the run state
// and meta.json fields as indented JSON.
func NewMetaCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "meta <ID>",
		Short:         "Print a run's state and meta.json fields as JSON",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := RunMeta(args[0])
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
}
