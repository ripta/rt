package structfiles

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/liggitt/tabwriter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/ripta/rt/pkg/structfiles/manager"
)

var (
	ErrInvalidDiffArgs = errors.New("invalid diff arguments: expected either ErrInvalidDiffArgs exactly two arguments (before and after), or more than two arguments with a double colon (::) separating the before and after")
)

type runner struct {
	Format  string
	Raw     bool
	Options map[string]string

	Kubernetes bool

	FilterIn   string
	FilterOut  string
	GroupBy    string
	SortBy     string
	SortByFunc string
}

func (r *runner) BindFlagSet(fs *pflag.FlagSet) {
	fs.StringVarP(&r.Format, "format", "f", r.Format, "Output format, one of: json, yaml, toml, hclv2, gob")
	fs.BoolVarP(&r.Raw, "raw", "r", r.Raw, "Output raw structure")
	fs.StringToStringVarP(&r.Options, "option", "o", r.Options, "Options for the output format")

	fs.BoolVarP(&r.Kubernetes, "kubernetes", "k", r.Kubernetes, "Process files as Kubernetes resources (see help)")

	fs.StringVarP(&r.FilterIn, "filter-in", "i", r.FilterIn, "Filter documents in by the result of evaluating the expression; variables: doc, index, source.name, source.index")
	fs.StringVarP(&r.FilterOut, "filter-out", "I", r.FilterOut, "Filter documents out by the result of evaluating the expression; variables: doc, index, source.name, source.index")
	fs.StringVarP(&r.GroupBy, "group-by", "g", r.GroupBy, "Group documents by the result of evaluating the expression; variables: doc, index, source.name, source.index")
	fs.StringVarP(&r.SortBy, "sort-by", "s", r.SortBy, "Sort documents by the result of evaluating the expression; variables: {a,b}.{doc,index,source}")
	fs.StringVarP(&r.SortByFunc, "sort-by-func", "S", r.SortByFunc, "Sort documents by the result of evaluating the expression; variables: doc, index, source.name, source.index")
}

func (r *runner) RunDiff(files []string) error {
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

	preBuf, err := r.eval(preFiles)
	if err != nil {
		return fmt.Errorf("evaluating 'before' files: %w", err)
	}

	postBuf, err := r.eval(postFiles)
	if err != nil {
		return fmt.Errorf("evaluating 'after' files: %w", err)
	}

	preName := "before"
	if len(preFiles) == 1 {
		preName = preFiles[0]
	}

	postName := "after"
	if len(postFiles) == 1 {
		postName = postFiles[0]
	}

	uni, err := generateDiff(preName, postName, preBuf, postBuf)
	if err != nil {
		return fmt.Errorf("generating diff: %w", err)
	}

	fmt.Printf("%s\n", uni)
	return nil
}

func (r *runner) RunEval(files []string) error {
	buf, err := r.eval(files)
	if err != nil {
		return fmt.Errorf("evaluating files: %w", err)
	}

	fmt.Print(buf.String())
	return nil
}

func (r *runner) Defaulting(_ *cobra.Command, _ []string) error {
	if !r.Kubernetes {
		return nil
	}

	if r.GroupBy == "" {
		r.GroupBy = `doc.apiVersion + "." + doc.kind`
	}
	if r.SortBy == "" && r.SortByFunc == "" {
		r.SortBy = `a.doc.apiVersion + "." + a.doc.kind + "/" + a.doc.metadata.name < b.doc.apiVersion + "." + b.doc.kind + "/" + b.doc.metadata.name`
	}
	if r.Format == "" {
		r.Format = "yaml"
	}

	return nil
}

