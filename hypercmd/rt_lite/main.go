package main

import (
	"fmt"
	"os"

	"github.com/ripta/hypercmd/pkg/hypercmd"

	"github.com/ripta/rt/pkg/enc"
	"github.com/ripta/rt/pkg/hashsum"
	"github.com/ripta/rt/pkg/lipsum"
	"github.com/ripta/rt/pkg/uni"
	"github.com/ripta/rt/pkg/version"
	"github.com/ripta/rt/pkg/yfmt"
)

func main() {
	root := hypercmd.New("rt_lite")
	root.Root().Aliases = []string{"rt_lite.wasm"}
	root.Root().SilenceErrors = true
	root.Root().SilenceUsage = true

	root.AddCommand(enc.NewCommand())
	root.AddCommand(hashsum.NewCommand())
	root.AddCommand(lipsum.NewCommand())
	root.AddCommand(uni.NewCommand())
	root.AddCommand(yfmt.NewCommand())

	v := version.NewCommand()
	root.Root().AddCommand(v)
	for _, c := range root.Commands() {
		c.AddCommand(v)
	}

	cmd, err := root.Resolve(os.Args, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(1)
	}

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(1)
	}
}
