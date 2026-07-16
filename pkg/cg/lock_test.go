package cg

import (
	"errors"
	"testing"
)

func TestAcquireRunLock(t *testing.T) {
	dir := t.TempDir()

	first, err := acquireRunLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := acquireRunLock(dir); !errors.Is(err, errRunLockHeld) {
		t.Fatalf("second acquire: got %v, want errRunLockHeld", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("releasing lock: %v", err)
	}

	again, err := acquireRunLock(dir)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	again.Close()
}

func TestRunLockReleased(t *testing.T) {
	dir := t.TempDir()

	if RunLockReleased(dir) {
		t.Fatal("missing lock file: got released, want not")
	}

	lock, err := acquireRunLock(dir)
	if err != nil {
		t.Fatalf("acquiring lock: %v", err)
	}

	if RunLockReleased(dir) {
		t.Fatal("held lock: got released, want not")
	}

	if err := lock.Close(); err != nil {
		t.Fatalf("releasing lock: %v", err)
	}

	if !RunLockReleased(dir) {
		t.Fatal("released lock: got not released, want released")
	}
}
