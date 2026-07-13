package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ripta/rt/pkg/cg"
)

// seedNote writes a note JSON file directly under cg.NotesRoot with a controlled
// ID and created_at, so ordering tests do not depend on wall-clock resolution.
func seedNote(t *testing.T, id string, createdAt time.Time, keys map[string]string, message string) {
	t.Helper()
	root := cg.NotesRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir notes root: %v", err)
	}
	n := cg.Note{ID: id, CreatedAt: createdAt, Keys: keys, Message: message}
	data, err := json.MarshalIndent(&n, "", "  ")
	if err != nil {
		t.Fatalf("marshal note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, id+".json"), data, 0o644); err != nil {
		t.Fatalf("write note %s: %v", id, err)
	}
}

func noteAdd(t *testing.T, in noteAddInput) note {
	t.Helper()
	_, out, err := handleNoteAdd(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("handleNoteAdd: %v", err)
	}
	return out
}

func noteList(t *testing.T, in noteListInput) noteListOutput {
	t.Helper()
	_, out, err := handleNoteList(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("handleNoteList: %v", err)
	}
	return out
}

func TestHandleNoteAddThenList(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	added := noteAdd(t, noteAddInput{Message: "baseline suite is green", Keys: map[string]string{"run": "4KQ2ZP"}})
	if !cg.IsValidRunID(added.ID) {
		t.Errorf("added ID %q is not a valid Crockford ID", added.ID)
	}
	if added.Message != "baseline suite is green" {
		t.Errorf("Message = %q, want %q", added.Message, "baseline suite is green")
	}
	if added.Keys["run"] != "4KQ2ZP" {
		t.Errorf("Keys[run] = %q, want 4KQ2ZP", added.Keys["run"])
	}

	out := noteList(t, noteListInput{})
	if len(out.Notes) != 1 {
		t.Fatalf("len(notes) = %d, want 1", len(out.Notes))
	}
	if out.Notes[0].ID != added.ID {
		t.Errorf("listed ID = %q, want %q", out.Notes[0].ID, added.ID)
	}
}

func TestHandleNoteAddEmptyMessage(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleNoteAdd(context.Background(), nil, noteAddInput{Message: ""})
	if err == nil {
		t.Fatalf("expected error for empty message")
	}
}

func TestHandleNoteListNewestFirst(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, "AAAAAA", base, nil, "oldest")
	seedNote(t, "BBBBBB", base.Add(time.Minute), nil, "middle")
	seedNote(t, "CCCCCC", base.Add(2*time.Minute), nil, "newest")

	out := noteList(t, noteListInput{})
	if len(out.Notes) != 3 {
		t.Fatalf("len(notes) = %d, want 3", len(out.Notes))
	}
	want := []string{"newest", "middle", "oldest"}
	for i, w := range want {
		if out.Notes[i].Message != w {
			t.Errorf("notes[%d].Message = %q, want %q", i, out.Notes[i].Message, w)
		}
	}
}

func TestHandleNoteListKeyExistenceFilter(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, "AAAAAA", base, map[string]string{"run": "4KQ2ZP"}, "has run")
	seedNote(t, "BBBBBB", base.Add(time.Minute), map[string]string{"env": "ci"}, "no run")

	out := noteList(t, noteListInput{Key: "run"})
	if len(out.Notes) != 1 {
		t.Fatalf("len(notes) = %d, want 1", len(out.Notes))
	}
	if out.Notes[0].Message != "has run" {
		t.Errorf("Message = %q, want %q", out.Notes[0].Message, "has run")
	}
}

func TestHandleNoteListKeyValueFilter(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, "AAAAAA", base, map[string]string{"run": "4KQ2ZP"}, "match")
	seedNote(t, "BBBBBB", base.Add(time.Minute), map[string]string{"run": "ZZZZZZ"}, "other")

	want := "4KQ2ZP"
	out := noteList(t, noteListInput{Key: "run", Value: &want})
	if len(out.Notes) != 1 {
		t.Fatalf("len(notes) = %d, want 1", len(out.Notes))
	}
	if out.Notes[0].Message != "match" {
		t.Errorf("Message = %q, want %q", out.Notes[0].Message, "match")
	}
}

func TestHandleNoteListEmptyValueMatches(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, "AAAAAA", base, map[string]string{"flag": ""}, "empty value")
	seedNote(t, "BBBBBB", base.Add(time.Minute), map[string]string{"flag": "set"}, "set value")

	empty := ""
	out := noteList(t, noteListInput{Key: "flag", Value: &empty})
	if len(out.Notes) != 1 {
		t.Fatalf("len(notes) = %d, want 1", len(out.Notes))
	}
	if out.Notes[0].Message != "empty value" {
		t.Errorf("Message = %q, want %q", out.Notes[0].Message, "empty value")
	}
}

func TestHandleNoteListDefaultLimit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("A%05d", i)
		seedNote(t, id, base.Add(time.Duration(i)*time.Minute), nil, id)
	}

	out := noteList(t, noteListInput{})
	if len(out.Notes) != defaultListLimit {
		t.Fatalf("len(notes) = %d, want %d", len(out.Notes), defaultListLimit)
	}
}

