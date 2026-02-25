package cg

import (
	"fmt"
	"io"
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
	prefix := func() string {
		return time.Now().Format(opts.Format)
	}
	w := NewAnnotatedWriter(cmd.OutOrStdout(), prefix)

	// writeInfo writes a lifecycle message to the annotated writer and
	// optionally buffers it for later replay to the capture lifecycle file.
	var preCaptureMsgs []string
	writeInfo := func(msg string) error {
		if err := w.WriteLine(IndicatorInfo, msg); err != nil {
			return err
		}
		if opts.Capture {
			preCaptureMsgs = append(preCaptureMsgs, msg)
		}
		return nil
	}

	v := version.GetString()
	if v == "" {
		v = "unknown"
	}

	if err := writeInfo(fmt.Sprintf("cg %s", v)); err != nil {
		return fmt.Errorf("writing version info: %w", err)
	}

	if err := writeInfo(fmt.Sprintf("prefix=%q", opts.Format)); err != nil {
		return fmt.Errorf("writing prefix info: %w", err)
	}

	if err := writeInfo(fmt.Sprintf("Started %s", escapeArgs(args))); err != nil {
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
		_ = writeInfo(fmt.Sprintf("Finished with exitcode %d", code))
		return &ExitError{Code: code}
	}

	var cap *Capture
	if opts.Capture {
		cap, err = NewCapture(child.Process.Pid, prefix)
		if err != nil {
			_ = child.Process.Kill()
			_, _ = child.Process.Wait()
			return fmt.Errorf("creating capture files: %w", err)
		}
		defer cap.Close()

		for _, msg := range preCaptureMsgs {
			if err := cap.WriteLifecycle(msg); err != nil {
				return fmt.Errorf("writing buffered lifecycle: %w", err)
			}
		}

		// Rebind writeInfo to write to both destinations
		writeInfo = func(msg string) error {
			if err := w.WriteLine(IndicatorInfo, msg); err != nil {
				return err
			}
			return cap.WriteLifecycle(msg)
		}

		if err := writeInfo(fmt.Sprintf("capture.stdout=%s", cap.Stdout.Name())); err != nil {
			return fmt.Errorf("writing capture stdout path: %w", err)
		}
		if err := writeInfo(fmt.Sprintf("capture.stderr=%s", cap.Stderr.Name())); err != nil {
			return fmt.Errorf("writing capture stderr path: %w", err)
		}
		if err := writeInfo(fmt.Sprintf("capture.lifecycle=%s", cap.Lifecycle.Name())); err != nil {
			return fmt.Errorf("writing capture lifecycle path: %w", err)
		}
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

	if cap != nil {
		go func() {
			defer wg.Done()
			_, _ = io.Copy(cap.Stdout, stdout)
		}()
		go func() {
			defer wg.Done()
			_, _ = io.Copy(cap.Stderr, stderr)
		}()
	} else {
		go func() {
			defer wg.Done()
			_ = w.WriteLines(stdout, IndicatorOut)
		}()
		go func() {
			defer wg.Done()
			_ = w.WriteLines(stderr, IndicatorErr)
		}()
	}

	wg.Wait()

	waitErr := child.Wait()
	code := ExitCodeFromError(waitErr)
	if ws := exitStatus(child); ws != nil && ws.Signaled() {
		_ = writeInfo(fmt.Sprintf("Finished with signal %d", ws.Signal()))
	} else {
		_ = writeInfo(fmt.Sprintf("Finished with exitcode %d", code))
	}

	if code != 0 {
		return &ExitError{Code: code}
	}

	return nil
}
