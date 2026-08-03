package model

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

// pruneCandidate is a single directory under consideration for eviction: a run
// dir, or a pool dir carrying the member IDs its manifest names. A pool and its
// members are one unit and count as one candidate.
type pruneCandidate struct {
	id      string
	dir     string
	mtime   time.Time
	members []string
}

// PruneRuns evicts capture runs from CaptureRoot() per opts. It returns the
// IDs that were removed (or, under DryRun, would have been removed) in
// eviction order. A missing CaptureRoot is not an error.
//
// A pool and its manifest-named members are evicted as one unit counting once
// against Keep. Members are never evicted individually while their pool
// directory exists; a member whose pool is gone is an orphan and evicts like
// any run. A live pool, or a still-live member under a dead one, is skipped.
func PruneRuns(opts PruneOptions) ([]string, error) {
	root := CaptureRoot()
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading capture root: %w", err)
	}

	// First pass: classify dirs and collect every pool-named member, so member
	// protection is complete before candidates are chosen. Members of dead
	// pools ride their unit; members of live pools are skipped outright.
	type scanned struct {
		id    string
		dir   string
		mtime time.Time
		pool  *PoolManifest
	}
	items := make([]scanned, 0, len(entries))
	protected := make(map[string]struct{})
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !IsValidRunID(name) {
			continue
		}
		dir := filepath.Join(root, name)

		var pool *PoolManifest
		if _, err := os.Stat(filepath.Join(dir, MetaFilename)); err != nil {
			if m, perr := ReadPoolManifest(dir); perr == nil {
				pool = m
				for _, id := range m.MemberIDs() {
					protected[id] = struct{}{}
				}
				if PoolState(dir, m) == PoolStateRunning {
					continue
				}
			} else if !metaLessDirEvictable(dir) {
				continue
			}
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, scanned{id: name, dir: dir, mtime: info.ModTime(), pool: pool})
	}

	candidates := make([]pruneCandidate, 0, len(items))
	for _, it := range items {
		c := pruneCandidate{id: it.id, dir: it.dir, mtime: it.mtime}
		if it.pool != nil {
			c.members = it.pool.MemberIDs()
		} else if _, ok := protected[it.id]; ok {
			continue
		}
		candidates = append(candidates, c)
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
		ids, err := evictCandidate(c, opts.DryRun)
		removed = append(removed, ids...)
		if err != nil {
			return removed, err
		}
	}
	return removed, nil
}

// evictCandidate removes one candidate and returns the IDs it evicted. For a
// pool unit, members go first and the pool dir last, so a partial failure
// leaves the manifest behind for a retry; the returned IDs list the pool
// before its members on success. Manifest entries whose member dir is already
// gone are silently tolerated.
func evictCandidate(c pruneCandidate, dryRun bool) ([]string, error) {
	root := CaptureRoot()

	var members []string
	for _, id := range c.members {
		dir := filepath.Join(root, id)
		if !memberEvictable(dir) {
			continue
		}
		if !dryRun {
			if err := os.RemoveAll(dir); err != nil {
				return members, fmt.Errorf("removing %s: %w", dir, err)
			}
		}
		members = append(members, id)
	}

	if !dryRun {
		if err := os.RemoveAll(c.dir); err != nil {
			return members, fmt.Errorf("removing %s: %w", c.dir, err)
		}
	}
	return append([]string{c.id}, members...), nil
}

// memberEvictable reports whether a pool member's dir exists and is safe to
// remove: finished (meta.json present) or dead. A still-live member under a
// dead pool survives the unit and becomes an orphan, prunable on its own once
// it dies.
func memberEvictable(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, MetaFilename)); err == nil {
		return true
	}
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	return metaLessDirEvictable(dir)
}

// metaLessDirEvictable reports whether a meta-less run dir carries no evidence
// of a live process. A released lock is abandoned: the supervisor died before
// writing meta.json. A missing lock file with no start.json is unknown: no
// liveness signal was ever recorded, which only happens when the supervisor
// died before acquiring its lock, since that lock is the first thing it does.
// Both are evictable. A held lock, or a missing lock file with a surviving
// start.json (a shell-path run, presumed running since nothing else records
// its liveness), is not.
func metaLessDirEvictable(dir string) bool {
	if LockFileExists(dir) {
		return RunLockReleased(dir)
	}
	_, err := ReadStartInfo(dir)
	return err != nil
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
