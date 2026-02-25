package cg

import (
	"fmt"

	"github.com/spf13/cobra"
)

// DefaultFormat is the default time prefix format: short time.
const DefaultFormat = "15:04:05 "

type Options struct {
	Format   string
	Capture  bool
	Buffered bool

	LogParse  string
	LogMsgKey string
	LogTSKey  string
	LogTSFmt  string
	LogFields string
}

// NewCommand creates the cg cobra command.
func NewCommand() *cobra.Command {
	opts := &Options{}
	c := &cobra.Command{
		Use:   "cg [flags] -- COMMAND [ARGS...]",
		Short: "Execute a command and annotate its output",
		Long:  "Execute a child command, annotating each line of stdout and stderr with a timestamp prefix and stream indicator.",

		SilenceErrors: true,
		SilenceUsage:  true,

		Args: cobra.MinimumNArgs(1),
		RunE: opts.run,
	}

	c.Flags().StringVar(&opts.Format, "format", DefaultFormat, "time prefix format (Go time.Format layout)")
	c.Flags().BoolVar(&opts.Capture, "capture", false, "capture child output to temporary files")
	c.Flags().BoolVar(&opts.Buffered, "buffered", false, "defer child output until command finishes, grouped by stream")
	c.Flags().StringVar(&opts.LogParse, "log-parse", "", "log line parser (\"json\", \"logfmt\")")
	c.Flags().StringVar(&opts.LogMsgKey, "log-message-key", "message", "JSON key for the log message")
	c.Flags().StringVar(&opts.LogTSKey, "log-timestamp-key", "timestamp", "JSON key for the timestamp (empty to disable)")
	c.Flags().StringVar(&opts.LogTSFmt, "log-timestamp-format", "", "timestamp format: rfc3339, unix-s, unix-ms (empty for auto-detect)")
	c.Flags().StringVar(&opts.LogFields, "log-fields", "", "comma-separated JSON keys to append, or \"*\" for all")

	return c
}

// logDependentFlags are flags that require --log-parse to be set.
var logDependentFlags = []string{
	"log-message-key",
	"log-timestamp-key",
	"log-timestamp-format",
	"log-fields",
}

func (opts *Options) validateFlags(cmd *cobra.Command) error {
	if opts.LogParse == "" {
		for _, name := range logDependentFlags {
			if cmd.Flags().Changed(name) {
				return fmt.Errorf("--%s requires --log-parse", name)
			}
		}
		return nil
	}

	switch opts.LogParse {
	case "json", "logfmt":
	default:
		return fmt.Errorf("unsupported --log-parse value: %q (supported: json, logfmt)", opts.LogParse)
	}

	switch opts.LogTSFmt {
	case "", "rfc3339", "unix-s", "unix-ms":
	default:
		return fmt.Errorf("unsupported --log-timestamp-format value: %q (supported: rfc3339, unix-s, unix-ms)", opts.LogTSFmt)
	}

	return nil
}
