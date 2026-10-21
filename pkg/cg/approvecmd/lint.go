package approvecmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/cg/approve"
	"github.com/ripta/rt/pkg/cg/model"
)

// lintOptions holds the flags for `cg lint`.
type lintOptions struct {
	ProjectConfig []string
	Output        string
}

// lintResult is the `cg lint --output json` payload.
type lintResult struct {
	Global  lintLayerResult  `json:"global"`
	Project lintLayerResult  `json:"project"`
	Merged  lintMergedResult `json:"merged"`
}

// lintLayerResult is one layer's lint outcome: whether the file exists, and
// every problem found in it, flattened out of the layer's joined error.
type lintLayerResult struct {
	Path     string   `json:"path"`
	Present  bool     `json:"present"`
	Issues   []string `json:"issues,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// lintMergedResult is the outcome of the pass over the merged ruleset. The pass is
// skipped, with the reason in Note, when either layer failed to parse.
type lintMergedResult struct {
	Skipped  bool     `json:"skipped,omitempty"`
	Note     string   `json:"note,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

const lintMergedSkipNote = "skipped: a layer failed to parse"

// NewLintCommand returns the `cg lint` subcommand. It validates the global and
// project approval rules files without evaluating any command against them,
// reporting every problem in each file rather than stopping at the first.
func NewLintCommand() *cobra.Command {
	opts := &lintOptions{}
	c := &cobra.Command{
		Use:   "lint [flags]",
		Short: "Validate the cg_run approval rules files",
		Long: "Validate the global and project approval rules files for syntax and schema\n" +
			"errors, without evaluating any command against them.\n\n" +
			"Exit codes: 0 both files are valid (or absent), 1 one or more files have\n" +
			"errors, 2 the invocation itself was invalid (bad flags, or the rules files\n" +
			"could not be located).\n\n" +
			"Warnings are reported apart from errors and do not change the exit code. A\n" +
			"second pass checks the merged rules for problems that span both files, and is\n" +
			"skipped when either file has errors.",

		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: opts.run,
	}

	c.Flags().StringSliceVar(&opts.ProjectConfig, "project-config", nil,
		"project-specific approval rules files, relative to the project root, tried in order (first existing wins; default "+strings.Join(approve.DefaultProjectFiles(), ", ")+")")
	c.Flags().StringVarP(&opts.Output, "output", "o", "text", "output format: text or json")

	return c
}

func (opts *lintOptions) run(cmd *cobra.Command, _ []string) error {
	switch opts.Output {
	case "text", "json":
	default:
		fmt.Fprintf(cmd.ErrOrStderr(), "unsupported --output value: %q (supported: text, json)\n", opts.Output)
		return &model.ExitError{Code: 2}
	}

	global, project, err := approve.Diagnose(approve.LoadOptions{ProjectFiles: opts.ProjectConfig})
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "locating approval rules: %v\n", err)
		return &model.ExitError{Code: 2}
	}

	res := lintResult{
		Global:  lintLayerResultFrom(global),
		Project: lintLayerResultFrom(project),
		Merged:  lintMergedResultFrom(global, project),
	}

	if opts.Output == "json" {
		if err := writeJSON(cmd.OutOrStdout(), res); err != nil {
			return err
		}
	} else {
		writeLintText(cmd.OutOrStdout(), "global", res.Global)
		writeLintText(cmd.OutOrStdout(), "project", res.Project)
		writeLintMergedText(cmd.OutOrStdout(), res.Merged)
	}

	if len(res.Global.Issues) > 0 || len(res.Project.Issues) > 0 {
		return &model.ExitError{Code: 1}
	}

	return nil
}

// lintLayerResultFrom flattens a layer's diagnosis into its JSON/text shape.
func lintLayerResultFrom(d approve.LayerDiagnosis) lintLayerResult {
	return lintLayerResult{
		Path:     d.Path,
		Present:  d.Present,
		Issues:   unwrapIssues(d.Err),
		Warnings: warningMessages(d.Warnings),
	}
}

// lintMergedResultFrom runs the merged pass over both layers.
func lintMergedResultFrom(global, project approve.LayerDiagnosis) lintMergedResult {
	warnings, ok := approve.MergedWarnings(global, project)
	if !ok {
		return lintMergedResult{Skipped: true, Note: lintMergedSkipNote}
	}

	return lintMergedResult{Warnings: warningMessages(warnings)}
}

// writeLintMergedText renders the merged pass: its warnings, or why it was
// skipped.
func writeLintMergedText(w io.Writer, r lintMergedResult) {
	switch {
	case r.Skipped:
		fmt.Fprintf(w, "merged: %s\n", r.Note)
	case len(r.Warnings) > 0:
		fmt.Fprintln(w, "merged:")
		for _, warning := range r.Warnings {
			fmt.Fprintf(w, "  warning: %s\n", warning)
		}
	default:
		fmt.Fprintln(w, "merged: (ok)")
	}
}

// warningMessages reduces warnings to their text for the output formats.
func warningMessages(warnings []approve.Warning) []string {
	var out []string
	for _, w := range warnings {
		out = append(out, w.Message)
	}

	return out
}

// writeLintText renders one layer's lint result as a short human-readable
// report.
func writeLintText(w io.Writer, label string, r lintLayerResult) {
	switch {
	case len(r.Issues) > 0 || len(r.Warnings) > 0:
		fmt.Fprintf(w, "%s: %s\n", label, r.Path)
		for _, issue := range r.Issues {
			fmt.Fprintf(w, "  - %s\n", issue)
		}
		for _, warning := range r.Warnings {
			fmt.Fprintf(w, "  warning: %s\n", warning)
		}
	case !r.Present:
		fmt.Fprintf(w, "%s: %s (absent)\n", label, r.Path)
	default:
		fmt.Fprintf(w, "%s: %s (ok)\n", label, r.Path)
	}
}

// unwrapIssues flattens a layer's error into its individual messages. loadLayer
// wraps a read error, or ParseDocument's joined validation errors, in a single
// "path: %w"; unwrapIssues descends past that wrapper so each returned string
// is one problem, without repeating the path the caller already displays as
// the layer header.
func unwrapIssues(err error) []string {
	if err == nil {
		return nil
	}
	if u, ok := err.(interface{ Unwrap() []error }); ok {
		var out []string
		for _, e := range u.Unwrap() {
			out = append(out, unwrapIssues(e)...)
		}
		return out
	}
	if u, ok := err.(interface{ Unwrap() error }); ok {
		return unwrapIssues(u.Unwrap())
	}

	return []string{err.Error()}
}
