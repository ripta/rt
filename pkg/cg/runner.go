package cg

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/version"
)

// lineCountingReader wraps an io.Reader and counts '\n' bytes as they pass
// through. The counter is intended to be read after the reader has been fully
// drained, but uses atomic operations so callers can sample it earlier if
// needed.
type lineCountingReader struct {
	r io.Reader
	n atomic.Int64
}

func (lc *lineCountingReader) Read(p []byte) (int, error) {
	n, err := lc.r.Read(p)
	for _, b := range p[:n] {
		if b == '\n' {
			lc.n.Add(1)
		}
	}
	return n, err
}

// formatDuration renders d with tiered rounding so the finish-line duration
// stays readable at every scale.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < time.Minute:
		return d.Round(10 * time.Millisecond).String()
	default:
		return d.Round(time.Second).String()
	}
}

// formatFinish builds the end-of-run summary line. When signaled is true, the
// head is rendered as signal=<sig>; otherwise exitcode=<code>.
func formatFinish(code int, signaled bool, sig int, d time.Duration, outLines, errLines int64) string {
	head := fmt.Sprintf("exitcode=%d", code)
	if signaled {
		head = fmt.Sprintf("signal=%d", sig)
	}
	return fmt.Sprintf("Finished %s in %s (out=%d err=%d)", head, formatDuration(d), outLines, errLines)
}

func (opts *Options) run(cmd *cobra.Command, args []string) error {
	if err := opts.validateFlags(cmd); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return &ExitError{Code: 2}
	}

	brief := !opts.Verbose
	prefix := func() string {
		return time.Now().Format(opts.Format)
	}
	w := NewAnnotatedWriter(cmd.OutOrStdout(), prefix, brief)

	switch opts.LogParse {
	case "json":
		w.SetProcessor(NewJSONProcessor(JSONProcessorOptions{
			MessageKey:   opts.LogMsgKey,
			TimestampKey: opts.LogTSKey,
			TimestampFmt: opts.LogTSFmt,
			Fields:       parseFieldsFlag(opts.LogFields),
			Format:       opts.Format,
		}))
	case "logfmt":
		w.SetProcessor(NewLogfmtProcessor(LogfmtProcessorOptions{
			MessageKey:   opts.LogMsgKey,
			TimestampKey: opts.LogTSKey,
			TimestampFmt: opts.LogTSFmt,
			Fields:       parseFieldsFlag(opts.LogFields),
			Format:       opts.Format,
		}))
	}

	var buf *LineBuffer
	if opts.Buffered {
		buf = NewLineBuffer(prefix, brief)
		if w.proc != nil {
			buf.SetProcessor(w.proc)
		}
	}

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

	if opts.Verbose {
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
	}

	if opts.Buffered {
		if err := writeInfo("buffered mode, output deferred"); err != nil {
			return fmt.Errorf("writing buffered info: %w", err)
		}
	}

	if opts.Verbose {
		if err := writeInfo(fmt.Sprintf("Started %s", escapeArgs(args))); err != nil {
			return fmt.Errorf("writing start info: %w", err)
		}
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

	start := time.Now()
	if err := child.Start(); err != nil {
		code := ExitCodeFromError(err)
		_ = writeInfo(formatFinish(code, false, 0, time.Since(start), 0, 0))
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

	outCounter := &lineCountingReader{r: stdout}
	errCounter := &lineCountingReader{r: stderr}

	var wg sync.WaitGroup
	wg.Add(2)

	switch {
	case cap != nil && buf != nil:
		go func() {
			defer wg.Done()
			_ = buf.WriteLines(io.TeeReader(outCounter, cap.Stdout), IndicatorOut)
		}()
		go func() {
			defer wg.Done()
			_ = buf.WriteLines(io.TeeReader(errCounter, cap.Stderr), IndicatorErr)
		}()
	case cap != nil:
		go func() {
			defer wg.Done()
			_, _ = io.Copy(cap.Stdout, outCounter)
		}()
		go func() {
			defer wg.Done()
			_, _ = io.Copy(cap.Stderr, errCounter)
		}()
	case buf != nil:
		go func() {
			defer wg.Done()
			_ = buf.WriteLines(outCounter, IndicatorOut)
		}()
		go func() {
			defer wg.Done()
			_ = buf.WriteLines(errCounter, IndicatorErr)
		}()
	default:
		go func() {
			defer wg.Done()
			_ = w.WriteLines(outCounter, IndicatorOut)
		}()
		go func() {
			defer wg.Done()
			_ = w.WriteLines(errCounter, IndicatorErr)
		}()
	}

	wg.Wait()

	waitErr := child.Wait()
	elapsed := time.Since(start)
	code := ExitCodeFromError(waitErr)

	if buf != nil {
		if err := buf.Flush(w); err != nil {
			return fmt.Errorf("flushing buffered output: %w", err)
		}
	}

	outLines := outCounter.n.Load()
	errLines := errCounter.n.Load()

	if ws := exitStatus(child); ws != nil && ws.Signaled() {
		_ = writeInfo(formatFinish(code, true, int(ws.Signal()), elapsed, outLines, errLines))
	} else {
		_ = writeInfo(formatFinish(code, false, 0, elapsed, outLines, errLines))
	}

	if code != 0 {
		return &ExitError{Code: code}
	}

	return nil
}