func TestHandleNoteListExplicitLimit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("A%05d", i)
		seedNote(t, id, base.Add(time.Duration(i)*time.Minute), nil, id)
	}

	out := noteList(t, noteListInput{Limit: 3})
	if len(out.Notes) != 3 {
		t.Fatalf("len(notes) = %d, want 3", len(out.Notes))
	}
}

func TestHandleNoteListEmptyStore(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	out := noteList(t, noteListInput{})
	if out.Notes == nil {
		t.Errorf("Notes = nil, want empty slice")
	}
	if len(out.Notes) != 0 {
		t.Errorf("len(notes) = %d, want 0", len(out.Notes))
	}
}

func TestHandleNoteDelete(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	added := noteAdd(t, noteAddInput{Message: "temporary"})

	_, out, err := handleNoteDelete(context.Background(), nil, noteDeleteInput{ID: added.ID})
	if err != nil {
		t.Fatalf("handleNoteDelete: %v", err)
	}
	if !out.Deleted || out.ID != added.ID {
		t.Errorf("out = %+v, want deleted %q", out, added.ID)
	}

	remaining := noteList(t, noteListInput{})
	if len(remaining.Notes) != 0 {
		t.Errorf("len(notes) = %d after delete, want 0", len(remaining.Notes))
	}
}

func TestHandleNoteDeleteUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleNoteDelete(context.Background(), nil, noteDeleteInput{ID: "ZZZZZZ"})
	if err == nil {
		t.Fatalf("expected error for unknown ID")
	}
	if !strings.Contains(err.Error(), "unknown note id") {
		t.Errorf("error = %q, want unknown note id message", err.Error())
	}
}

func TestHandleNoteDeleteInvalidID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, err := handleNoteDelete(context.Background(), nil, noteDeleteInput{ID: "../evil"})
	if err == nil {
		t.Fatalf("expected error for invalid ID")
	}
	if !strings.Contains(err.Error(), "invalid note id") {
		t.Errorf("error = %q, want invalid note id message", err.Error())
	}
}

func noteGrep(t *testing.T, in noteGrepInput) noteListOutput {
	t.Helper()
	_, out, err := handleNoteGrep(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("handleNoteGrep: %v", err)
	}
	return out
}

func TestHandleNoteGrepText(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, "AAAAAA", base, nil, "baseline suite is green")
	seedNote(t, "BBBBBB", base.Add(time.Minute), nil, "migration pending")

	out := noteGrep(t, noteGrepInput{Text: "green"})
	if len(out.Notes) != 1 {
		t.Fatalf("len(notes) = %d, want 1", len(out.Notes))
	}
	if out.Notes[0].Message != "baseline suite is green" {
		t.Errorf("Message = %q, want green note", out.Notes[0].Message)
	}
}

func TestHandleNoteGrepPatternNewestFirst(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, "AAAAAA", base, nil, "run 4KQ2ZP is slow")
	seedNote(t, "BBBBBB", base.Add(time.Minute), nil, "run ZZ11YY is fast")

	out := noteGrep(t, noteGrepInput{Pattern: `run \w+ is`})
	if len(out.Notes) != 2 {
		t.Fatalf("len(notes) = %d, want 2", len(out.Notes))
	}
	if out.Notes[0].ID != "BBBBBB" {
		t.Errorf("first ID = %q, want BBBBBB (newest-first)", out.Notes[0].ID)
	}
}

func TestHandleNoteGrepCaseInsensitive(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedNote(t, "AAAAAA", time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC), nil, "Migration DONE")

	sensitive := noteGrep(t, noteGrepInput{Text: "migration"})
	if len(sensitive.Notes) != 0 {
		t.Errorf("case-sensitive len = %d, want 0", len(sensitive.Notes))
	}
	insensitive := noteGrep(t, noteGrepInput{Text: "migration", CaseInsensitive: true})
	if len(insensitive.Notes) != 1 {
		t.Errorf("case-insensitive len = %d, want 1", len(insensitive.Notes))
	}
}

func TestHandleNoteGrepEmptyResult(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	seedNote(t, "AAAAAA", time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC), nil, "nothing here")

	out := noteGrep(t, noteGrepInput{Text: "absent"})
	if out.Notes == nil {
		t.Errorf("Notes = nil, want empty slice")
	}
	if len(out.Notes) != 0 {
		t.Errorf("len(notes) = %d, want 0", len(out.Notes))
	}
}

func TestHandleNoteGrepRequiresExactlyOneQuery(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, _, neither := handleNoteGrep(context.Background(), nil, noteGrepInput{})
	if neither == nil {
		t.Errorf("expected error when neither text nor pattern set")
	}
	_, _, both := handleNoteGrep(context.Background(), nil, noteGrepInput{Text: "a", Pattern: "b"})
	if both == nil {
		t.Errorf("expected error when both text and pattern set")
	}
}
