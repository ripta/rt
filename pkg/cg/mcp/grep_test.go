package mcp

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg"
)

// writeStreams seeds $TMPDIR/cg/<id>/ with a finished-looking run and the given
// stdout and stderr contents.
func writeStreams(t *testing.T, id, stdout, stderr string) {
	t.Helper()
	seedRunDir(t, id, &cg.Meta{ID: id, Command: []string{"echo", "hi"}})
	root := cg.CaptureRoot()
	if err := os.WriteFile(filepath.Join(root, id, "stdout"), []byte(stdout), 0o644); err != nil {
		t.Fatalf("writing stdout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, id, "stderr"), []byte(stderr), 0o644); err != nil {
		t.Fatalf("writing stderr: %v", err)
	}
}

func grep(t *testing.T, in grepInput) grepOutput {
	t.Helper()
	_, out, err := handleGrep(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("handleGrep: %v", err)
	}
	return out
}

func TestHandleGrepTextAcrossStreams(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "ok pkg/a\nbuilding\nok pkg/b\n", "warn: x\nerror: boom\n")

	out := grep(t, grepInput{ID: "AAAAAA", Text: "o"})
	// stdout matches: "ok pkg/a" (1), "ok pkg/b" (3); stderr: "error: boom" (2).
	if out.MatchCount != 3 {
		t.Fatalf("match_count = %d, want 3", out.MatchCount)
	}
	if out.Truncated {
		t.Errorf("Truncated = true, want false")
	}
	want := []grepMatch{
		{Stream: "stdout", LineNumber: 1, Line: "ok pkg/a"},
		{Stream: "stdout", LineNumber: 3, Line: "ok pkg/b"},
		{Stream: "stderr", LineNumber: 2, Line: "error: boom"},
	}
	for i, w := range want {
		got := out.Matches[i]
		if got.Stream != w.Stream || got.LineNumber != w.LineNumber || got.Line != w.Line {
			t.Errorf("match[%d] = %+v, want %+v", i, got, w)
		}
	}
}

func TestHandleGrepStreamsStdoutOnly(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "match here\n", "match there\n")

	out := grep(t, grepInput{ID: "AAAAAA", Text: "match", Streams: "stdout"})
	if out.MatchCount != 1 {
		t.Fatalf("match_count = %d, want 1", out.MatchCount)
	}
	if out.Matches[0].Stream != "stdout" {
		t.Errorf("stream = %q, want stdout", out.Matches[0].Stream)
	}
}

func TestHandleGrepStreamsStderrOnly(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "match here\n", "match there\n")

	out := grep(t, grepInput{ID: "AAAAAA", Text: "match", Streams: "stderr"})
	if out.MatchCount != 1 {
		t.Fatalf("match_count = %d, want 1", out.MatchCount)
	}
	if out.Matches[0].Stream != "stderr" {
		t.Errorf("stream = %q, want stderr", out.Matches[0].Stream)
	}
}

func TestHandleGrepPattern(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "FAIL: a_test.go:10\nok\nFAIL: b_test.go:20\n", "")

	out := grep(t, grepInput{ID: "AAAAAA", Pattern: `^FAIL: \w+_test\.go:\d+`})
	if out.MatchCount != 2 {
		t.Fatalf("match_count = %d, want 2", out.MatchCount)
	}
}

func TestHandleGrepInvalidPattern(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "x\n", "")

	_, _, err := handleGrep(context.Background(), nil, grepInput{ID: "AAAAAA", Pattern: "("})
	if err == nil {
		t.Fatalf("expected error for invalid pattern")
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Errorf("error = %q, want pattern mention", err.Error())
	}
}

func TestHandleGrepCaseInsensitiveText(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "Error: boom\nfine\n", "")

	sensitive := grep(t, grepInput{ID: "AAAAAA", Text: "error"})
	if sensitive.MatchCount != 0 {
		t.Errorf("case-sensitive match_count = %d, want 0", sensitive.MatchCount)
	}
	insensitive := grep(t, grepInput{ID: "AAAAAA", Text: "error", CaseInsensitive: true})
	if insensitive.MatchCount != 1 {
		t.Errorf("case-insensitive match_count = %d, want 1", insensitive.MatchCount)
	}
}

func TestHandleGrepCaseInsensitivePattern(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "WARNING here\n", "")

	out := grep(t, grepInput{ID: "AAAAAA", Pattern: "warning", CaseInsensitive: true})
	if out.MatchCount != 1 {
		t.Errorf("match_count = %d, want 1", out.MatchCount)
	}
}

func TestHandleGrepInvertMatch(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "keep\ndrop\nkeep\n", "")

	out := grep(t, grepInput{ID: "AAAAAA", Text: "drop", Streams: "stdout", InvertMatch: true})
	if out.MatchCount != 2 {
		t.Fatalf("match_count = %d, want 2", out.MatchCount)
	}
	for _, m := range out.Matches {
		if m.Line != "keep" {
			t.Errorf("line = %q, want keep", m.Line)
		}
	}
}

