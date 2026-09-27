package model

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// seedSecret writes a file outside the capture root for planted symlinks to
// point at.
func seedSecret(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(`{"id":"LEAKED"}`), 0o600); err != nil {
		t.Fatalf("writing secret: %v", err)
	}
	return path
}

func TestOpenRunFileRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "stdout")
	if err := os.Symlink(seedSecret(t), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := OpenRunFile(link); !errors.Is(err, ErrNotRegularFile) {
		t.Errorf("OpenRunFile err = %v, want ErrNotRegularFile", err)
	}
}

func TestOpenRunFileRefusesFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stdout")
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	errc := make(chan error, 1)
	go func() {
		_, err := OpenRunFile(path)
		errc <- err
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, ErrNotRegularFile) {
			t.Errorf("OpenRunFile err = %v, want ErrNotRegularFile", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("OpenRunFile blocked on a FIFO")
	}
}

func TestReadMetaRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(seedSecret(t), filepath.Join(dir, MetaFilename)); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if m, err := ReadMeta(dir); err == nil {
		t.Errorf("ReadMeta = %+v, want an error for a symlinked meta.json", m)
	}
}

func TestWritePidFileRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	secret := seedSecret(t)
	if err := os.Symlink(secret, filepath.Join(dir, PidFilename)); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := WritePidFile(dir, 1234); !errors.Is(err, ErrNotRegularFile) {
		t.Errorf("WritePidFile err = %v, want ErrNotRegularFile", err)
	}

	data, err := os.ReadFile(secret)
	if err != nil {
		t.Fatalf("reading secret: %v", err)
	}
	if string(data) != `{"id":"LEAKED"}` {
		t.Errorf("symlink target was overwritten: %q", data)
	}
}

func TestLookupRunDirRefusesSymlinkedRunDir(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(CaptureRoot(), 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	elsewhere := t.TempDir()
	if err := WriteMeta(elsewhere, &Meta{RunInfo: RunInfo{ID: "AAAAAA"}}); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(CaptureRoot(), "AAAAAA")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := LookupRunDir("AAAAAA"); !errors.Is(err, ErrUnknownRunID) {
		t.Errorf("LookupRunDir err = %v, want ErrUnknownRunID", err)
	}
}

func TestCaptureRootRefusesSymlink(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.Symlink(t.TempDir(), CaptureRoot()); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := ReadCaptureRoot(); !errors.Is(err, ErrUntrustedDir) {
		t.Errorf("ReadCaptureRoot err = %v, want ErrUntrustedDir", err)
	}
	if _, err := LookupRunDir("AAAAAA"); !errors.Is(err, ErrUntrustedDir) {
		t.Errorf("LookupRunDir err = %v, want ErrUntrustedDir", err)
	}
	if _, err := NewCapture(); !errors.Is(err, ErrUntrustedDir) {
		t.Errorf("NewCapture err = %v, want ErrUntrustedDir", err)
	}
}

func TestReadCaptureRootMissing(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	if _, err := ReadCaptureRoot(); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadCaptureRoot err = %v, want fs.ErrNotExist", err)
	}
}

func TestReadNotesSkipsSymlinkedNote(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(NotesRoot(), 0o755); err != nil {
		t.Fatalf("mkdir notes: %v", err)
	}
	if err := os.Symlink(seedSecret(t), filepath.Join(NotesRoot(), "AAAAAA"+noteFileSuffix)); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	notes, err := readNotes()
	if err != nil {
		t.Fatalf("readNotes: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("readNotes = %+v, want the symlinked note skipped", notes)
	}
}
