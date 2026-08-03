package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// assertExitCode asserts err is an *ExitError carrying the given code.
func assertExitCode(t *testing.T, err error, want int) {
	t.Helper()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != want {
		t.Errorf("exit code = %d, want %d", exitErr.Code, want)
	}
}

// runCgIn invokes the cg command with a stdin reader and separate stdout/stderr
// buffers, so `cg note add` reading from stdin is deterministic.
func runCgIn(stdin string, args ...string) (stdout, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	cmd := newTestRoot()
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestNoteAddThenLs(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	stdout, _, err := runCgSplit("note", "add", "-k", "run=4KQ2ZP", "baseline green")
	if err != nil {
		t.Fatalf("note add: %v", err)
	}
	id := strings.TrimSpace(stdout)
	if !IsValidRunID(id) {
		t.Fatalf("add printed %q, want a valid note ID", id)
	}

	lsOut, _, err := runCgSplit("note", "ls")
	if err != nil {
		t.Fatalf("note ls: %v", err)
	}
	if !strings.Contains(lsOut, id) {
		t.Errorf("ls output %q missing ID %q", lsOut, id)
	}
	if !strings.Contains(lsOut, "run=4KQ2ZP") {
		t.Errorf("ls output %q missing key tag", lsOut)
	}
	if !strings.Contains(lsOut, "  baseline green") {
		t.Errorf("ls output %q missing indented message", lsOut)
	}
}

func TestNoteAddFromStdin(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	stdout, _, err := runCgIn("piped message\n", "note", "add")
	if err != nil {
		t.Fatalf("note add: %v", err)
	}
	id := strings.TrimSpace(stdout)
	if !IsValidRunID(id) {
		t.Fatalf("add printed %q, want a valid note ID", id)
	}

	notes, err := ListNotes(ListNoteOptions{})
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 1 || notes[0].Message != "piped message" {
		t.Errorf("stored notes = %+v, want one note with trimmed message", notes)
	}
}

func TestNoteAddEmptyMessageExit2(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, stderr, err := runCgIn("", "note", "add")
	assertExitCode(t, err, 2)
	if !strings.Contains(stderr, "must not be empty") {
		t.Errorf("stderr = %q, want empty-message error", stderr)
	}
}

func TestNoteAddBareKeyNoValue(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	if _, _, err := runCgSplit("note", "add", "-k", "flag", "tagged"); err != nil {
		t.Fatalf("note add: %v", err)
	}
	notes, err := ListNotes(ListNoteOptions{})
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("len(notes) = %d, want 1", len(notes))
	}
	if v, ok := notes[0].Keys["flag"]; !ok || v != "" {
		t.Errorf("Keys[flag] = %q, ok=%v; want empty value present", v, ok)
	}
}

func TestNoteLsJSON(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Message: "first"})

	stdout, _, err := runCgSplit("note", "ls", "--json")
	if err != nil {
		t.Fatalf("note ls --json: %v", err)
	}
	var res noteListResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("unmarshal %q: %v", stdout, err)
	}
	if len(res.Notes) != 1 || res.Notes[0].ID != "AAAAAA" {
		t.Errorf("notes = %+v, want one note AAAAAA", res.Notes)
	}
}

func TestNoteLsNewestFirst(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Message: "oldest"})
	seedNote(t, Note{ID: "BBBBBB", CreatedAt: base.Add(time.Minute), Message: "middle"})
	seedNote(t, Note{ID: "CCCCCC", CreatedAt: base.Add(2 * time.Minute), Message: "newest"})

	stdout, _, err := runCgSplit("note", "ls")
	if err != nil {
		t.Fatalf("note ls: %v", err)
	}
	if strings.Index(stdout, "newest") > strings.Index(stdout, "oldest") {
		t.Errorf("ls output not newest-first:\n%s", stdout)
	}
}

func TestNoteLsKeyExistenceFilter(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Keys: map[string]string{"run": "4KQ2ZP"}, Message: "has run"})
	seedNote(t, Note{ID: "BBBBBB", CreatedAt: base.Add(time.Minute), Keys: map[string]string{"env": "ci"}, Message: "no run"})

	stdout, _, err := runCgSplit("note", "ls", "-k", "run")
	if err != nil {
		t.Fatalf("note ls -k run: %v", err)
	}
	if !strings.Contains(stdout, "has run") || strings.Contains(stdout, "no run") {
		t.Errorf("ls -k run output = %q, want only 'has run'", stdout)
	}
}

