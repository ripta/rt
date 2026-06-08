package cg

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// pruneOptions holds flags for the `cg prune` subcommand.
type pruneOptions struct {
	Keep      int
	OlderThan string
	DryRun    bool
}

// NewPruneCommand returns the `cg prune` subcommand. It removes capture run
// directories from $TMPDIR/cg/, keeping recent runs or evicting by age.
func NewPruneCommand() *cobra.Command {
	opts := &pruneOptions{}
	c := &cobra.Command{
		Use:           "prune",
		Short:         "Remove old capture runs",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          opts.run,
	}
	c.Flags().IntVar(&opts.Keep, "keep", 50, "keep the N most recent capture runs by mtime")
	c.Flags().StringVar(&opts.OlderThan, "older-than", "", "evict runs whose mtime is older than DUR (e.g., 7d, 2h)")
	c.Flags().BoolVar(&opts.DryRun, "dry-run", false, "print what would be removed without removing")
	return c
}

// ParsePruneDuration parses a duration string, accepting the Go
// time.ParseDuration grammar plus single-unit Nd (days) and Nw (weeks)
// suffixes. Mixed forms like "7d12h" are not supported; they fall through to
// time.ParseDuration and error.
func ParsePruneDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	last := s[len(s)-1]
	if last == 'd' || last == 'w' {
		prefix := s[:len(s)-1]
		n, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		if n < 0 {
			return 0, fmt.Errorf("invalid duration %q: negative", s)
		}
		mult := time.Duration(24) * time.Hour
		if last == 'w' {
			mult *= 7
		}
		return time.Duration(n) * mult, nil
	}
	return time.ParseDuration(s)
}

// PruneOptions controls which capture runs PruneRuns considers for eviction
// and whether the removal is actually performed.
type PruneOptions struct {
	// Keep is the number of most-recent (by mtime) runs to retain. Ignored
	// when UseOlderThan is true.
	Keep int
	// OlderThan, when UseOlderThan is true, evicts runs whose mtime is
	// before now-OlderThan.
	OlderThan time.Duration
	// UseOlderThan selects age-based eviction over count-based.
	UseOlderThan bool
	// DryRun returns the list of IDs that would be removed without
	// touching the filesystem.
	DryRun bool
}

// pruneCandidate is a single run dir under consideration for eviction.
type pruneCandidate struct {
	id    string
	dir   string
	mtime time.Time
}

// PruneRuns evicts capture runs from CaptureRoot() per opts. It returns the
// IDs that were removed (or, under DryRun, would have been removed) in
// eviction order. A missing CaptureRoot is not an error.
func PruneRuns(opts PruneOptions) ([]string, error) {
	root := CaptureRoot()
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading capture root: %w", err)
	}

	candidates := make([]pruneCandidate, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !IsValidRunID(name) {
			continue
		}
		dir := filepath.Join(root, name)
		if _, err := os.Stat(filepath.Join(dir, MetaFilename)); err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, pruneCandidate{id: name, dir: dir, mtime: info.ModTime()})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].mtime.After(candidates[j].mtime)
	})

	var toRemove []pruneCandidate
	if opts.UseOlderThan {
		cutoff := time.Now().Add(-opts.OlderThan)
		for _, c := range candidates {
			if c.mtime.Before(cutoff) {
				toRemove = append(toRemove, c)
			}
		}
	} else if len(candidates) > opts.Keep {
		toRemove = candidates[opts.Keep:]
	}

	removed := make([]string, 0, len(toRemove))
	for _, c := range toRemove {
		if !opts.DryRun {
			if err := os.RemoveAll(c.dir); err != nil {
				return removed, fmt.Errorf("removing %s: %w", c.dir, err)
			}
		}
		removed = append(removed, c.id)
	}
	return removed, nil
}

func (opts *pruneOptions) run(cmd *cobra.Command, args []string) error {
	keepSet := cmd.Flags().Changed("keep")
	if keepSet && opts.OlderThan != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "--keep and --older-than are mutually exclusive")
		return &ExitError{Code: 2}
	}

	pruneOpts := PruneOptions{Keep: opts.Keep, DryRun: opts.DryRun}
	if opts.OlderThan != "" {
		d, err := ParsePruneDuration(opts.OlderThan)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "invalid --older-than: %v\n", err)
			return &ExitError{Code: 2}
		}
		pruneOpts.UseOlderThan = true
		pruneOpts.OlderThan = d
	}

	removed, err := PruneRuns(pruneOpts)
	out := cmd.OutOrStdout()
	for _, id := range removed {
		fmt.Fprintln(out, id)
	}
	return err
}
