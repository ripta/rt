package main

import (
	"fmt"
	"os"

	"github.com/ripta/rt/pkg/hashsum"
	"github.com/ripta/rt/pkg/version"
)

func main() {
	cmd := hashsum.NewCommand()
	cmd.AddCommand(version.NewCommand())

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(1)
	}
}
