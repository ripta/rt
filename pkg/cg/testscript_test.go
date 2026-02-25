package cg

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cg":              cgMain,
		"emit-json-log":   emitJSONLog,
		"emit-logfmt-log": emitLogfmtLog,
	}))
}

func cgMain() int {
	cmd := NewCommand()
	err := cmd.Execute()
	if err == nil {
		return 0
	}

	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}

	fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
	return 1
}

func emitJSONLog() int {
	fmt.Println(`{"timestamp":"2025-01-15T10:30:00Z","level":"info","message":"server started","port":8080}`)
	fmt.Println(`{"timestamp":"2025-01-15T10:30:01Z","level":"info","message":"listening on port","port":8080}`)
	return 0
}

func emitLogfmtLog() int {
	fmt.Println(`timestamp=2025-01-15T10:30:00Z level=info message="server started" port=8080`)
	fmt.Println(`timestamp=2025-01-15T10:30:01Z level=info message="listening on port" port=8080`)
	return 0
}

func TestCgScript(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testscript tests")
	}

	testscript.Run(t, testscript.Params{
		Dir: "testdata",
	})
}
