package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/ripta/rt/pkg/cg"
	"github.com/ripta/rt/pkg/version"
)

func main() {
	cmd := cg.NewCommand()
	cmd.AddCommand(version.NewCommand())

	err := cmd.Execute()
	if err == nil {
		return
	}

	var exitErr *cg.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.Code)
	}

	fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
	os.Exit(1)
}
