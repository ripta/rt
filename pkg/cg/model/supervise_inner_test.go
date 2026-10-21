package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuperviseInnerExec(t *testing.T) {
	cap := newSuperviseDir(t)

	spec := superviseSpec(t, []string{"echo", "a", "b"}, "", nil)
	spec.InnerExec = map[int]string{2: "pinned"}

	if _, _, err := runSupervise(t, cap.Dir, spec); err != nil {
		t.Fatalf("superviseRun: %v", err)
	}

	stdout, err := os.ReadFile(filepath.Join(cap.Dir, "stdout"))
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if string(stdout) != "a pinned\n" {
		t.Errorf("stdout = %q, want the pinned token in place of the typed one", stdout)
	}

	meta, err := ReadMeta(cap.Dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if got := strings.Join(meta.Command, " "); got != "echo a b" {
		t.Errorf("recorded command = %q, want the typed argv", got)
	}
}

func TestSuperviseInnerExecOutOfRange(t *testing.T) {
	for _, offset := range []int{0, 3, -1} {
		cap := newSuperviseDir(t)

		spec := superviseSpec(t, []string{"echo", "a", "b"}, "", nil)
		spec.InnerExec = map[int]string{offset: "pinned"}

		ack, _, err := runSupervise(t, cap.Dir, spec)
		if err == nil || ack.Started || !strings.Contains(ack.StartError, "inner exec offset") {
			t.Errorf("offset %d: ack = %+v, err = %v, want a start error naming the offset", offset, ack, err)
		}
	}
}
