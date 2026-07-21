package model

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// noteListResult is the --json shape for `cg note ls` and `cg note grep`. It
// mirrors the MCP noteListOutput so both surfaces render identical JSON.
type noteListResult struct {
	Notes []Note `json:"notes"`
}

// NewNoteCommand returns the `cg note` parent command grouping the note
// subcommands over the shared store under $TMPDIR/cg/notes.
func NewNoteCommand() *cobra.Command {
	c := &cobra.Command{
		Use:           "note",
		Short:         "Record and inspect free-form notes",
		Long:          "Record free-form notes that outlive a single run, and list, search, or delete them. Notes are shared with the cg_note_* MCP tools.",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	c.AddCommand(newNoteAddCommand())
	c.AddCommand(newNoteLsCommand())
	c.AddCommand(newNoteGrepCommand())
	c.AddCommand(newNoteRmCommand())

	return c
}

// noteAddOptions holds flags for `cg note add`.
type noteAddOptions struct {
	Keys []string
	JSON bool
}

func newNoteAddCommand() *cobra.Command {
	opts := &noteAddOptions{}
	c := &cobra.Command{
		Use:           "add [message]",
		Short:         "Record a note from an argument or stdin",
		Long:          "Record a note. The message is the positional argument, or is read from stdin when no argument is given. Repeat -k to attach key=value tags.",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          opts.run,
	}
	c.Flags().StringArrayVarP(&opts.Keys, "key", "k", nil, "key=value tag to attach; repeatable")
	c.Flags().BoolVar(&opts.JSON, "json", false, "print the created note as JSON")
	return c
}

func (opts *noteAddOptions) run(cmd *cobra.Command, args []string) error {
	message, err := readNoteMessage(cmd, args)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return &ExitError{Code: 2}
	}

	keys, err := parseNoteKeys(opts.Keys)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return &ExitError{Code: 2}
	}

	n, err := AddNote(message, keys)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return &ExitError{Code: 2}
	}

	if opts.JSON {
		return writeJSON(cmd.OutOrStdout(), n)
	}
	fmt.Fprintln(cmd.OutOrStdout(), n.ID)
	return nil
}

// readNoteMessage resolves the note message from the single positional argument
// or, when none is given, from stdin. Piped stdin has its trailing newlines
// trimmed. A terminal stdin with no argument is a usage error, since there is
// nothing to read.
func readNoteMessage(cmd *cobra.Command, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}

	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return "", errors.New("no message: provide an argument or pipe stdin")
	}

	data, err := io.ReadAll(io.LimitReader(in, maxNoteMessageBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading message from stdin: %w", err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// parseNoteKeys turns repeated key=value flag values into a keys map. A value
// with no '=' is stored as a key with an empty value. The store enforces the
// key count and length bounds.
func parseNoteKeys(vals []string) (map[string]string, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	keys := make(map[string]string, len(vals))
	for _, v := range vals {
		k, val, _ := strings.Cut(v, "=")
		if k == "" {
			return nil, fmt.Errorf("invalid -k value %q: key must not be empty", v)
		}
		keys[k] = val
	}
	return keys, nil
}

// noteLsOptions holds flags for `cg note ls`.
type noteLsOptions struct {
	Key   string
	Limit int
	JSON  bool
}

func newNoteLsCommand() *cobra.Command {
	opts := &noteLsOptions{}
	c := &cobra.Command{
		Use:           "ls",
		Short:         "List notes, newest-first",
		Long:          "List notes newest-first. Narrow with -k key (existence) or -k key=value (exact value).",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          opts.run,
	}
	c.Flags().StringVarP(&opts.Key, "key", "k", "", "filter to notes carrying key, or key=value")
	c.Flags().IntVarP(&opts.Limit, "limit", "n", 20, "maximum number of notes to list")
	c.Flags().BoolVar(&opts.JSON, "json", false, "print notes as JSON")
	return c
}

func (opts *noteLsOptions) run(cmd *cobra.Command, args []string) error {
	if opts.Limit <= 0 {
		return nil
	}

	listOpts := ListNoteOptions{Limit: opts.Limit}
	if opts.Key != "" {
		k, v, hasValue := strings.Cut(opts.Key, "=")
		listOpts.Key = k
		listOpts.Value = v
		listOpts.HasValue = hasValue
	}

	notes, err := ListNotes(listOpts)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return &ExitError{Code: 2}
	}
	return writeNotes(cmd.OutOrStdout(), notes, opts.JSON)
}

// noteGrepOptions holds flags for `cg note grep`.
type noteGrepOptions struct {
	Text            string
	Pattern         string
	CaseInsensitive bool
	JSON            bool
}

func newNoteGrepCommand() *cobra.Command {
	opts := &noteGrepOptions{}
	c := &cobra.Command{
		Use:           "grep",
		Short:         "Search note bodies and print matching notes",
		Long:          "Search note message bodies and print whole matching notes newest-first. Supply exactly one of --text (fixed string) or --pattern (RE2 regex).",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          opts.run,
	}
	c.Flags().StringVar(&opts.Text, "text", "", "fixed-string substring to match (mutually exclusive with --pattern)")
	c.Flags().StringVar(&opts.Pattern, "pattern", "", "RE2 regular expression to match (mutually exclusive with --text)")
	c.Flags().BoolVarP(&opts.CaseInsensitive, "ignore-case", "i", false, "fold case when matching")
	c.Flags().BoolVar(&opts.JSON, "json", false, "print notes as JSON")
	return c
}

func (opts *noteGrepOptions) run(cmd *cobra.Command, args []string) error {
	notes, err := GrepNotes(GrepNoteOptions{
		Text:            opts.Text,
		Pattern:         opts.Pattern,
		CaseInsensitive: opts.CaseInsensitive,
	})
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return &ExitError{Code: 2}
	}
	return writeNotes(cmd.OutOrStdout(), notes, opts.JSON)
}

func newNoteRmCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "rm <ID>...",
		Short:         "Delete notes by ID",
		Args:          cobra.MinimumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          runNoteRm,
	}
}

func runNoteRm(cmd *cobra.Command, args []string) error {
	// DeleteNote joins the ID into a path with no sanitization, so validate
	// every untrusted ID before touching the filesystem, and reject the whole
	// invocation if any is malformed.
	for _, id := range args {
		if !IsValidRunID(id) {
			fmt.Fprintf(cmd.ErrOrStderr(), "invalid note id: %s\n", id)
			return &ExitError{Code: 2}
		}
	}

	out := cmd.OutOrStdout()
	anyUnknown := false
	for _, id := range args {
		err := DeleteNote(id)
		if errors.Is(err, ErrUnknownNoteID) {
			fmt.Fprintf(cmd.ErrOrStderr(), "unknown note id: %s\n", id)
			anyUnknown = true
			continue
		}
		if err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return &ExitError{Code: 2}
		}
		fmt.Fprintln(out, id)
	}

	if anyUnknown {
		return &ExitError{Code: 1}
	}
	return nil
}

// writeNotes renders notes as JSON or as the human block format.
func writeNotes(w io.Writer, notes []Note, asJSON bool) error {
	if asJSON {
		if notes == nil {
			notes = []Note{}
		}
		return writeJSON(w, noteListResult{Notes: notes})
	}
	for _, n := range notes {
		if err := formatNote(w, n); err != nil {
			return err
		}
	}
	return nil
}

// formatNote writes one note as a header line of ID, timestamp, and sorted
// key=value tags, followed by the message indented two spaces, then a blank
// line separating it from the next note.
func formatNote(w io.Writer, n Note) error {
	header := fmt.Sprintf("%s  %s", n.ID, n.CreatedAt.UTC().Format(time.RFC3339))
	if tags := formatNoteKeys(n.Keys); tags != "" {
		header += "  " + tags
	}
	if _, err := fmt.Fprintln(w, header); err != nil {
		return err
	}
	for _, line := range strings.Split(n.Message, "\n") {
		if _, err := fmt.Fprintf(w, "  %s\n", line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// formatNoteKeys renders a note's keys as space-separated key=value pairs sorted
// by key, so the output is deterministic.
func formatNoteKeys(keys map[string]string) string {
	if len(keys) == 0 {
		return ""
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)

	pairs := make([]string, 0, len(names))
	for _, k := range names {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, keys[k]))
	}
	return strings.Join(pairs, " ")
}
