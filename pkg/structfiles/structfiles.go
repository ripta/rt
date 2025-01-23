package structfiles

import (
	"encoding/json"
	"fmt"
	"github.com/ripta/rt/pkg/structfiles/manager"
	"github.com/spf13/cobra"
)

type runner struct{}

func (r *runner) run(files []string) error {
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

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "structfiles",
		Aliases:       []string{"sf"},
		Short:         "Manage files with structured data",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.AddCommand(newEvalCommand())
	return cmd
}

func newEvalCommand() *cobra.Command {
	sf := &runner{}

	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Process and evaluate files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return sf.run(args)
		},
	}

	return cmd
}
