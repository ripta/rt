package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/ripta/rt/pkg/shast"
	"github.com/ripta/rt/pkg/version"
)

func main() {
	cmd := shast.NewCommand()
	cmd.AddCommand(version.NewCommand())

	err := cmd.Execute()
	if err == nil {
		return
	}

	var exitErr *shast.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.Code)
	}

	fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
	os.Exit(1)
}
