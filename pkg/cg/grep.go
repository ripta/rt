package cg

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"unicode/utf8"
)

const (
	// DefaultGrepMaxMatches caps returned matches when the caller leaves
	// MaxMatches unset. MaxGrepMatches is the hard ceiling.
	DefaultGrepMaxMatches = 1000
	MaxGrepMatches        = 10000

	// maxGrepLineBytes bounds a single scanned line so a pathological line
	// without newlines cannot exhaust memory. Bytes past the cap are discarded.
	maxGrepLineBytes = 65536

	// GrepStreamsAll, GrepStreamsStdout, and GrepStreamsStderr are the accepted
	// Streams selectors.
	GrepStreamsAll    = "all"
	GrepStreamsStdout = "stdout"
	GrepStreamsStderr = "stderr"

	grepContentEncodingBase64 = "base64"
)

// GrepOptions configures a Grep scan. Exactly one of Text or Pattern must be
// set: Text is a fixed-string substring search, Pattern is an RE2 regex.
type GrepOptions struct {
	Text            string
	Pattern         string
	Streams         string
	CaseInsensitive bool
	InvertMatch     bool
	MaxMatches      int
}

// GrepMatch is one matching line. ContentEncoding is omitted (meaning utf8) for
// valid UTF-8 lines and set to "base64" when the line carries invalid bytes, in
// which case Line is base64-encoded.
type GrepMatch struct {
	Stream          string `json:"stream"`
	LineNumber      int64  `json:"line_number"`
	Line            string `json:"line"`
	ContentEncoding string `json:"content_encoding,omitempty"`
}

// GrepResult is the outcome of a Grep scan. Truncated reports that the
// MaxMatches cap was hit before the targeted streams were fully scanned.
type GrepResult struct {
	Matches    []GrepMatch `json:"matches"`
	MatchCount int         `json:"match_count"`
	Truncated  bool        `json:"truncated"`
}

// Grep searches the captured output of run id line by line. It resolves the run
// directory tolerating in-flight and failed-to-start runs, so a running capture
// can be searched as its output grows. An unknown ID surfaces as
// ErrUnknownRunID for the caller to map. Both streams are scanned by default;
// Streams narrows to stdout or stderr. Lines with invalid UTF-8 are
// base64-encoded and tagged content_encoding: "base64".
func Grep(id string, opts GrepOptions) (GrepResult, error) {
	if (opts.Text == "") == (opts.Pattern == "") {
		return GrepResult{}, fmt.Errorf("exactly one of text or pattern must be set")
	}

	streams := opts.Streams
	if streams == "" {
		streams = GrepStreamsAll
	}
	switch streams {
	case GrepStreamsAll, GrepStreamsStdout, GrepStreamsStderr:
	default:
		return GrepResult{}, fmt.Errorf("invalid streams %q: want all|stdout|stderr", opts.Streams)
	}

	if opts.MaxMatches < 0 {
		return GrepResult{}, fmt.Errorf("max_matches must be non-negative")
	}
	maxMatches := opts.MaxMatches
	if maxMatches == 0 {
		maxMatches = DefaultGrepMaxMatches
	}
	if maxMatches > MaxGrepMatches {
		maxMatches = MaxGrepMatches
	}

	matcher, err := buildGrepMatcher(opts)
	if err != nil {
		return GrepResult{}, err
	}

	dir, err := LookupRunDir(id)
	if err != nil && !errors.Is(err, ErrIncompleteRun) && !errors.Is(err, ErrFailedRun) {
		return GrepResult{}, err
	}

	out := GrepResult{Matches: []GrepMatch{}}
	for _, name := range grepTargetStreams(streams) {
		more, err := grepStream(filepath.Join(dir, name), name, matcher, maxMatches, &out.Matches)
		if err != nil {
			return GrepResult{}, err
		}
		if more {
			out.Truncated = true
			break
		}
	}

	out.MatchCount = len(out.Matches)
	return out, nil
}

// grepTargetStreams expands the streams selector into the ordered file names to
// scan. all scans stdout before stderr so the result ordering is deterministic.
func grepTargetStreams(streams string) []string {
	switch streams {
	case GrepStreamsStdout:
		return []string{GrepStreamsStdout}
	case GrepStreamsStderr:
		return []string{GrepStreamsStderr}
	default:
		return []string{GrepStreamsStdout, GrepStreamsStderr}
	}
}

// buildGrepMatcher compiles the options into a single line predicate.
// InvertMatch negates the predicate after the underlying match test.
func buildGrepMatcher(opts GrepOptions) (func([]byte) bool, error) {
	var match func([]byte) bool
	if opts.Text != "" {
		if opts.CaseInsensitive {
			needle := bytes.ToLower([]byte(opts.Text))
			match = func(line []byte) bool { return bytes.Contains(bytes.ToLower(line), needle) }
		} else {
			needle := []byte(opts.Text)
			match = func(line []byte) bool { return bytes.Contains(line, needle) }
		}
	} else {
		expr := opts.Pattern
		if opts.CaseInsensitive {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern: %w", err)
		}
		match = re.Match
	}
	if opts.InvertMatch {
		inner := match
		return func(line []byte) bool { return !inner(line) }, nil
	}
	return match, nil
}

// grepStream scans path line by line, appending matches to *acc until the
// maxMatches cap is reached. It returns more=true when matches remain beyond
// the cap. A missing file yields no matches and no error, since the run dir was
// already validated by the caller.
func grepStream(path, stream string, matcher func([]byte) bool, maxMatches int, acc *[]GrepMatch) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("opening %s: %w", stream, err)
	}
	defer f.Close()

	r := bufio.NewReader(f)
	var lineNo int64
	for {
		line, err := readGrepLine(r)
		if err != nil && !errors.Is(err, io.EOF) {
			return false, fmt.Errorf("reading %s: %w", stream, err)
		}
		if len(line) > 0 || err == nil {
			lineNo++
			if matcher(line) {
				if len(*acc) >= maxMatches {
					return true, nil
				}
				*acc = append(*acc, newGrepMatch(stream, lineNo, line))
			}
		}
		if errors.Is(err, io.EOF) {
			return false, nil
		}
	}
}

// readGrepLine reads a single newline-terminated line from r, dropping the
// trailing newline. Lines longer than maxGrepLineBytes are capped and the
// remainder is discarded so a pathological line cannot exhaust memory. A final
// line without a trailing newline is returned with err == io.EOF.
func readGrepLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		b, e := r.ReadByte()
		if e != nil {
			return buf, e
		}
		if b == '\n' {
			return buf, nil
		}
		if len(buf) < maxGrepLineBytes {
			buf = append(buf, b)
		}
	}
}

// newGrepMatch builds a match, base64-encoding the line and tagging it when the
// bytes are not valid UTF-8.
func newGrepMatch(stream string, lineNo int64, line []byte) GrepMatch {
	if utf8.Valid(line) {
		return GrepMatch{Stream: stream, LineNumber: lineNo, Line: string(line)}
	}
	return GrepMatch{
		Stream:          stream,
		LineNumber:      lineNo,
		Line:            base64.StdEncoding.EncodeToString(line),
		ContentEncoding: grepContentEncodingBase64,
	}
}
