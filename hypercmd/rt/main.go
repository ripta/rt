package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/enc"
	"github.com/ripta/rt/pkg/grpcto"
	"github.com/ripta/rt/pkg/hashsum"
	"github.com/ripta/rt/pkg/streamdiff"
	"github.com/ripta/rt/pkg/toto"
	"github.com/ripta/rt/pkg/uni"
	"github.com/ripta/rt/pkg/version"
	"github.com/ripta/rt/pkg/yfmt"
)

func main() {
	root := &cobra.Command{
		Use:           "rt",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	root.AddCommand(enc.NewCommand())
	root.AddCommand(hashsum.NewCommand())

	root.AddCommand(uni.NewCommand())

	root.AddCommand(grpcto.NewCommand())
	root.AddCommand(toto.NewCommand())
	root.AddCommand(yfmt.NewCommand())

	root.AddCommand(streamdiff.NewCommand())

	root.AddCommand(version.NewCommand())

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(1)
	}
}
