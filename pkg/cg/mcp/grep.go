package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"unicode/utf8"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg"
)

const (
	defaultMaxMatches = 1000
	maxMaxMatches     = 10000
	maxGrepLineBytes  = 65536

	grepStreamsAll    = "all"
	grepStreamsStdout = "stdout"
	grepStreamsStderr = "stderr"
)

// grepInput is the argument shape for `cg_grep`. Exactly one of text or pattern
// must be set: text is a fixed-string substring search, pattern is an RE2 regex.
type grepInput struct {
	ID              string `json:"id" jsonschema:"capture run ID"`
	Text            string `json:"text,omitempty" jsonschema:"fixed-string substring to match; mutually exclusive with pattern"`
	Pattern         string `json:"pattern,omitempty" jsonschema:"RE2 regular expression to match; mutually exclusive with text"`
	Streams         string `json:"streams,omitempty" jsonschema:"which streams to search: all (default), stdout, or stderr"`
	CaseInsensitive bool   `json:"case_insensitive,omitempty" jsonschema:"fold case when matching"`
	InvertMatch     bool   `json:"invert_match,omitempty" jsonschema:"return lines that do NOT match"`
	MaxMatches      int    `json:"max_matches,omitempty" jsonschema:"cap on returned matches; default 1000, max 10000"`
}

// grepMatch is one matching line. ContentEncoding is omitted (meaning utf8) for
// valid UTF-8 lines and set to "base64" when the line carries invalid bytes, in
// which case Line is base64-encoded.
type grepMatch struct {
	Stream          string `json:"stream"`
	LineNumber      int64  `json:"line_number"`
	Line            string `json:"line"`
	ContentEncoding string `json:"content_encoding,omitempty"`
}

// grepOutput is the result shape for `cg_grep`. Truncated reports that the
// max_matches cap was hit before the targeted streams were fully scanned.
type grepOutput struct {
	Matches    []grepMatch `json:"matches"`
	MatchCount int         `json:"match_count"`
	Truncated  bool        `json:"truncated"`
}

func registerGrep(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_grep",
		Description: "Search a run's captured output line by line and return matching lines with stream and 1-based line number. Supply exactly one of text (fixed string) or pattern (RE2 regex). Searches both streams by default; streams selects stdout or stderr. Supports case_insensitive and invert_match. Works for in-flight runs. Lines with invalid UTF-8 are base64-encoded and tagged content_encoding: \"base64\".",
	}, handleGrep)
}

func handleGrep(_ context.Context, _ *mcpsdk.CallToolRequest, in grepInput) (*mcpsdk.CallToolResult, grepOutput, error) {
	if (in.Text == "") == (in.Pattern == "") {
		return nil, grepOutput{}, fmt.Errorf("exactly one of text or pattern must be set")
	}

	streams := in.Streams
	if streams == "" {
		streams = grepStreamsAll
	}
	switch streams {
	case grepStreamsAll, grepStreamsStdout, grepStreamsStderr:
	default:
		return nil, grepOutput{}, fmt.Errorf("invalid streams %q: want all|stdout|stderr", in.Streams)
	}

	if in.MaxMatches < 0 {
		return nil, grepOutput{}, fmt.Errorf("max_matches must be non-negative")
	}
	maxMatches := in.MaxMatches
	if maxMatches == 0 {
		maxMatches = defaultMaxMatches
	}
	if maxMatches > maxMaxMatches {
		maxMatches = maxMaxMatches
	}

	matcher, err := buildMatcher(in)
	if err != nil {
		return nil, grepOutput{}, err
	}

	dir, err := cg.LookupRunDir(in.ID)
	if err != nil && !errors.Is(err, cg.ErrIncompleteRun) && !errors.Is(err, cg.ErrFailedRun) {
		return nil, grepOutput{}, mapLookupError(in.ID, err)
	}

	out := grepOutput{Matches: []grepMatch{}}
	for _, name := range targetStreams(streams) {
		more, err := grepStream(filepath.Join(dir, name), name, matcher, maxMatches, &out.Matches)
		if err != nil {
			return nil, grepOutput{}, err
		}
		if more {
			out.Truncated = true
			break
		}
	}

	out.MatchCount = len(out.Matches)
	return nil, out, nil
}

// targetStreams expands the streams selector into the ordered file names to
// scan. all scans stdout before stderr so the result ordering is deterministic.
func targetStreams(streams string) []string {
	switch streams {
	case grepStreamsStdout:
		return []string{grepStreamsStdout}
	case grepStreamsStderr:
		return []string{grepStreamsStderr}
	default:
		return []string{grepStreamsStdout, grepStreamsStderr}
	}
}

// buildMatcher compiles the input into a single line predicate. invert_match
// negates the predicate after the underlying match test.
func buildMatcher(in grepInput) (func([]byte) bool, error) {
	var match func([]byte) bool
	if in.Text != "" {
		if in.CaseInsensitive {
			needle := bytes.ToLower([]byte(in.Text))
			match = func(line []byte) bool { return bytes.Contains(bytes.ToLower(line), needle) }
		} else {
			needle := []byte(in.Text)
			match = func(line []byte) bool { return bytes.Contains(line, needle) }
		}
	} else {
		expr := in.Pattern
		if in.CaseInsensitive {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern: %w", err)
		}
		match = re.Match
	}
	if in.InvertMatch {
		inner := match
		return func(line []byte) bool { return !inner(line) }, nil
	}
	return match, nil
}

// grepStream scans path line by line, appending matches to *acc until the
// max_matches cap is reached. It returns more=true when matches remain beyond
// the cap. A missing file yields no matches and no error, since the run dir was
// already validated by the caller.
func grepStream(path, stream string, matcher func([]byte) bool, maxMatches int, acc *[]grepMatch) (bool, error) {
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
func newGrepMatch(stream string, lineNo int64, line []byte) grepMatch {
	if utf8.Valid(line) {
		return grepMatch{Stream: stream, LineNumber: lineNo, Line: string(line)}
	}
	return grepMatch{
		Stream:          stream,
		LineNumber:      lineNo,
		Line:            base64.StdEncoding.EncodeToString(line),
		ContentEncoding: contentEncodingBase64,
	}
}
