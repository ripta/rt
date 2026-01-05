package version

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version is a build-time variable that users can set to override the version string.
//
// It can be set using: -ldflags "-X github.com/ripta/rt/pkg/version.Version=1.2.3"
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

		SilenceUsage:  true,
		SilenceErrors: true,
	}

	c.PersistentFlags().BoolVarP(&v.JSON, "json", "j", false, "Print out version and debug information in JSON")
	return &c
}

// Info contains version information about the build.
type Info struct {
	// BuildInfo contains the Go build information from the binary
	*debug.BuildInfo
	// Version is the formatted version string
	Version string
}

// Get retrieves the version information from the build info. This relies on
// the vcs.revision and vcs.modified build settings. If the binary was built
// without module support, it returns an error.
func Get() (*Info, error) {
	_ = Version // mark used

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil, errors.New("version not available: binary was not built with module support")
	}

	version := bi.Main.Version
	dirty := false
	if version == "(devel)" {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				version = s.Value
			}
			if s.Key == "vcs.modified" && s.Value == "true" {
				dirty = true
			}
		}
	}

	if dirty {
		version = version + "-dirty"
	}

	return &Info{
		BuildInfo: bi,
		Version:   version,
	}, nil
}

// GetString returns the version as a string. If the Version package variable
// is set, it returns that value. Otherwise, it attempts to get the version
// from the build info. If _that_ fails, it returns empty string, which you
// may presumably want to handle.
func GetString() string {
	if Version != "" {
		return Version
	}

	vi, err := Get()
	if err != nil {
		return ""
	}

	return vi.Version
}

func (v *versioner) run(cmd *cobra.Command, args []string) error {
	vi, err := Get()
	if err != nil {
		return err
	}

	if Version != "" {
		vi.Version = Version
	}

	if v.JSON {
		bs, err := json.MarshalIndent(&vi, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(bs))
		return nil
	}

	fmt.Printf("%s version %s\n", vi.BuildInfo.Path, vi.Version)
	return nil
}
