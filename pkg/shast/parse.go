package shast

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"mvdan.cc/sh/v3/syntax"
)

// parseOptions holds the flags for `shast parse`.
type parseOptions struct {
	File string
	Lang string
	Pos  bool
	Flat bool
}

// NewParseCommand returns the `shast parse` subcommand. It parses a shell
// command string and prints the syntax tree as JSON: nested by default,
// mirroring the shell grammar, or a flat array of every simple command with
// --flat.
func NewParseCommand() *cobra.Command {
	opts := &parseOptions{}
	c := &cobra.Command{
		Use:   "parse [SCRIPT]",
		Short: "Parse a shell command and print its AST as JSON",
		Long: "Parse SCRIPT (or --file, or stdin) as a shell command and print the syntax tree as JSON.\n\n" +
			"By default the tree is nested, mirroring the shell grammar. --flat instead prints a bare\n" +
			"array of every simple command found anywhere in the tree, including inside command and\n" +
			"process substitutions, easier to walk with jq.",

		Args: cobra.MaximumNArgs(1),

		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: opts.run,
	}

	c.Flags().StringVarP(&opts.File, "file", "f", "", "read the script from a file instead of SCRIPT or stdin")
	c.Flags().StringVar(&opts.Lang, "lang", "bash", "shell dialect to parse as: bash, posix, mksh, bats, zsh")
	c.Flags().BoolVar(&opts.Pos, "pos", false, "include source position (offset/line/col) on every node")
	c.Flags().BoolVar(&opts.Flat, "flat", false, "emit a flat array of every simple command instead of the nested AST")

	return c
}

func (opts *parseOptions) run(cmd *cobra.Command, args []string) error {
	lang, err := parseLangVariant(opts.Lang)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "%v\n", err)
		return &ExitError{Code: 2}
	}

	src, err := opts.resolveInput(cmd, args, os.Stdin, term.IsTerminal(int(os.Stdin.Fd())))
	if err != nil {
		return err
	}

	f, parseErr := syntax.NewParser(syntax.Variant(lang)).Parse(strings.NewReader(src), "")
	if parseErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "parsing shell command: %v\n", parseErr)
		return &ExitError{Code: 2}
	}

	conv := &converter{src: src, withPos: opts.Pos}

	var payload any
	if opts.Flat {
		payload = conv.flatCommands(f)
	} else {
		payload = conv.file(f)
	}

	return writeJSON(cmd.OutOrStdout(), payload)
}

// resolveInput picks SCRIPT, --file, or stdin, in that order. stdin and
// stdinIsTerminal are threaded in explicitly so this stays testable without
// touching the real os.Stdin.
func (opts *parseOptions) resolveInput(cmd *cobra.Command, args []string, stdin io.Reader, stdinIsTerminal bool) (string, error) {
	if len(args) == 1 {
		if opts.File != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "specify SCRIPT or --file, not both")
			return "", &ExitError{Code: 2}
		}
		return args[0], nil
	}

	if opts.File != "" {
		data, err := os.ReadFile(opts.File)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "reading %s: %v\n", opts.File, err)
			return "", &ExitError{Code: 1}
		}
		return string(data), nil
	}

	if stdinIsTerminal {
		fmt.Fprintln(cmd.ErrOrStderr(), "no input: pass SCRIPT, --file, or pipe a script on stdin")
		return "", &ExitError{Code: 2}
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "reading stdin: %v\n", err)
		return "", &ExitError{Code: 1}
	}
	if len(data) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "no input: pass SCRIPT, --file, or pipe a script on stdin")
		return "", &ExitError{Code: 2}
	}

	return string(data), nil
}

func parseLangVariant(s string) (syntax.LangVariant, error) {
	switch s {
	case "bash":
		return syntax.LangBash, nil
	case "posix":
		return syntax.LangPOSIX, nil
	case "mksh":
		return syntax.LangMirBSDKorn, nil
	case "bats":
		return syntax.LangBats, nil
	case "zsh":
		return syntax.LangZsh, nil
	default:
		return 0, fmt.Errorf("unsupported --lang value: %q (supported: bash, posix, mksh, bats, zsh)", s)
	}
}

// writeJSON marshals v as indented JSON and writes it to w with a trailing
// newline. Unlike approvecmd's helper of the same name, it turns off HTML
// escaping: op fields here are full of &&, <, <<, >, >>, and the default
// &-style escaping makes them unreadable for no benefit, since this
// output is consumed by jq and human eyes, never embedded in HTML.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("marshalling output: %w", err)
	}
	return nil
}