func (r *runner) eval(files []string) (*bytes.Buffer, error) {
	m := manager.New()

	if err := m.ProcessAll(files); err != nil {
		return nil, err
	}

	//for _, f := range files {
	//	if err := m.ProcessDir(f); err != nil {
	//		return nil, err
	//	}
	//}

	if m.Len() == 0 {
		return nil, errors.New("no documents found")
	}

	if filter := r.FilterIn; filter != "" {
		if err := m.Filter(filter, true); err != nil {
			return nil, fmt.Errorf("filtering-in documents: %w", err)
		}
	}

	if filter := r.FilterOut; filter != "" {
		if err := m.Filter(filter, false); err != nil {
			return nil, fmt.Errorf("filtering-out documents: %w", err)
		}
	}

	if group := r.GroupBy; group != "" {
		if err := m.GroupBy(group); err != nil {
			return nil, fmt.Errorf("grouping documents: %w", err)
		}
	}

	if sort := r.SortBy; sort != "" {
		if err := m.SortBy(sort); err != nil {
			return nil, fmt.Errorf("sorting documents: %w", err)
		}
	}

	if sort := r.SortByFunc; sort != "" {
		if err := m.SortByFunc(sort, false); err != nil {
			return nil, fmt.Errorf("sorting documents: %w", err)
		}
	}

	buf := &bytes.Buffer{}
	if r.Raw {
		if err := m.EmitRaw(buf, r.Format, r.Options); err != nil {
			return nil, fmt.Errorf("emitting raw result: %w", err)
		}
	} else {
		if err := m.Emit(manager.MemoryWriter(buf), r.Format, r.Options); err != nil {
			return nil, fmt.Errorf("emitting result: %w", err)
		}
	}

	return buf, nil
}

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "sf",
		Aliases:       []string{"structfiles"},
		Short:         "Normalize and compare files with structured data",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.AddCommand(newDiffCommand())
	cmd.AddCommand(newEvalCommand())
	cmd.AddCommand(newFormatsCommand())
	return cmd
}

func newDiffCommand() *cobra.Command {
	sf := &runner{
		Format: "json",
	}

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff files with structured data",

		PreRunE: sf.Defaulting,
		RunE: func(cmd *cobra.Command, args []string) error {
			return sf.RunDiff(args)
		},
	}

	sf.BindFlagSet(cmd.Flags())
	return cmd
}

func newEvalCommand() *cobra.Command {
	sf := &runner{
		Format: "json",
	}

	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Process and evaluate files",
		Example: `
	# These two are the same:
	sf eval --kubernetes dirs...
	sf eval --group-by 'doc.apiVersion + "." + doc.kind' --sort-by 'a.doc.name < b.doc.name' dirs...

	# Split each document into its own file by providing the index of the document as the group name:
	sf eval --group-by 'index' dirs...

	# Combine all documents into one file by providing a constant as the group name:
	sf eval --group-by '0' dirs...
`,

		PreRunE: sf.Defaulting,
		RunE: func(cmd *cobra.Command, args []string) error {
			return sf.RunEval(args)
		},
	}

	sf.BindFlagSet(cmd.Flags())
	return cmd
}

func newFormatsCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "formats",
		Aliases: []string{"format", "fmt"},
		RunE: func(cmd *cobra.Command, args []string) error {
			tw := tabwriter.NewWriter(os.Stdout, 6, 4, 3, ' ', tabwriter.RememberWidths)

			fmt.Fprintln(tw, "FORMAT\tEXTENSIONS\tINPUT\tOUTPUT\tOPTIONS")
			for _, f := range manager.GetFormats() {
				exts := strings.Join(manager.GetExtensions(f), " ")

				hasDecoder := "no"
				if manager.GetDecoderFactory(f) != nil {
					hasDecoder = "yes"
				}

				hasEncoder := "no"
				df, _ := manager.GetEncoderFactory(f, nil)
				if df != nil {
					hasEncoder = "yes"
				}

				optionDescs := []string{}
				for k, v := range manager.GetEncoderOptions(f) {
					optionDescs = append(optionDescs, fmt.Sprintf("%s:%s", k, v))
				}
				if len(optionDescs) == 0 {
					optionDescs = []string{"-"}
				}

				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", f, exts, hasDecoder, hasEncoder, strings.Join(optionDescs, " "))
			}

			return tw.Flush()
		},
	}
}
