package cg

import (
	"bufio"
	"io"
	"sync"
)

type bufferedLine struct {
	prefix  string
	line    string
	partial bool
}

// LineBuffer accumulates child output lines in memory, grouped by indicator.
// Lines are stored with the prefix captured at receive time so they can be
// replayed later with their original timestamps.
type LineBuffer struct {
	mu      sync.Mutex
	prefix  PrefixFunc
	proc    LineProcessor
	streams map[Indicator][]bufferedLine
}

// SetProcessor sets the line processor for this buffer.
func (b *LineBuffer) SetProcessor(proc LineProcessor) {
	b.proc = proc
}

// NewLineBuffer creates a LineBuffer that calls prefix to obtain the prefix
// string for each line as it arrives.
func NewLineBuffer(prefix PrefixFunc) *LineBuffer {
	return &LineBuffer{
		prefix:  prefix,
		streams: make(map[Indicator][]bufferedLine),
	}
}

// WriteLines reads line-by-line from r, capturing each line with its
// receive-time prefix under the given indicator. Partial final lines (no
// trailing newline) are recorded with partial set to true.
func (b *LineBuffer) WriteLines(r io.Reader, ind Indicator) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				if len(line) > 0 {
					prefix, display := b.processLine(string(line))
					if prefix == "" {
						prefix = b.prefix()
					}

					b.mu.Lock()
					b.streams[ind] = append(b.streams[ind], bufferedLine{
						prefix:  prefix,
						line:    display,
						partial: true,
					})
					b.mu.Unlock()
				}
				return nil
			}
			return err
		}

		raw := string(line[:len(line)-1])
		prefix, display := b.processLine(raw)
		if prefix == "" {
			prefix = b.prefix()
		}

		b.mu.Lock()
		b.streams[ind] = append(b.streams[ind], bufferedLine{
			prefix: prefix,
			line:   display,
		})
		b.mu.Unlock()
	}
}

func (b *LineBuffer) processLine(line string) (prefix string, display string) {
	if b.proc == nil {
		return "", line
	}

	result := b.proc(line)
	if result == nil {
		return "", line
	}

	return result.Prefix, result.Line
}

// Flush writes all buffered lines to w, grouped by stream. Stdout lines are
// written first, then stderr. Each non-empty stream is preceded by a section
// header. Lines are replayed with their original receive-time prefix.
func (b *LineBuffer) Flush(w *AnnotatedWriter) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, ind := range []Indicator{IndicatorOut, IndicatorErr} {
		lines := b.streams[ind]
		if len(lines) == 0 {
			continue
		}

		header := "--- stdout ---"
		if ind == IndicatorErr {
			header = "--- stderr ---"
		}
		if err := w.WriteLine(IndicatorInfo, header); err != nil {
			return err
		}

		for _, bl := range lines {
			if bl.partial {
				if err := w.WritePartialLineWithPrefix(bl.prefix, ind, bl.line); err != nil {
					return err
				}
			} else {
				if err := w.WriteLineWithPrefix(bl.prefix, ind, bl.line); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
