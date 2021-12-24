package version

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

var Version string

type versioner struct {
	JSON bool
}

func NewCommand() *cobra.Command {
	v := &versioner{}
	c := cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE:  v.run,
	}

	c.PersistentFlags().BoolVarP(&v.JSON, "json", "j", false, "Print out version and debug information in JSON")
	return &c
}

func (v *versioner) run(cmd *cobra.Command, args []string) error {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return errors.New("version not available: binary was not built with module support")
	}

	if bi.Main.Version == "(devel)" && Version != "" {
		bi.Main.Version = Version
	}

	if v.JSON {
		bs, err := json.MarshalIndent(&bi, "", "  ")
		if err != nil {
			return err
		}

		fmt.Printf(string(bs) + "\n")
		return nil
	}

	fmt.Printf("%s version %s\n", bi.Path, bi.Main.Version)
	return nil
}
