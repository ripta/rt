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

type VersionInfo struct {
	*debug.BuildInfo
	Version string
}

func (v *versioner) run(cmd *cobra.Command, args []string) error {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return errors.New("version not available: binary was not built with module support")
	}

	version := bi.Main.Version
	if version == "(devel)" {
		if Version != "" {
			version = Version
		} else {
			for _, s := range bi.Settings {
				if s.Key == "vcs.revision" {
					version = s.Value
				}
			}
		}
	}

	if v.JSON {
		vi := VersionInfo{
			BuildInfo: bi,
			Version:   version,
		}
		bs, err := json.MarshalIndent(&vi, "", "  ")
		if err != nil {
			return err
		}

		fmt.Printf(string(bs) + "\n")
		return nil
	}

	fmt.Printf("%s version %s\n", bi.Path, version)
	return nil
}
