package model

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeStreams seeds a finished run dir with the given stdout and stderr.
func writeStreams(t *testing.T, id, stdout, stderr string) {
	t.Helper()
	dir := seedRunDir(t, id, &Meta{RunInfo: RunInfo{ID: id, Command: []string{"echo", "hi"}}})
	if err := os.WriteFile(filepath.Join(dir, "stdout"), []byte(stdout), 0o644); err != nil {
		t.Fatalf("writing stdout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stderr"), []byte(stderr), 0o644); err != nil {
		t.Fatalf("writing stderr: %v", err)
	}
}

func TestGrepTextAcrossStreams(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "ok pkg/a\nbuilding\nok pkg/b\n", "warn\nerror: boom\n")

	res, err := Grep("AAAAAA", GrepOptions{Text: "o"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if res.MatchCount != 3 {
		t.Fatalf("match_count = %d, want 3", res.MatchCount)
	}
}

func TestGrepRequiresExactlyOneQuery(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "x\n", "")

	if _, err := Grep("AAAAAA", GrepOptions{}); err == nil {
		t.Errorf("expected error when neither text nor pattern set")
	}
	if _, err := Grep("AAAAAA", GrepOptions{Text: "a", Pattern: "b"}); err == nil {
		t.Errorf("expected error when both text and pattern set")
	}
}

func TestGrepUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := Grep("ZZZZZZ", GrepOptions{Text: "x"}); !errors.Is(err, ErrUnknownRunID) {
		t.Errorf("err = %v, want ErrUnknownRunID", err)
	}
}

func TestGrepCommandJSON(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writeStreams(t, "AAAAAA", "match here\nnope\n", "")

	stdout, _, err := runCgSplit("grep", "AAAAAA", "--text", "match", "--streams", "stdout")
	if err != nil {
		t.Fatalf("grep command: %v", err)
	}
	var res GrepResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("unmarshal stdout %q: %v", stdout, err)
	}
	if res.MatchCount != 1 || res.Matches[0].Line != "match here" {
		t.Errorf("res = %+v, want one match 'match here'", res)
	}
}

func TestGrepCommandUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, stderr, err := runCgSplit("grep", "ABCDEF", "--text", "x")
	assertExitCode1(t, err)
	if stderr != "unknown run id: ABCDEF\n" {
		t.Errorf("stderr = %q, want unknown-id line", stderr)
	}
}
