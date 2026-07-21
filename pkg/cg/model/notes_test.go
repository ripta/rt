package model

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// seedNote writes a note JSON file directly under NotesRoot with a controlled
// created_at, so ordering tests do not depend on wall-clock resolution.
func seedNote(t *testing.T, n Note) {
	t.Helper()
	root := NotesRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir notes root: %v", err)
	}
	data, err := json.MarshalIndent(&n, "", "  ")
	if err != nil {
		t.Fatalf("marshal note: %v", err)
	}
	path := filepath.Join(root, n.ID+noteFileSuffix)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write note %s: %v", n.ID, err)
	}
}

func TestAddNoteRoundTrip(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	added, err := AddNote("baseline suite is green", map[string]string{"run": "4KQ2ZP"})
	if err != nil {
		t.Fatalf("AddNote() error = %v", err)
	}
	if !IsValidRunID(added.ID) {
		t.Errorf("note ID %q is not a valid Crockford ID", added.ID)
	}
	if added.Message != "baseline suite is green" {
		t.Errorf("Message = %q, want %q", added.Message, "baseline suite is green")
	}
	if added.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	notes, err := ListNotes(ListNoteOptions{})
	if err != nil {
		t.Fatalf("ListNotes() error = %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("len(notes) = %d, want 1", len(notes))
	}
	if notes[0].ID != added.ID {
		t.Errorf("listed ID = %q, want %q", notes[0].ID, added.ID)
	}
	if notes[0].Keys["run"] != "4KQ2ZP" {
		t.Errorf("Keys[run] = %q, want %q", notes[0].Keys["run"], "4KQ2ZP")
	}

	if err := DeleteNote(added.ID); err != nil {
		t.Fatalf("DeleteNote() error = %v", err)
	}

	notes, err = ListNotes(ListNoteOptions{})
	if err != nil {
		t.Fatalf("ListNotes() after delete error = %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("len(notes) after delete = %d, want 0", len(notes))
	}
}

func TestAddNotePersistsOnDisk(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	added, err := AddNote("hi", nil)
	if err != nil {
		t.Fatalf("AddNote() error = %v", err)
	}

	path := filepath.Join(NotesRoot(), added.ID+noteFileSuffix)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading note file: %v", err)
	}
	var got Note
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal note file: %v", err)
	}
	if got.ID != added.ID || got.Message != "hi" {
		t.Errorf("on-disk note = %+v, want ID %q message %q", got, added.ID, "hi")
	}
}

func TestAddNoteRetriesOnCollision(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	// Force the first two allocations onto a taken ID, then fall back to the
	// real generator.
	collide := "COLIDE"
	seedNote(t, Note{ID: collide, CreatedAt: time.Now(), Message: "existing"})

	calls := 0
	prev := idSource
	idSource = func() (string, error) {
		calls++
		if calls <= 2 {
			return collide, nil
		}
		return generateRunID()
	}
	t.Cleanup(func() { idSource = prev })

	added, err := AddNote("fresh", nil)
	if err != nil {
		t.Fatalf("AddNote() error = %v", err)
	}
	if added.ID == collide {
		t.Fatalf("AddNote returned colliding id %q", collide)
	}
	if calls < 3 {
		t.Errorf("idSource called %d times, want at least 3", calls)
	}
}

func TestAddNoteExhaustsAttempts(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	fixed := "BLOCK1"
	seedNote(t, Note{ID: fixed, CreatedAt: time.Now(), Message: "existing"})

	prev := idSource
	idSource = func() (string, error) { return fixed, nil }
	t.Cleanup(func() { idSource = prev })

	_, err := AddNote("fresh", nil)
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	want := "after 10 attempts"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want substring %q", err, want)
	}
}

type addNoteBoundTest struct {
	name    string
	message string
	keys    map[string]string
}

var addNoteBoundTests = []addNoteBoundTest{
	{name: "empty message", message: ""},
	{name: "oversize message", message: strings.Repeat("x", maxNoteMessageBytes+1)},
	{name: "too many keys", message: "ok", keys: manyKeys(maxNoteKeys + 1)},
	{name: "oversize key", message: "ok", keys: map[string]string{strings.Repeat("k", maxNoteKeyLen+1): "v"}},
	{name: "oversize value", message: "ok", keys: map[string]string{"k": strings.Repeat("v", maxNoteValueLen+1)}},
}

func manyKeys(n int) map[string]string {
	m := make(map[string]string, n)
	for i := range n {
		m["k"+strconv.Itoa(i)] = "v"
	}
	return m
}

func TestAddNoteRejectsBounds(t *testing.T) {
	for _, tt := range addNoteBoundTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			_, err := AddNote(tt.message, tt.keys)
			if err == nil {
				t.Fatalf("AddNote(%q) = nil error, want rejection", tt.name)
			}

			notes, err := ListNotes(ListNoteOptions{})
			if err != nil {
				t.Fatalf("ListNotes() error = %v", err)
			}
			if len(notes) != 0 {
				t.Errorf("store holds %d notes after rejection, want 0", len(notes))
			}
		})
	}
}

