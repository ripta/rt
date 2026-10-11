package model

import (
	"bytes"
	"sync"

	"github.com/spf13/cobra"
)

// syncBuffer is a bytes.Buffer safe for a test to read while a command is still writing to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// newTestRoot builds a bare cobra root with every model command attached, for
// tests that dispatch through cobra's flag/arg parsing rather than calling
// package functions directly. It mirrors package cg's NewCommand() minus the
// bare-invocation help/error behavior, which belongs to that package.
func newTestRoot() *cobra.Command {
	c := &cobra.Command{
		Use:           "cg",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	c.AddCommand(NewRunCommand())
	c.AddCommand(NewOutCommand())
	c.AddCommand(NewErrCommand())
	c.AddCommand(NewPathsCommand())
	c.AddCommand(NewLsCommand())
	c.AddCommand(NewStatsCommand())
	c.AddCommand(NewPruneCommand())
	c.AddCommand(NewMetaCommand())
	c.AddCommand(NewWaitCommand())
	c.AddCommand(NewCancelCommand())
	c.AddCommand(NewGrepCommand())
	c.AddCommand(NewNoteCommand())
	c.AddCommand(NewSuperviseRunCommand())
	c.AddCommand(NewSupervisePoolCommand())
	return c
}