func TestNoteLsKeyValueFilter(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Keys: map[string]string{"run": "4KQ2ZP"}, Message: "match"})
	seedNote(t, Note{ID: "BBBBBB", CreatedAt: base.Add(time.Minute), Keys: map[string]string{"run": "ZZZZZZ"}, Message: "other"})

	stdout, _, err := runCgSplit("note", "ls", "-k", "run=4KQ2ZP")
	if err != nil {
		t.Fatalf("note ls -k run=4KQ2ZP: %v", err)
	}
	if !strings.Contains(stdout, "match") || strings.Contains(stdout, "other") {
		t.Errorf("ls -k run=4KQ2ZP output = %q, want only 'match'", stdout)
	}
}

func TestNoteLsLimit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Message: "one"})
	seedNote(t, Note{ID: "BBBBBB", CreatedAt: base.Add(time.Minute), Message: "two"})
	seedNote(t, Note{ID: "CCCCCC", CreatedAt: base.Add(2 * time.Minute), Message: "three"})

	stdout, _, err := runCgSplit("note", "ls", "-n", "1")
	if err != nil {
		t.Fatalf("note ls -n 1: %v", err)
	}
	if !strings.Contains(stdout, "three") || strings.Contains(stdout, "two") {
		t.Errorf("ls -n 1 output = %q, want only newest note 'three'", stdout)
	}
}

func TestNoteGrepText(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Message: "baseline suite is green"})
	seedNote(t, Note{ID: "BBBBBB", CreatedAt: base.Add(time.Minute), Message: "migration pending"})

	stdout, _, err := runCgSplit("note", "grep", "--text", "green")
	if err != nil {
		t.Fatalf("note grep --text green: %v", err)
	}
	if !strings.Contains(stdout, "baseline suite is green") || strings.Contains(stdout, "migration") {
		t.Errorf("grep output = %q, want only the green note", stdout)
	}
}

func TestNoteGrepPatternJSON(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Message: "run 4KQ2ZP is slow"})
	seedNote(t, Note{ID: "BBBBBB", CreatedAt: base.Add(time.Minute), Message: "run ZZ11YY is fast"})

	stdout, _, err := runCgSplit("note", "grep", "--pattern", `run \w+ is`, "--json")
	if err != nil {
		t.Fatalf("note grep --pattern --json: %v", err)
	}
	var res noteListResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("unmarshal %q: %v", stdout, err)
	}
	if len(res.Notes) != 2 || res.Notes[0].ID != "BBBBBB" {
		t.Errorf("notes = %+v, want two matches newest-first", res.Notes)
	}
}

func TestNoteGrepIgnoreCase(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedNote(t, Note{ID: "AAAAAA", CreatedAt: time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC), Message: "Migration DONE"})

	sensitive, _, err := runCgSplit("note", "grep", "--text", "migration")
	if err != nil {
		t.Fatalf("note grep sensitive: %v", err)
	}
	if strings.Contains(sensitive, "Migration") {
		t.Errorf("case-sensitive grep matched, output = %q", sensitive)
	}

	insensitive, _, err := runCgSplit("note", "grep", "--text", "migration", "-i")
	if err != nil {
		t.Fatalf("note grep -i: %v", err)
	}
	if !strings.Contains(insensitive, "Migration DONE") {
		t.Errorf("case-insensitive grep missed note, output = %q", insensitive)
	}
}

func TestNoteGrepRequiresExactlyOneQuery(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, neither := runCgSplit("note", "grep")
	assertExitCode(t, neither, 2)

	_, _, both := runCgSplit("note", "grep", "--text", "a", "--pattern", "b")
	assertExitCode(t, both, 2)
}

func TestNoteRm(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedNote(t, Note{ID: "AAAAAA", CreatedAt: time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC), Message: "temporary"})

	stdout, _, err := runCgSplit("note", "rm", "AAAAAA")
	if err != nil {
		t.Fatalf("note rm: %v", err)
	}
	if strings.TrimSpace(stdout) != "AAAAAA" {
		t.Errorf("rm stdout = %q, want AAAAAA", stdout)
	}

	notes, err := ListNotes(ListNoteOptions{})
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("len(notes) = %d after rm, want 0", len(notes))
	}
}

func TestNoteRmUnknownIDExit1(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, stderr, err := runCgSplit("note", "rm", "ZZZZZZ")
	assertExitCode(t, err, 1)
	if !strings.Contains(stderr, "unknown note id: ZZZZZZ") {
		t.Errorf("stderr = %q, want unknown-id message", stderr)
	}
}

func TestNoteRmInvalidIDExit2(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, stderr, err := runCgSplit("note", "rm", "../evil")
	assertExitCode(t, err, 2)
	if !strings.Contains(stderr, "invalid note id") {
		t.Errorf("stderr = %q, want invalid-id message", stderr)
	}
}
