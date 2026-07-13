package cg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// NotesDirName is the subdirectory under CaptureRoot() that holds note files.
// The name is not a valid run ID, so the run listing and prune paths skip it.
const NotesDirName = "notes"

// noteFileSuffix is appended to a note ID to form its on-disk file name.
const noteFileSuffix = ".json"

// Note size bounds. The message and keys are untrusted input from an MCP
// client, so a bound keeps a runaway or malicious caller from filling the store
// or exhausting memory.
const (
	maxNoteMessageBytes = 65536
	maxNoteKeys         = 32
	maxNoteKeyLen       = 128
	maxNoteValueLen     = 1024
)

// ErrUnknownNoteID is returned by DeleteNote when no note with the given ID
// exists in the store.
var ErrUnknownNoteID = errors.New("unknown note id")

// ErrEmptyNoteMessage is returned by AddNote when the message is empty.
var ErrEmptyNoteMessage = errors.New("note message must not be empty")

// NotesRoot returns the directory that holds all note files: $TMPDIR/cg/notes.
func NotesRoot() string {
	return filepath.Join(CaptureRoot(), NotesDirName)
}

// Note is a single free-form memo persisted as one JSON file under NotesRoot.
// Message is required. Keys is an optional set of string tags for filtering.
type Note struct {
	ID        string            `json:"id"`
	CreatedAt time.Time         `json:"created_at"`
	Keys      map[string]string `json:"keys,omitempty"`
	Message   string            `json:"message"`
}

// AddNote validates message and keys, allocates an ID, and writes the note
// atomically under NotesRoot. It returns the stored note.
func AddNote(message string, keys map[string]string) (*Note, error) {
	if message == "" {
		return nil, ErrEmptyNoteMessage
	}
	if len(message) > maxNoteMessageBytes {
		return nil, fmt.Errorf("note message exceeds %d bytes", maxNoteMessageBytes)
	}
	if err := validateNoteKeys(keys); err != nil {
		return nil, err
	}

	n := &Note{CreatedAt: time.Now(), Keys: keys, Message: message}
	if err := writeNote(n); err != nil {
		return nil, err
	}
	return n, nil
}

// validateNoteKeys enforces the key count and per-key/value length bounds.
func validateNoteKeys(keys map[string]string) error {
	if len(keys) > maxNoteKeys {
		return fmt.Errorf("note has %d keys, exceeds %d", len(keys), maxNoteKeys)
	}
	for k, v := range keys {
		if len(k) > maxNoteKeyLen {
			return fmt.Errorf("note key %q exceeds %d bytes", k, maxNoteKeyLen)
		}
		if len(v) > maxNoteValueLen {
			return fmt.Errorf("note value for key %q exceeds %d bytes", k, maxNoteValueLen)
		}
	}
	return nil
}

// writeNote allocates a unique ID under NotesRoot and writes n atomically. It
// reserves the final path with O_EXCL to detect collisions, matching how
// newRunDir uses os.Mkdir, then renames a temp file over the reservation to get
// WriteMeta's atomic-write guarantee.
func writeNote(n *Note) error {
	root := NotesRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("creating notes root: %w", err)
	}

	for range runIDAttempts {
		id, err := idSource()
		if err != nil {
			return err
		}

		final := filepath.Join(root, id+noteFileSuffix)
		f, err := os.OpenFile(final, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reserving note file: %w", err)
		}
		f.Close()

		n.ID = id
		if err := writeNoteFile(root, final, n); err != nil {
			os.Remove(final)
			return err
		}
		return nil
	}

	return fmt.Errorf("could not allocate unique note id after %d attempts", runIDAttempts)
}

// writeNoteFile marshals n and writes it atomically to final via a temp file
// and rename in root, so the rename stays on one filesystem.
func writeNoteFile(root, final string, n *Note) error {
	data, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling note: %w", err)
	}

	tmp, err := os.CreateTemp(root, "note.json.tmp-*")
	if err != nil {
		return fmt.Errorf("creating note tmpfile: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing note tmpfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing note tmpfile: %w", err)
	}

	if err := os.Rename(tmpPath, final); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming note tmpfile: %w", err)
	}
	return nil
}

// ListNoteOptions filters and bounds a ListNotes call. Limit == 0 means no
// limit; the surface layers apply their own caps before calling.
type ListNoteOptions struct {
	// Key, when non-empty, keeps only notes carrying this key.
	Key string
	// Value, when HasValue is true, additionally requires keys[Key] == Value.
	Value string
	// HasValue selects value matching over key-existence matching.
	HasValue bool
	// Limit caps the number of returned notes. Zero means unlimited.
	Limit int
}

// ListNotes returns stored notes newest-first by CreatedAt, filtered and
// limited per opts. A missing notes root yields an empty slice, matching
// PruneRuns. Files that fail to parse are skipped so an in-flight reservation
// does not surface as an error.
func ListNotes(opts ListNoteOptions) ([]Note, error) {
	notes, err := readNotes()
	if err != nil {
		return nil, err
	}

	filtered := notes[:0]
	for _, n := range notes {
		if noteMatchesFilter(n, opts) {
			filtered = append(filtered, n)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}
	return filtered, nil
}

// noteMatchesFilter reports whether n satisfies the key filter in opts. An
// empty Key matches every note.
func noteMatchesFilter(n Note, opts ListNoteOptions) bool {
	if opts.Key == "" {
		return true
	}
	v, ok := n.Keys[opts.Key]
	if !ok {
		return false
	}
	if opts.HasValue {
		return v == opts.Value
	}
	return true
}

// readNotes loads every parseable note under NotesRoot. A missing root is not
// an error.
func readNotes() ([]Note, error) {
	root := NotesRoot()
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return []Note{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading notes root: %w", err)
	}

	notes := make([]Note, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), noteFileSuffix) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		var n Note
		if err := json.Unmarshal(data, &n); err != nil {
			continue
		}
		notes = append(notes, n)
	}
	return notes, nil
}

// DeleteNote removes the note file for id. A missing note yields
// ErrUnknownNoteID.
func DeleteNote(id string) error {
	path := filepath.Join(NotesRoot(), id+noteFileSuffix)
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrUnknownNoteID
	}
	if err != nil {
		return fmt.Errorf("removing note %s: %w", id, err)
	}
	return nil
}

// GrepNoteOptions configures a note-body search. Exactly one of Text or Pattern
// must be set: Text is a fixed-string substring search, Pattern is an RE2 regex.
type GrepNoteOptions struct {
	Text            string
	Pattern         string
	CaseInsensitive bool
}

// GrepNotes returns whole notes whose message matches opts, newest-first by
// CreatedAt. It reuses the run grep match semantics for parity with cg grep.
func GrepNotes(opts GrepNoteOptions) ([]Note, error) {
	if (opts.Text == "") == (opts.Pattern == "") {
		return nil, fmt.Errorf("exactly one of text or pattern must be set")
	}

	matcher, err := buildGrepMatcher(GrepOptions{
		Text:            opts.Text,
		Pattern:         opts.Pattern,
		CaseInsensitive: opts.CaseInsensitive,
	})
	if err != nil {
		return nil, err
	}

	notes, err := readNotes()
	if err != nil {
		return nil, err
	}

	matched := notes[:0]
	for _, n := range notes {
		if matcher([]byte(n.Message)) {
			matched = append(matched, n)
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	return matched, nil
}