func TestHandleGrepRequiresExactlyOneQuery(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "x\n", "")

	_, _, neither := handleGrep(context.Background(), nil, grepInput{ID: "AAAAAA"})
	if neither == nil {
		t.Errorf("expected error when neither text nor pattern set")
	}
	_, _, both := handleGrep(context.Background(), nil, grepInput{ID: "AAAAAA", Text: "a", Pattern: "b"})
	if both == nil {
		t.Errorf("expected error when both text and pattern set")
	}
}

func TestHandleGrepMaxMatchesTruncates(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "m\nm\nm\nm\n", "")

	out := grep(t, grepInput{ID: "AAAAAA", Text: "m", Streams: "stdout", MaxMatches: 2})
	if out.MatchCount != 2 {
		t.Fatalf("match_count = %d, want 2", out.MatchCount)
	}
	if !out.Truncated {
		t.Errorf("Truncated = false, want true")
	}
}

func TestHandleGrepNoTruncateAtExactCap(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "m\nm\n", "")

	out := grep(t, grepInput{ID: "AAAAAA", Text: "m", Streams: "stdout", MaxMatches: 2})
	if out.MatchCount != 2 {
		t.Fatalf("match_count = %d, want 2", out.MatchCount)
	}
	if out.Truncated {
		t.Errorf("Truncated = true, want false")
	}
}

func TestHandleGrepInvalidStreams(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "x\n", "")

	_, _, err := handleGrep(context.Background(), nil, grepInput{ID: "AAAAAA", Text: "x", Streams: "both"})
	if err == nil {
		t.Fatalf("expected error for invalid streams")
	}
	if !strings.Contains(err.Error(), "streams") {
		t.Errorf("error = %q, want streams mention", err.Error())
	}
}

func TestHandleGrepNegativeMaxMatches(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "x\n", "")

	_, _, err := handleGrep(context.Background(), nil, grepInput{ID: "AAAAAA", Text: "x", MaxMatches: -1})
	if err == nil {
		t.Fatalf("expected error for negative max_matches")
	}
}

func TestHandleGrepUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleGrep(context.Background(), nil, grepInput{ID: "ZZZZZZ", Text: "x"})
	if err == nil {
		t.Fatalf("expected error for unknown ID")
	}
	if !strings.Contains(err.Error(), "unknown run id") {
		t.Errorf("error = %q, want unknown run id message", err.Error())
	}
}

func TestHandleGrepIncompleteRun(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	dir := filepath.Join(cg.CaptureRoot(), "AAAAAA")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stdout"), []byte("partial match\n"), 0o644); err != nil {
		t.Fatalf("write stdout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stderr"), nil, 0o644); err != nil {
		t.Fatalf("write stderr: %v", err)
	}

	out := grep(t, grepInput{ID: "AAAAAA", Text: "match"})
	if out.MatchCount != 1 {
		t.Fatalf("match_count = %d, want 1", out.MatchCount)
	}
}

func TestHandleGrepFinalLineNoNewline(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "first\nlast match", "")

	out := grep(t, grepInput{ID: "AAAAAA", Text: "match", Streams: "stdout"})
	if out.MatchCount != 1 {
		t.Fatalf("match_count = %d, want 1", out.MatchCount)
	}
	if out.Matches[0].LineNumber != 2 || out.Matches[0].Line != "last match" {
		t.Errorf("match = %+v, want line 2 'last match'", out.Matches[0])
	}
}

func TestHandleGrepInvalidUTF8LineBase64(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	raw := []byte("match\xff\xfe\n")
	writeStreams(t, "AAAAAA", string(raw), "")

	out := grep(t, grepInput{ID: "AAAAAA", Text: "match", Streams: "stdout"})
	if out.MatchCount != 1 {
		t.Fatalf("match_count = %d, want 1", out.MatchCount)
	}
	m := out.Matches[0]
	if m.ContentEncoding != "base64" {
		t.Fatalf("content_encoding = %q, want base64", m.ContentEncoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(m.Line)
	if err != nil {
		t.Fatalf("decoding base64: %v", err)
	}
	if string(decoded) != "match\xff\xfe" {
		t.Errorf("decoded = %q, want %q", decoded, "match\xff\xfe")
	}
}

func TestHandleGrepNoMatchesEmptySlice(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "nothing\n", "here\n")

	out := grep(t, grepInput{ID: "AAAAAA", Text: "absent"})
	if out.MatchCount != 0 {
		t.Errorf("match_count = %d, want 0", out.MatchCount)
	}
	if out.Matches == nil {
		t.Errorf("Matches = nil, want empty slice")
	}
}
