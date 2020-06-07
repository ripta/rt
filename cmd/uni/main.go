package main

import (
	"fmt"
	"os"

	"github.com/ripta/rt/pkg/uni"
)

func main() {
	cmd := uni.NewCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(1)
	}
}
