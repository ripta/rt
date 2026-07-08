package cg

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

type parseSignalTest struct {
	name    string
	input   string
	want    syscall.Signal
	wantErr bool
}

var parseSignalTests = []parseSignalTest{
	{name: "empty uses default", input: "", want: syscall.SIGTERM},
	{name: "SIGTERM", input: "SIGTERM", want: syscall.SIGTERM},
	{name: "lowercase sigint", input: "sigint", want: syscall.SIGINT},
	{name: "SIGKILL", input: "SIGKILL", want: syscall.SIGKILL},
	{name: "numeric", input: "3", want: syscall.Signal(3)},
	{name: "zero rejected", input: "0", wantErr: true},
	{name: "too large rejected", input: "999", wantErr: true},
	{name: "garbage rejected", input: "SIGFOO", wantErr: true},
}

func TestParseSignal(t *testing.T) {
	for _, tt := range parseSignalTests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSignal(tt.input, syscall.SIGTERM)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseSignal(%q) = %v, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSignal(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseSignal(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestCancelRunAlreadyFinished(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	seedRunDir(t, "AAAAAA", &Meta{ID: "AAAAAA", Command: []string{"echo", "hi"}})

	res, err := CancelRun(context.Background(), "AAAAAA", CancelOptions{})
	if err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if res.Signaled {
		t.Errorf("signaled = true, want false for finished run")
	}
	if !res.Finished {
		t.Errorf("finished = false, want true for finished run")
	}
}

func TestCancelRunUnknownID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := CancelRun(context.Background(), "ZZZZZZ", CancelOptions{}); !errors.Is(err, ErrUnknownRunID) {
		t.Errorf("err = %v, want ErrUnknownRunID", err)
	}
}

func TestCancelRunInvalidSignal(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := CancelRun(context.Background(), "AAAAAA", CancelOptions{Signal: "SIGFOO"}); err == nil {
		t.Fatalf("expected error for invalid signal")
	}
}

func TestCancelRunLive(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	run, err := RunCapture([]string{"sleep", "30"}, nil, "", nil)
	if err != nil {
		t.Fatalf("RunCapture: %v", err)
	}

	res, err := CancelRun(context.Background(), run.ID, CancelOptions{Signal: "SIGKILL"})
	if err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if !res.Signaled {
		t.Errorf("signaled = false, want true for live run")
	}

	select {
	case <-run.Done:
	case <-time.After(5 * time.Second):
		t.Fatalf("run %s did not finish after cancel", run.ID)
	}
}
