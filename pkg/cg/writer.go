package cg

import (
	"bufio"
	"fmt"
	"io"
	"sync"
)

// Indicator identifies the source of an annotated output line.
type Indicator byte

const (
	// IndicatorInfo marks informational messages from cg itself.
	IndicatorInfo Indicator = 'I'
	// IndicatorOut marks lines from the child's stdout.
	IndicatorOut Indicator = 'O'
	// IndicatorErr marks lines from the child's stderr.
	IndicatorErr Indicator = 'E'
)

// PrefixFunc returns the prefix string to prepend to each annotated line
type PrefixFunc func() string

// AnnotatedWriter writes lines with a prefix and stream indicator to an
// underlying writer. All writes are mutex-protected to prevent interleaving.
type AnnotatedWriter struct {
	mu     sync.Mutex
	dest   io.Writer
	prefix PrefixFunc
}

// NewAnnotatedWriter creates an AnnotatedWriter that writes to dest, calling
// prefix before each line to obtain the current prefix string.
func NewAnnotatedWriter(dest io.Writer, prefix PrefixFunc) *AnnotatedWriter {
	return &AnnotatedWriter{
		dest:   dest,
		prefix: prefix,
	}
}

// WriteLine writes a single annotated line to the destination. The line should
// not include a trailing newline; one will be appended. The write is atomic
// with respect to other WriteLine and WriteLines calls.
func (w *AnnotatedWriter) WriteLine(ind Indicator, line string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := fmt.Fprintf(w.dest, "%s%c: %s\n", w.prefix(), ind, line)
	return err
}

// WritePartialLine writes a single annotated line without a trailing newline.
// Used for the final line of output when it does not end with a newline.
func (w *AnnotatedWriter) WritePartialLine(ind Indicator, line string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := fmt.Fprintf(w.dest, "%s%c: %s", w.prefix(), ind, line)
	return err
}

// WriteLines reads linewise from r, and writes each as an annotated line.
//
// If the final line does not end with a newline, it is written without a
// trailing newline to preserve the child's exact output.
func (w *AnnotatedWriter) WriteLines(r io.Reader, ind Indicator) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				if len(line) > 0 {
					return w.WritePartialLine(ind, string(line))
				}
				return nil
			}
			return err
		}

		if line[len(line)-1] == '\n' {
			if werr := w.WriteLine(ind, string(line[:len(line)-1])); werr != nil {
				return werr
			}
			continue
		}

		if werr := w.WritePartialLine(ind, string(line)); werr != nil {
			return werr
		}
	}
}
