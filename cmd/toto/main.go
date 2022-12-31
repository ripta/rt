package main

import (
	"fmt"
	"os"

	"github.com/ripta/rt/pkg/toto"
	"github.com/ripta/rt/pkg/version"
)

func main() {
	cmd := toto.NewCommand()
	cmd.AddCommand(version.NewCommand())

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(1)
	}
}
