package cg

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/version"
)

func (opts *Options) run(cmd *cobra.Command, args []string) error {
	w := NewAnnotatedWriter(cmd.OutOrStdout(), func() string {
		return time.Now().Format(opts.Format)
	})

	v := version.GetString()
	if v == "" {
		v = "unknown"
	}

	if err := w.WriteLine(IndicatorInfo, fmt.Sprintf("cg %s", v)); err != nil {
		return fmt.Errorf("writing version info: %w", err)
	}

	if err := w.WriteLine(IndicatorInfo, fmt.Sprintf("prefix=%q", opts.Format)); err != nil {
		return fmt.Errorf("writing prefix info: %w", err)
	}

	if err := w.WriteLine(IndicatorInfo, fmt.Sprintf("Started %s", escapeArgs(args))); err != nil {
		return fmt.Errorf("writing start info: %w", err)
	}

	child := exec.CommandContext(cmd.Context(), args[0], args[1:]...)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := child.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	stderr, err := child.StderrPipe()
	if err != nil {
		return fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := child.Start(); err != nil {
		code := ExitCodeFromError(err)
		_ = w.WriteLine(IndicatorInfo, fmt.Sprintf("Finished with exitcode %d", code))
		return &ExitError{Code: code}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
	}()

	go func() {
		for sig := range sigCh {
			// Forward signal to the child's process group
			_ = syscall.Kill(-child.Process.Pid, sig.(syscall.Signal))
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = w.WriteLines(stdout, IndicatorOut)
	}()
	go func() {
		defer wg.Done()
		_ = w.WriteLines(stderr, IndicatorErr)
	}()

	wg.Wait()

	waitErr := child.Wait()
	code := ExitCodeFromError(waitErr)
	if ws := exitStatus(child); ws != nil && ws.Signaled() {
		_ = w.WriteLine(IndicatorInfo, fmt.Sprintf("Finished with signal %d", ws.Signal()))
	} else {
		_ = w.WriteLine(IndicatorInfo, fmt.Sprintf("Finished with exitcode %d", code))
	}

	if code != 0 {
		return &ExitError{Code: code}
	}

	return nil
}
