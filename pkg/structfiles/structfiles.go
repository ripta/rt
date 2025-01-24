package structfiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/ripta/gxti/diff"
	"github.com/ripta/rt/pkg/structfiles/manager"
	"github.com/spf13/cobra"
)

var (
	ErrInvalidDiffArgs = errors.New("invalid diff arguments: expected either ErrInvalidDiffArgs exactly two arguments (before and after), or more than two arguments with a double colon (::) separating the before and after")
)

type runner struct{}

func (r *runner) diff(files []string) error {
	preFiles := []string{}
	postFiles := []string{}

	if len(files) == 2 {
		preFiles = []string{files[0]}
		postFiles = []string{files[1]}
	} else if colon := slices.Index(files, "::"); colon >= 0 {
		preFiles = files[:colon]
		postFiles = files[colon+1:]
	} else {
		return ErrInvalidDiffArgs
	}

	preBuf, err := eval(preFiles)
	if err != nil {
		return fmt.Errorf("evaluating 'before' files: %w", err)
	}

	postBuf, err := eval(postFiles)
	if err != nil {
		return fmt.Errorf("evaluating 'after' files: %w", err)
	}

	uni := diff.Unified("before", "after", preBuf.String(), postBuf.String())
	fmt.Printf("%s\n", uni)

	return nil
}

func (r *runner) eval(files []string) error {
	m := manager.New()

	//if err := m.ProcessAll(files); err != nil {
	//	return err
	//}
	for _, f := range files {
		if err := m.ProcessDir(f); err != nil {
			return err
		}
	}

	//if err := m.Flatten(); err != nil {
	//	return err
	//}

	if err := m.GroupBy(`doc.apiVersion + "." + doc.kind`); err != nil {
		return err
	}

	bs, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", string(bs))
	return nil
}

func eval(files []string) (*bytes.Buffer, error) {
	m := manager.New()
	if err := m.ProcessAll(files); err != nil {
		return nil, fmt.Errorf("processing files: %w", err)
	}

	if err := m.AllInOne(); err != nil {
		return nil, fmt.Errorf("flattening: %w", err)
	}

	if err := m.SortBy(`a.doc.apiVersion + "." + a.doc.kind < b.doc.apiVersion + "." + b.doc.kind`); err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	if err := m.Emit(manager.MemoryWriter(buf), "yaml"); err != nil {
		return nil, fmt.Errorf("emitting result: %w", err)
	}

	return buf, nil
}

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "structfiles",
		Aliases:       []string{"sf"},
		Short:         "Manage files with structured data",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.AddCommand(newDiffCommand())
	cmd.AddCommand(newEvalCommand())
	return cmd
}

func newDiffCommand() *cobra.Command {
	sf := &runner{}

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff files with structured data",
		RunE: func(cmd *cobra.Command, args []string) error {
			return sf.diff(args)
		},
	}

	return cmd
}

func newEvalCommand() *cobra.Command {
	sf := &runner{}

	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Process and evaluate files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return sf.eval(args)
		},
	}

	return cmd
}
