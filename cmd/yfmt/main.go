package main

import (
	"fmt"
	"os"

	"github.com/ripta/rt/pkg/version"
	"github.com/ripta/rt/pkg/yfmt"
)

func main() {
	cmd := yfmt.NewCommand()
	cmd.AddCommand(version.NewCommand())

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(1)
	}
}
