package cg

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// isValidRunID reports whether id has the right shape for a capture run ID:
// the Crockford base-32 alphabet, exactly runIDLen characters.
func isValidRunID(id string) bool {
	if len(id) != runIDLen {
		return false
	}
	for _, r := range id {
		if !strings.ContainsRune(runIDAlphabet, r) {
			return false
		}
	}
	return true
}

// resolveRunDir locates the per-run capture directory for id. It reports an
// ExitError with code 1 paired with a single-line stderr message for unknown
// IDs and for runs whose meta.json is missing or unreadable.
func resolveRunDir(cmd *cobra.Command, id string) (string, error) {
	dir := filepath.Join(CaptureRoot(), id)

	if !isValidRunID(id) {
		fmt.Fprintf(cmd.ErrOrStderr(), "unknown run id: %s\n", id)
		return "", &ExitError{Code: 1}
	}

	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		fmt.Fprintf(cmd.ErrOrStderr(), "unknown run id: %s\n", id)
		return "", &ExitError{Code: 1}
	}
	if err != nil {
		return "", fmt.Errorf("stat run dir: %w", err)
	}

	if _, err := os.Stat(filepath.Join(dir, MetaFilename)); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "incomplete run: %s (missing meta.json)\n", id)
		return "", &ExitError{Code: 1}
	}

	return dir, nil
}

// NewOutCommand returns the `cg out <ID>` subcommand. It prints the absolute
// path of the captured stdout file.
func NewOutCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "out <ID>",
		Short:         "Print the absolute path of a captured run's stdout file",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveRunDir(cmd, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), filepath.Join(dir, "stdout"))
			return nil
		},
	}
}

// NewErrCommand returns the `cg err <ID>` subcommand. It prints the absolute
// path of the captured stderr file.
func NewErrCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "err <ID>",
		Short:         "Print the absolute path of a captured run's stderr file",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveRunDir(cmd, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), filepath.Join(dir, "stderr"))
			return nil
		},
	}
}

// NewPathsCommand returns the `cg paths <ID>` subcommand. It prints the
// absolute paths of the captured stdout and stderr files, one per line,
// stdout first.
func NewPathsCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "paths <ID>",
		Short:         "Print the absolute paths of a captured run's stdout and stderr files",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveRunDir(cmd, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, filepath.Join(dir, "stdout"))
			fmt.Fprintln(out, filepath.Join(dir, "stderr"))
			return nil
		},
	}
}

// lsOptions holds flags for the `cg ls` subcommand.
type lsOptions struct {
	N int
}

// NewLsCommand returns the `cg ls` subcommand. It lists recent capture runs in
// most-recent-first order by directory mtime.
func NewLsCommand() *cobra.Command {
	opts := &lsOptions{}
	c := &cobra.Command{
		Use:           "ls",
		Short:         "List recent capture runs, most-recent-first",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          opts.run,
	}
	c.Flags().IntVarP(&opts.N, "limit", "n", 20, "maximum number of runs to list")
	return c
}

type lsRow struct {
	id    string
	mtime time.Time
	meta  *Meta
}

func (opts *lsOptions) run(cmd *cobra.Command, args []string) error {
	if opts.N <= 0 {
		return nil
	}

	root := CaptureRoot()
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading capture root: %w", err)
	}

	rows := make([]lsRow, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !isValidRunID(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		row := lsRow{id: name, mtime: info.ModTime()}
		if m, err := ReadMeta(filepath.Join(root, name)); err == nil {
			row.meta = m
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].mtime.After(rows[j].mtime)
	})

	if len(rows) > opts.N {
		rows = rows[:opts.N]
	}

	out := cmd.OutOrStdout()
	for _, r := range rows {
		fmt.Fprintln(out, formatLsRow(r))
	}
	return nil
}

func formatLsRow(r lsRow) string {
	if r.meta == nil {
		return fmt.Sprintf("%s\texit=?\t?\t?", r.id)
	}

	head := fmt.Sprintf("exit=%d", r.meta.ExitCode)
	if r.meta.Signal != nil {
		head = fmt.Sprintf("signal=%d", *r.meta.Signal)
	}

	dur := formatDuration(time.Duration(r.meta.DurationMs) * time.Millisecond)
	return fmt.Sprintf("%s\t%s\t%s\t%s", r.id, head, dur, escapeArgs(r.meta.Command))
}
