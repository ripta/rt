package model

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

const (
	statsOutputTable = "table"
	statsOutputJSON  = "json"
)

// statsOptions holds flags for the `cg stats` subcommand.
type statsOptions struct {
	Cwd      string
	Since    string
	ExitCode string
	Samples  int
	Pool     string
	Output   string
}

// NewStatsCommand returns the `cg stats` subcommand. It reports how long earlier runs of the
// command took, for the exact argv and for each shorter prefix with samples.
func NewStatsCommand() *cobra.Command {
	opts := &statsOptions{}
	c := &cobra.Command{
		Use:   "stats [flags] -- COMMAND [ARGS...]",
		Short: "Report how long earlier runs of a command took",
		Long: "Report durations of earlier runs of a command from the duration history. The first entry is " +
			"the exact command. Each shorter prefix of it that has samples follows, longest first.",

		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: opts.run,
	}

	c.Flags().StringVar(&opts.Cwd, "cwd", "", "working `DIR` the runs started in (default: the current directory)")
	c.Flags().StringVar(&opts.Since, "since", "", "only count runs started at or after TIME: a duration ago (e.g. 4h, 7d), an RFC3339 timestamp, or a YYYY-MM-DD date")
	c.Flags().StringVar(&opts.ExitCode, "exit-code", "0", "only count runs with this exit code: N (equals), !=N, >=N, >N, <N, or <=N; signaled runs never count")
	c.Flags().IntVar(&opts.Samples, "samples", DefaultStatsSamples, fmt.Sprintf("count at most this many of the most recent runs per entry (1 to %d)", MaxStatsSamples))
	c.Flags().StringVar(&opts.Pool, "pool", StatsPoolAny, "pool membership: any, none (standalone runs only), or only (pooled runs only)")
	c.Flags().StringVarP(&opts.Output, "output", "o", statsOutputTable, "output format: table or json")

	// Flags must precede the command, as with `cg run`, so the command's own flags pass through.
	c.Flags().SetInterspersed(false)

	return c
}

func (opts *statsOptions) run(cmd *cobra.Command, args []string) error {
	usage := func(format string, a ...any) error {
		fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", a...)

		return &ExitError{Code: 2}
	}

	if len(args) == 0 {
		return usage("cg stats requires a command: cg stats [flags] -- COMMAND [ARGS...]")
	}

	switch opts.Output {
	case statsOutputTable, statsOutputJSON:
	default:
		return usage("invalid --output %q: want table|json", opts.Output)
	}

	filters := StatsFilters{
		Cwd:      opts.Cwd,
		Since:    opts.Since,
		ExitCode: opts.ExitCode,
		Samples:  opts.Samples,
		Pool:     opts.Pool,
	}

	query, err := filters.Query(args, time.Now())
	if err != nil {
		var filterErr *StatsFilterError
		if errors.As(err, &filterErr) {
			return usage("invalid --%s: %v", strings.ReplaceAll(filterErr.Field, "_", "-"), filterErr.Err)
		}

		return usage("%v", err)
	}

	history, err := OpenHistory()
	if err != nil {
		return fmt.Errorf("opening history: %w", err)
	}

	result, err := history.QueryStats(query)
	if errors.Is(err, ErrInvalidStatsQuery) {
		return usage("%v", err)
	}
	if err != nil {
		return fmt.Errorf("querying history: %w", err)
	}

	if opts.Output == statsOutputJSON {
		return writeJSON(cmd.OutOrStdout(), result)
	}

	return writeStatsTable(cmd.OutOrStdout(), result)
}

func writeStatsTable(w io.Writer, result *StatsResult) error {
	if _, err := fmt.Fprintf(w, "cwd: %s\n\n", result.Cwd); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MATCH\tPOOL\tCOUNT\tMIN\tP50\tP90\tMAX\tAVG\tLAST\tLAST AT\tCOMMAND")

	for _, m := range result.Matches {
		s := m.Stats

		command := m.Command
		if m.MatchType == StatsMatchPrefix {
			command = m.CommandPrefix
		}

		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			m.MatchType,
			statsPoolLabel(m),
			s.Count,
			statsDuration(s, s.MinMs),
			statsDuration(s, s.P50Ms),
			statsDuration(s, s.P90Ms),
			statsDuration(s, s.MaxMs),
			statsDuration(s, s.AvgMs),
			statsDuration(s, s.LastMs),
			statsLastAt(s),
			formatLsCommand(command),
		)
	}

	return tw.Flush()
}

func statsPoolLabel(m StatsMatch) string {
	switch {
	case !m.Pool:
		return "-"
	case m.Parallelism == 0:
		return "pool"
	default:
		return "pool x" + strconv.Itoa(m.Parallelism)
	}
}

func statsDuration(s DurationStats, ms int64) string {
	if s.Count == 0 {
		return "-"
	}

	return FormatDuration(time.Duration(ms) * time.Millisecond)
}

func statsLastAt(s DurationStats) string {
	if s.LastAt == nil {
		return "-"
	}

	return s.LastAt.Format(time.RFC3339)
}
