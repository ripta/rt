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
// head is rendered as signal=<sig>; otherwise exitcode=<code>. A non-empty id
// is appended as ` id=<ID>` for runs that produced a capture.
func formatFinish(code int, signaled bool, sig int, d time.Duration, outLines, errLines int64, id string) string {
	head := fmt.Sprintf("exitcode=%d", code)
	if signaled {
		head = fmt.Sprintf("signal=%d", sig)
	}
	line := fmt.Sprintf("Finished %s in %s (out=%d err=%d)", head, formatDuration(d), outLines, errLines)
	if id != "" {
		line += " id=" + id
	}
	return line
}

func (opts *Options) run(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		_ = cmd.Usage()
		return &ExitError{Code: 2}
	}

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

	writeInfo := func(msg string) error {
		return w.WriteLine(IndicatorInfo, msg)
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
		if err := writeInfo(fmt.Sprintf("Started %s", EscapeArgs(args))); err != nil {
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
		_ = writeInfo(formatFinish(code, false, 0, time.Since(start), 0, 0, ""))
		return &ExitError{Code: code}
	}

	var cap *Capture
	if opts.Capture {
		cap, err = NewCapture()
		if err != nil {
			_ = child.Process.Kill()
			_, _ = child.Process.Wait()
			return fmt.Errorf("creating capture files: %w", err)
		}
		defer cap.Close()

		_ = WritePidFile(cap.Dir, child.Process.Pid)

		if opts.Verbose {
			if err := writeInfo(fmt.Sprintf("capture.stdout=%s", cap.Stdout.Name())); err != nil {
				return fmt.Errorf("writing capture stdout path: %w", err)
			}
			if err := writeInfo(fmt.Sprintf("capture.stderr=%s", cap.Stderr.Name())); err != nil {
				return fmt.Errorf("writing capture stderr path: %w", err)
			}
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

	id := ""
	if cap != nil {
		id = cap.ID
	}

	ws := exitStatus(child)
	signaled := ws != nil && ws.Signaled()
	var sig int
	if signaled {
		sig = int(ws.Signal())
	}
	_ = writeInfo(formatFinish(code, signaled, sig, elapsed, outLines, errLines, id))

	if cap != nil {
		meta := &Meta{
			ID:          cap.ID,
			Command:     args,
			StartedAt:   start.UTC(),
			FinishedAt:  start.Add(elapsed).UTC(),
			DurationMs:  elapsed.Milliseconds(),
			ExitCode:    code,
			StdoutLines: outLines,
			StderrLines: errLines,
		}
		if signaled {
			meta.Signal = &sig
		}
		if err := WriteMeta(cap.Dir, meta); err != nil {
			_ = writeInfo(fmt.Sprintf("meta.json write failed: %s", err))
		}
		RemovePidFile(cap.Dir)
	}

	if code != 0 {
		return &ExitError{Code: code}
	}

	return nil
}
