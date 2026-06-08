package mcp

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/cg"
)

// writeStream overwrites $TMPDIR/cg/<id>/<name> with content. The run dir is
// seeded first so the file exists and the run looks well-formed.
func writeStream(t *testing.T, id, name, content string) string {
	t.Helper()
	seedRunDir(t, id, &cg.Meta{ID: id, Command: []string{"echo", "hi"}})
	path := filepath.Join(cg.CaptureRoot(), id, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestHandleStreamHeadShortFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "hi\n")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.Content != "hi\n" {
		t.Errorf("Content = %q, want %q", out.Content, "hi\n")
	}
	if out.ContentEncoding != "utf8" {
		t.Errorf("ContentEncoding = %q, want %q", out.ContentEncoding, "utf8")
	}
	if out.TotalBytes != 3 {
		t.Errorf("TotalBytes = %d, want 3", out.TotalBytes)
	}
	if out.ReturnedBytes != 3 {
		t.Errorf("ReturnedBytes = %d, want 3", out.ReturnedBytes)
	}
	if out.Truncated {
		t.Errorf("Truncated = true, want false")
	}
	if out.Clamped {
		t.Errorf("Clamped = true, want false")
	}
}

func TestHandleStreamHeadTruncated(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "abcdefghij")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA", MaxBytes: 4})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.Content != "abcd" {
		t.Errorf("Content = %q, want %q", out.Content, "abcd")
	}
	if !out.Truncated {
		t.Errorf("Truncated = false, want true")
	}
	if out.TotalBytes != 10 {
		t.Errorf("TotalBytes = %d, want 10", out.TotalBytes)
	}
}

func TestHandleStreamHeadOffset(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "abcdefghij")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA", MaxBytes: 4, Offset: 6})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.Content != "ghij" {
		t.Errorf("Content = %q, want %q", out.Content, "ghij")
	}
	if out.Truncated {
		t.Errorf("Truncated = true, want false")
	}
}

func TestHandleStreamHeadOffsetAtEnd(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "abc")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA", Offset: 3})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.Content != "" {
		t.Errorf("Content = %q, want empty", out.Content)
	}
	if out.ReturnedBytes != 0 {
		t.Errorf("ReturnedBytes = %d, want 0", out.ReturnedBytes)
	}
	if out.Truncated {
		t.Errorf("Truncated = true, want false")
	}
}

func TestHandleStreamTail(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "abcdefghij")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA", MaxBytes: 4, From: "tail"})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.Content != "ghij" {
		t.Errorf("Content = %q, want %q", out.Content, "ghij")
	}
	if out.ContentEncoding != "utf8" {
		t.Errorf("ContentEncoding = %q, want %q", out.ContentEncoding, "utf8")
	}
	if !out.Truncated {
		t.Errorf("Truncated = false, want true")
	}
}

func TestHandleStreamTailSmallerThanFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "abc")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA", MaxBytes: 100, From: "tail"})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.Content != "abc" {
		t.Errorf("Content = %q, want %q", out.Content, "abc")
	}
	if out.Truncated {
		t.Errorf("Truncated = true, want false")
	}
}

func TestHandleStreamClamped(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "ok")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA", MaxBytes: 1 << 24})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if !out.Clamped {
		t.Errorf("Clamped = false, want true")
	}
	if out.Content != "ok" {
		t.Errorf("Content = %q, want %q", out.Content, "ok")
	}
}

func TestHandleStreamStderrFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stderr", "boom\n")

	_, out, err := handleStream("stderr", streamInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.Content != "boom\n" {
		t.Errorf("Content = %q, want %q", out.Content, "boom\n")
	}
}

func TestHandleStreamIncompleteRun(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	dir := filepath.Join(cg.CaptureRoot(), "AAAAAA")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stdout"), []byte("partial"), 0o644); err != nil {
		t.Fatalf("write stdout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stderr"), nil, 0o644); err != nil {
		t.Fatalf("write stderr: %v", err)
	}

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.Content != "partial" {
		t.Errorf("Content = %q, want %q", out.Content, "partial")
	}
}

func TestHandleStreamUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleStream("stdout", streamInput{ID: "ZZZZZZ"})
	if err == nil {
		t.Fatalf("expected error for unknown ID")
	}
	if !strings.Contains(err.Error(), "unknown run id") {
		t.Errorf("error = %q, want unknown run id message", err.Error())
	}
}