func TestListNotesMissingRoot(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	notes, err := ListNotes(ListNoteOptions{})
	if err != nil {
		t.Fatalf("ListNotes() error = %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("len(notes) = %d, want 0", len(notes))
	}
}

func TestListNotesNewestFirst(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Message: "oldest"})
	seedNote(t, Note{ID: "BBBBBB", CreatedAt: base.Add(2 * time.Minute), Message: "newest"})
	seedNote(t, Note{ID: "CCCCCC", CreatedAt: base.Add(1 * time.Minute), Message: "middle"})

	notes, err := ListNotes(ListNoteOptions{})
	if err != nil {
		t.Fatalf("ListNotes() error = %v", err)
	}
	got := []string{notes[0].Message, notes[1].Message, notes[2].Message}
	want := []string{"newest", "middle", "oldest"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestListNotesLimit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Message: "oldest"})
	seedNote(t, Note{ID: "BBBBBB", CreatedAt: base.Add(2 * time.Minute), Message: "newest"})
	seedNote(t, Note{ID: "CCCCCC", CreatedAt: base.Add(1 * time.Minute), Message: "middle"})

	notes, err := ListNotes(ListNoteOptions{Limit: 2})
	if err != nil {
		t.Fatalf("ListNotes() error = %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("len(notes) = %d, want 2", len(notes))
	}
	if notes[0].Message != "newest" || notes[1].Message != "middle" {
		t.Errorf("limited notes = %q, %q, want newest, middle", notes[0].Message, notes[1].Message)
	}
}

func TestListNotesKeyFilter(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Keys: map[string]string{"run": "4KQ2ZP"}, Message: "a"})
	seedNote(t, Note{ID: "BBBBBB", CreatedAt: base.Add(time.Minute), Keys: map[string]string{"run": "ZZZZZZ"}, Message: "b"})
	seedNote(t, Note{ID: "CCCCCC", CreatedAt: base.Add(2 * time.Minute), Keys: map[string]string{"topic": "ci"}, Message: "c"})

	existence, err := ListNotes(ListNoteOptions{Key: "run"})
	if err != nil {
		t.Fatalf("ListNotes(key) error = %v", err)
	}
	if len(existence) != 2 {
		t.Fatalf("key existence match = %d notes, want 2", len(existence))
	}

	valued, err := ListNotes(ListNoteOptions{Key: "run", Value: "4KQ2ZP", HasValue: true})
	if err != nil {
		t.Fatalf("ListNotes(key=value) error = %v", err)
	}
	if len(valued) != 1 || valued[0].ID != "AAAAAA" {
		t.Fatalf("key=value match = %+v, want single AAAAAA", valued)
	}
}

func TestDeleteNoteMissing(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	err := DeleteNote("AAAAAA")
	if !errors.Is(err, ErrUnknownNoteID) {
		t.Errorf("DeleteNote(missing) = %v, want ErrUnknownNoteID", err)
	}
}

type grepNotesTest struct {
	name    string
	opts    GrepNoteOptions
	wantIDs []string
}

var grepNotesTests = []grepNotesTest{
	{name: "fixed string", opts: GrepNoteOptions{Text: "migration"}, wantIDs: []string{"BBBBBB"}},
	{name: "pattern", opts: GrepNoteOptions{Pattern: `run [0-9A-Z]{6}`}, wantIDs: []string{"AAAAAA"}},
	{name: "case insensitive", opts: GrepNoteOptions{Text: "GREEN", CaseInsensitive: true}, wantIDs: []string{"CCCCCC"}},
	{name: "no match", opts: GrepNoteOptions{Text: "nonexistent"}, wantIDs: []string{}},
}

func TestGrepNotes(t *testing.T) {
	for _, tt := range grepNotesTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
			seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Message: "run 4KQ2ZP is the slow one"})
			seedNote(t, Note{ID: "BBBBBB", CreatedAt: base.Add(time.Minute), Message: "waiting on the migration to finish"})
			seedNote(t, Note{ID: "CCCCCC", CreatedAt: base.Add(2 * time.Minute), Message: "baseline suite is green"})

			got, err := GrepNotes(tt.opts)
			if err != nil {
				t.Fatalf("GrepNotes() error = %v", err)
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("GrepNotes() = %d notes, want %d", len(got), len(tt.wantIDs))
			}
			for i, id := range tt.wantIDs {
				if got[i].ID != id {
					t.Errorf("match[%d] ID = %q, want %q", i, got[i].ID, id)
				}
			}
		})
	}
}

func TestGrepNotesNewestFirst(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	seedNote(t, Note{ID: "AAAAAA", CreatedAt: base, Message: "green at HEAD"})
	seedNote(t, Note{ID: "BBBBBB", CreatedAt: base.Add(time.Minute), Message: "green after rebase"})

	got, err := GrepNotes(GrepNoteOptions{Text: "green"})
	if err != nil {
		t.Fatalf("GrepNotes() error = %v", err)
	}
	if len(got) != 2 || got[0].ID != "BBBBBB" || got[1].ID != "AAAAAA" {
		t.Fatalf("grep order = %+v, want BBBBBB then AAAAAA", got)
	}
}

func TestGrepNotesRequiresExactlyOneMode(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	if _, err := GrepNotes(GrepNoteOptions{}); err == nil {
		t.Error("GrepNotes() with neither text nor pattern = nil error, want rejection")
	}
	if _, err := GrepNotes(GrepNoteOptions{Text: "a", Pattern: "b"}); err == nil {
		t.Error("GrepNotes() with both text and pattern = nil error, want rejection")
	}
}
