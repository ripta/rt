package main

import (
	"fmt"
	"os"

	"github.com/ripta/rt/pkg/uni"
	"github.com/ripta/rt/pkg/version"
)

func main() {
	cmd := uni.NewCommand()
	cmd.AddCommand(version.NewCommand())

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(1)
	}
}