func TestHandleStreamInvalidFrom(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "x")

	_, _, err := handleStream("stdout", streamInput{ID: "AAAAAA", From: "middle"})
	if err == nil {
		t.Fatalf("expected error for invalid from")
	}
}

func TestHandleStreamNegativeOffset(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "x")

	_, _, err := handleStream("stdout", streamInput{ID: "AAAAAA", Offset: -1})
	if err == nil {
		t.Fatalf("expected error for negative offset")
	}
}

func TestHandleStreamNegativeMaxBytes(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "x")

	_, _, err := handleStream("stdout", streamInput{ID: "AAAAAA", MaxBytes: -1})
	if err == nil {
		t.Fatalf("expected error for negative max_bytes")
	}
}

func TestHandleStreamUTF8Multibyte(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "héllo")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.Content != "héllo" {
		t.Errorf("Content = %q, want %q", out.Content, "héllo")
	}
	if out.ContentEncoding != "utf8" {
		t.Errorf("ContentEncoding = %q, want %q", out.ContentEncoding, "utf8")
	}
}

func TestHandleStreamBinaryFallsBackToBase64(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	raw := []byte{0xff, 0xfe, 0x00, 0x01}
	writeStream(t, "AAAAAA", "stdout", string(raw))

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.ContentEncoding != "base64" {
		t.Errorf("ContentEncoding = %q, want %q", out.ContentEncoding, "base64")
	}
	want := base64.StdEncoding.EncodeToString(raw)
	if out.Content != want {
		t.Errorf("Content = %q, want %q", out.Content, want)
	}
}

// TestHandleStreamTailMidCodepoint exercises the common case the design calls
// out: a tail read slices a multibyte codepoint, so the returned window
// starts with an orphan continuation byte and the encoder falls back to base64.
func TestHandleStreamTailMidCodepoint(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	// "héllo" is 6 bytes: h(0x68) é(0xC3 0xA9) l(0x6C) l(0x6C) o(0x6F).
	// A tail of 4 bytes starts at offset 2 (0xA9), an orphan continuation
	// byte without its 0xC3 leader.
	writeStream(t, "AAAAAA", "stdout", "héllo")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA", MaxBytes: 4, From: "tail"})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.ContentEncoding != "base64" {
		t.Errorf("ContentEncoding = %q, want %q", out.ContentEncoding, "base64")
	}
	decoded, err := base64.StdEncoding.DecodeString(out.Content)
	if err != nil {
		t.Fatalf("decoding base64 content: %v", err)
	}
	want := []byte("héllo")[2:]
	if string(decoded) != string(want) {
		t.Errorf("decoded = %q, want %q", decoded, want)
	}
}

func TestHandleStreamForcedBase64(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "hi\n")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA", ContentEncoding: "base64"})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.ContentEncoding != "base64" {
		t.Errorf("ContentEncoding = %q, want %q", out.ContentEncoding, "base64")
	}
	want := base64.StdEncoding.EncodeToString([]byte("hi\n"))
	if out.Content != want {
		t.Errorf("Content = %q, want %q", out.Content, want)
	}
}

func TestHandleStreamExplicitUTF8(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "hi\n")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA", ContentEncoding: "utf8"})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.ContentEncoding != "utf8" {
		t.Errorf("ContentEncoding = %q, want %q", out.ContentEncoding, "utf8")
	}
	if out.Content != "hi\n" {
		t.Errorf("Content = %q, want %q", out.Content, "hi\n")
	}
}

func TestHandleStreamInvalidContentEncoding(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "x")

	_, _, err := handleStream("stdout", streamInput{ID: "AAAAAA", ContentEncoding: "hex"})
	if err == nil {
		t.Fatalf("expected error for invalid content_encoding")
	}
	if !strings.Contains(err.Error(), "content_encoding") {
		t.Errorf("error = %q, want content_encoding mention", err.Error())
	}
}

func TestHandleStreamEmptyContentDefaultsToUTF8(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStream(t, "AAAAAA", "stdout", "")

	_, out, err := handleStream("stdout", streamInput{ID: "AAAAAA"})
	if err != nil {
		t.Fatalf("handleStream: %v", err)
	}
	if out.Content != "" {
		t.Errorf("Content = %q, want empty", out.Content)
	}
	if out.ContentEncoding != "utf8" {
		t.Errorf("ContentEncoding = %q, want %q", out.ContentEncoding, "utf8")
	}
}
