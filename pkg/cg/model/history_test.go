package model

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// historyAppendMain is the body of the re-exec'd "history-append" helper: it appends n run
// records to the history in dir, each tagged with a tag so lines are attributable.
func historyAppendMain(args []string) int {
	if len(args) != 3 {
		return 2
	}

	var n int
	if _, err := fmt.Sscanf(args[2], "%d", &n); err != nil {
		return 2
	}

	h, err := openHistoryAt(args[0])
	if err != nil {
		return 1
	}

	for i := range n {
		if err := h.Append(paddedRecord(fmt.Sprintf("%s-%d", args[1], i))); err != nil {
			return 1
		}
	}

	return 0
}

// paddedRecord returns a record large enough that interleaved writes would corrupt it.
func paddedRecord(id string) *RunHistoryRecord {
	return &RunHistoryRecord{
		V:        HistoryVersion,
		Kind:     HistoryKindRun,
		ID:       id,
		ArgvMACs: slices.Repeat([]string{strings.Repeat("x", 22)}, 64),
	}
}

func newTestHistory(t *testing.T) *History {
	t.Helper()

	h, err := openHistoryAt(filepath.Join(t.TempDir(), "cg"))
	if err != nil {
		t.Fatalf("openHistoryAt: %v", err)
	}
	return h
}

// readHistoryIDs decodes every line of the given log files and returns the record IDs. It
// fails the test on a line that is not a whole JSON record.
func readHistoryIDs(t *testing.T, paths ...string) []string {
	t.Helper()

	var ids []string
	for _, path := range paths {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}

		sc := bufio.NewScanner(f)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			var rec RunHistoryRecord
			if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
				t.Fatalf("%s: partial or corrupt line %q: %v", path, sc.Text(), err)
			}
			ids = append(ids, rec.ID)
		}
		f.Close()
	}
	return ids
}

func TestHistoryConcurrentAppendGoroutines(t *testing.T) {
	h := newTestHistory(t)

	const writers, each = 8, 40

	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// A separate handle per writer, so each append opens its own lock descriptor.
			hw, err := openHistoryAt(h.Dir())
			if err != nil {
				t.Errorf("openHistoryAt: %v", err)
				return
			}
			for i := range each {
				if err := hw.Append(paddedRecord(fmt.Sprintf("g%d-%d", w, i))); err != nil {
					t.Errorf("Append: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	ids := readHistoryIDs(t, h.LogPaths()...)
	if len(ids) != writers*each {
		t.Errorf("got %d whole lines, want %d", len(ids), writers*each)
	}
}

func TestHistoryConcurrentAppendProcesses(t *testing.T) {
	h := newTestHistory(t)

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	const procs, each = 4, 50

	cmds := make([]*exec.Cmd, procs)
	for p := range procs {
		cmds[p] = exec.Command(exe, "history-append", h.Dir(), fmt.Sprintf("p%d", p), fmt.Sprint(each))
		if err := cmds[p].Start(); err != nil {
			t.Fatalf("start helper %d: %v", p, err)
		}
	}
	for p, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Errorf("helper %d: %v", p, err)
		}
	}

	ids := readHistoryIDs(t, h.LogPaths()...)
	if len(ids) != procs*each {
		t.Errorf("got %d whole lines, want %d", len(ids), procs*each)
	}
}

func TestHistoryRotationKeepsOneGeneration(t *testing.T) {
	h := newTestHistory(t)
	h.maxBytes = 4096

	const total = 200
	for i := range total {
		if err := h.Append(paddedRecord(fmt.Sprintf("r%d", i))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(h.Dir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)

	want := []string{HistoryKeyFilename, HistoryLogFilename, HistoryLockFilename, HistoryRotatedFilename}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("history dir = %v, want %v", names, want)
	}

	ids := readHistoryIDs(t, h.LogPaths()...)
	if len(ids) == 0 || len(ids) >= total {
		t.Errorf("kept %d records, want some but fewer than %d after rotation dropped older ones", len(ids), total)
	}
	if got := ids[len(ids)-1]; got != fmt.Sprintf("r%d", total-1) {
		t.Errorf("last record = %q, want the newest", got)
	}
}

type argvMACsTest struct {
	Name   string
	Tokens int
	Want   int
}

var argvMACsTests = []argvMACsTest{
	{Name: "empty", Tokens: 0, Want: 0},
	{Name: "one token", Tokens: 1, Want: 1},
	{Name: "at the limit", Tokens: HistoryMaxArgvMACs, Want: HistoryMaxArgvMACs},
	{Name: "one past the limit adds the full MAC", Tokens: HistoryMaxArgvMACs + 1, Want: HistoryMaxArgvMACs + 1},
	{Name: "far past the limit", Tokens: 500, Want: HistoryMaxArgvMACs + 1},
}

func numberedArgv(n int) []string {
	argv := make([]string, n)
	for i := range argv {
		argv[i] = fmt.Sprintf("arg%d", i)
	}
	return argv
}

func TestHistoryArgvMACsLength(t *testing.T) {
	h := newTestHistory(t)

	for _, test := range argvMACsTests {
		t.Run(test.Name, func(t *testing.T) {
			got := h.ArgvMACs(numberedArgv(test.Tokens))
			if len(got) != test.Want {
				t.Errorf("len = %d, want %d", len(got), test.Want)
			}
		})
	}
}

func TestHistoryArgvMACsPrefixChain(t *testing.T) {
	h := newTestHistory(t)

	argv := []string{"make", "test", "JOBS=10"}
	macs := h.ArgvMACs(argv)

	for k := range argv {
		if want := h.ArgvMAC(argv[:k+1]); macs[k] != want {
			t.Errorf("macs[%d] = %q, want MAC of first %d tokens %q", k, macs[k], k+1, want)
		}
	}

	if macs[0] == h.ArgvMAC([]string{"make", "other"}) {
		t.Error("distinct argvs share a MAC")
	}
}

func TestHistoryLongArgvStillMatchesExactly(t *testing.T) {
	h := newTestHistory(t)

	long := numberedArgv(HistoryMaxArgvMACs + 10)
	macs := h.ArgvMACs(long)
	if got, want := macs[len(macs)-1], h.ArgvMAC(long); got != want {
		t.Errorf("final MAC = %q, want full-argv MAC %q", got, want)
	}

	// Same first 32 tokens, different tail: the prefix chain agrees and the final MAC differs.
	other := slices.Clone(long)
	other[len(other)-1] = "different"
	otherMACs := h.ArgvMACs(other)

	if !slices.Equal(macs[:HistoryMaxArgvMACs], otherMACs[:HistoryMaxArgvMACs]) {
		t.Error("argvs sharing 32 tokens differ in the prefix chain")
	}
	if macs[len(macs)-1] == otherMACs[len(otherMACs)-1] {
		t.Error("argvs differing past token 32 share the exact-match MAC")
	}

	// A query for the long argv must find its exact MAC at the end of a stored chain.
	if !slices.Contains(macs, h.ArgvMAC(long)) {
		t.Error("stored chain does not contain the full-argv MAC")
	}
}

type poolMACTest struct {
	Name     string
	A        [][]string
	ARepeat  int
	APar     int
	B        [][]string
	BRepeat  int
	BPar     int
	WantSame bool
}

var poolMACTests = []poolMACTest{
	{
		Name: "identical", WantSame: true,
		A: [][]string{{"a", "b"}, {"c"}}, ARepeat: 1, APar: 2,
		B: [][]string{{"a", "b"}, {"c"}}, BRepeat: 1, BPar: 2,
	},
	{
		Name: "same tokens split differently",
		A:    [][]string{{"a", "b"}, {"c"}}, ARepeat: 1, APar: 1,
		B: [][]string{{"a"}, {"b", "c"}}, BRepeat: 1, BPar: 1,
	},
	{
		Name: "same tokens as one command versus two",
		A:    [][]string{{"a", "b"}}, ARepeat: 1, APar: 1,
		B: [][]string{{"a"}, {"b"}}, BRepeat: 1, BPar: 1,
	},
	{
		Name: "listed order matters",
		A:    [][]string{{"a"}, {"b"}}, ARepeat: 1, APar: 1,
		B: [][]string{{"b"}, {"a"}}, BRepeat: 1, BPar: 1,
	},
	{
		Name: "repeat matters",
		A:    [][]string{{"a"}}, ARepeat: 1, APar: 1,
		B: [][]string{{"a"}}, BRepeat: 2, BPar: 1,
	},
	{
		Name: "parallelism matters",
		A:    [][]string{{"a"}}, ARepeat: 2, APar: 1,
		B: [][]string{{"a"}}, BRepeat: 2, BPar: 2,
	},
	{
		Name: "repeat and parallelism do not swap",
		A:    [][]string{{"a"}}, ARepeat: 2, APar: 3,
		B: [][]string{{"a"}}, BRepeat: 3, BPar: 2,
	},
}

func TestHistoryPoolMAC(t *testing.T) {
	h := newTestHistory(t)

	for _, test := range poolMACTests {
		t.Run(test.Name, func(t *testing.T) {
			a := h.PoolMAC(test.A, test.ARepeat, test.APar)
			b := h.PoolMAC(test.B, test.BRepeat, test.BPar)
			if (a == b) != test.WantSame {
				t.Errorf("same = %v, want %v", a == b, test.WantSame)
			}
		})
	}
}

func TestHistoryLogHoldsNoArgvOrCwd(t *testing.T) {
	h := newTestHistory(t)

	cwd := t.TempDir()
	meta := &Meta{
		RunInfo: RunInfo{
			ID:        "SECRET1",
			Command:   []string{"deploy", "--token=hunter2-sentinel"},
			Cwd:       cwd,
			StartedAt: time.Now(),
		},
		DurationMs: 12,
	}
	if err := h.Append(h.NewRunRecord(meta, 0)); err != nil {
		t.Fatalf("Append run: %v", err)
	}

	finished := time.Now().UTC()
	pool := &PoolManifest{
		ID:          "POOL1",
		Commands:    [][]string{{"deploy", "--token=hunter2-sentinel"}},
		Repeat:      1,
		Parallelism: 1,
		Cwd:         cwd,
		StartedAt:   finished.Add(-time.Second),
		FinishedAt:  &finished,
	}
	rec, ok := h.NewPoolRecord(pool)
	if !ok {
		t.Fatal("NewPoolRecord returned not ok for a finished pool")
	}
	if err := h.Append(rec); err != nil {
		t.Fatalf("Append pool: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(h.Dir(), HistoryLogFilename))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	for _, secret := range []string{"hunter2-sentinel", "deploy", "--token", cwd, filepath.Base(cwd)} {
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("log contains %q:\n%s", secret, data)
		}
	}

	key, err := os.ReadFile(filepath.Join(h.Dir(), HistoryKeyFilename))
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if bytes.Contains(data, key) {
		t.Error("log contains the key bytes")
	}
}

func TestHistoryRecordShapes(t *testing.T) {
	h := newTestHistory(t)

	sig := 9
	started := time.Date(2026, 10, 9, 18, 10, 23, 0, time.UTC)
	run := h.NewRunRecord(&Meta{
		RunInfo:    RunInfo{ID: "K3FQ2M", Command: []string{"make", "test"}, Cwd: t.TempDir(), Pool: "P1", StartedAt: started},
		DurationMs: 701000,
		ExitCode:   0,
		Signal:     &sig,
	}, 4)

	data, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	for _, field := range []string{"v", "kind", "id", "started_at", "duration_ms", "exit_code", "signal", "pool", "parallelism", "cwd_mac", "argv_macs"} {
		if _, ok := got[field]; !ok {
			t.Errorf("run record lacks %q: %s", field, data)
		}
	}
	if got["kind"] != HistoryKindRun || got["pool"] != true || got["parallelism"] != float64(4) {
		t.Errorf("run record = %s, want kind run, pool true, parallelism 4", data)
	}
	if got["started_at"] != "2026-10-09T18:10:23Z" {
		t.Errorf("started_at = %v", got["started_at"])
	}
}

type poolOKTest struct {
	Name string
	Runs []PoolRunRecord
	Want bool
}

func exitPtr(n int) *int { return &n }

var poolOKTests = []poolOKTest{
	{
		Name: "all succeeded",
		Runs: []PoolRunRecord{{Status: PoolRunFinished, ExitCode: exitPtr(0)}, {Status: PoolRunFinished, ExitCode: exitPtr(0)}},
		Want: true,
	},
	{
		Name: "one failed",
		Runs: []PoolRunRecord{{Status: PoolRunFinished, ExitCode: exitPtr(0)}, {Status: PoolRunFinished, ExitCode: exitPtr(1)}},
		Want: false,
	},
	{
		Name: "one skipped",
		Runs: []PoolRunRecord{{Status: PoolRunFinished, ExitCode: exitPtr(0)}, {Status: PoolRunSkipped}},
		Want: false,
	},
	{
		Name: "start error",
		Runs: []PoolRunRecord{{Status: PoolRunStartError}},
		Want: false,
	},
}

func TestHistoryPoolRecordOK(t *testing.T) {
	h := newTestHistory(t)

	for _, test := range poolOKTests {
		t.Run(test.Name, func(t *testing.T) {
			now := time.Now().UTC()
			rec, ok := h.NewPoolRecord(&PoolManifest{ID: "P", Repeat: 1, Parallelism: 1, StartedAt: now.Add(-2 * time.Second), FinishedAt: &now, Runs: test.Runs})
			if !ok {
				t.Fatal("not ok for a finished pool")
			}
			if rec.OK != test.Want {
				t.Errorf("OK = %v, want %v", rec.OK, test.Want)
			}
			if rec.DurationMs != 2000 {
				t.Errorf("DurationMs = %d, want 2000", rec.DurationMs)
			}
		})
	}

	if _, ok := h.NewPoolRecord(&PoolManifest{ID: "P"}); ok {
		t.Error("NewPoolRecord accepted an unfinished pool")
	}
}

func TestHistoryKeyAgreesAcrossConcurrentFirstUse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cg")

	const n = 16

	keys := make([][]byte, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()

			h, err := openHistoryAt(dir)
			if err != nil {
				t.Errorf("openHistoryAt: %v", err)
				return
			}
			keys[i] = h.key
		}()
	}
	wg.Wait()

	for i := 1; i < n; i++ {
		if !bytes.Equal(keys[0], keys[i]) {
			t.Fatalf("opener %d got a different key", i)
		}
	}
}

func TestHistoryPermissions(t *testing.T) {
	h := newTestHistory(t)
	if err := h.Append(paddedRecord("x")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	info, err := os.Stat(h.Dir())
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode = %o, want 700", got)
	}

	for _, name := range []string{HistoryKeyFilename, HistoryLogFilename, HistoryLockFilename} {
		info, err := os.Stat(filepath.Join(h.Dir(), name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode = %o, want 600", name, got)
		}
	}
}

func TestHistoryRefusesFIFOLog(t *testing.T) {
	h := newTestHistory(t)

	if err := syscall.Mkfifo(filepath.Join(h.Dir(), HistoryLogFilename), 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- h.Append(paddedRecord("x")) }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Append wrote to a FIFO log")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Append hung on a FIFO log")
	}
}

func TestHistoryRefusesSymlinkedLog(t *testing.T) {
	h := newTestHistory(t)

	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Symlink(target, filepath.Join(h.Dir(), HistoryLogFilename)); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := h.Append(paddedRecord("x")); err == nil {
		t.Error("Append followed a symlinked log")
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("append created the symlink target")
	}
}

type historyDirTest struct {
	Name string
	XDG  string
	Want func(home string) string
}

var historyDirTests = []historyDirTest{
	{Name: "xdg set", XDG: "/var/state", Want: func(string) string { return "/var/state/cg" }},
	{Name: "xdg unset", XDG: "", Want: func(home string) string { return filepath.Join(home, ".local", "state", "cg") }},
	{Name: "xdg relative is ignored", XDG: "relative/state", Want: func(home string) string { return filepath.Join(home, ".local", "state", "cg") }},
}

func TestHistoryDir(t *testing.T) {
	for _, test := range historyDirTests {
		t.Run(test.Name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", test.XDG)

			got, err := HistoryDir()
			if err != nil {
				t.Fatalf("HistoryDir: %v", err)
			}
			if want := test.Want(home); got != want {
				t.Errorf("HistoryDir = %q, want %q", got, want)
			}
		})
	}
}

func TestSupervisedRunAppendsHistory(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	run, err := RunSupervised([]string{"echo", "hi"}, SuperviseOptions{})
	if err != nil {
		t.Fatalf("RunSupervised: %v", err)
	}
	waitDone(t, run, 5*time.Second)

	h, err := openHistoryAt(filepath.Join(state, "cg"))
	if err != nil {
		t.Fatalf("openHistoryAt: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(h.Dir(), HistoryLogFilename))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	var rec RunHistoryRecord
	if err := json.Unmarshal(bytes.TrimSpace(data), &rec); err != nil {
		t.Fatalf("decoding %q: %v", data, err)
	}

	cwd, _ := os.Getwd()
	if rec.ID != run.ID || rec.Pool || rec.Parallelism != 0 || rec.ExitCode != 0 {
		t.Errorf("record = %+v, want standalone run %s with exit 0", rec, run.ID)
	}
	if rec.CwdMAC != h.CwdMAC(cwd) {
		t.Error("cwd_mac does not match the run's cwd")
	}
	if !slices.Equal(rec.ArgvMACs, h.ArgvMACs([]string{"echo", "hi"})) {
		t.Error("argv_macs does not match the run's argv")
	}
}

func TestSupervisePoolAppendsHistory(t *testing.T) {
	_, dir := newPoolTestDir(t)
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	argvs := [][]string{{"echo", "one"}, {"echo", "two"}}
	spec := poolSpec(t, argvs...)
	spec.Parallelism = 2

	if _, err := runPool(t, dir, spec); err != nil {
		t.Fatalf("supervisePool: %v", err)
	}

	h, err := openHistoryAt(filepath.Join(state, "cg"))
	if err != nil {
		t.Fatalf("openHistoryAt: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(h.Dir(), HistoryLogFilename))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	var runs []RunHistoryRecord
	var pools []PoolHistoryRecord
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var head struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			t.Fatalf("decoding %q: %v", line, err)
		}

		switch head.Kind {
		case HistoryKindRun:
			var rec RunHistoryRecord
			if err := json.Unmarshal(line, &rec); err != nil {
				t.Fatalf("decoding run: %v", err)
			}
			runs = append(runs, rec)
		case HistoryKindPool:
			var rec PoolHistoryRecord
			if err := json.Unmarshal(line, &rec); err != nil {
				t.Fatalf("decoding pool: %v", err)
			}
			pools = append(pools, rec)
		}
	}

	if len(runs) != 2 || len(pools) != 1 {
		t.Fatalf("got %d run and %d pool records, want 2 and 1", len(runs), len(pools))
	}
	for _, r := range runs {
		if !r.Pool || r.Parallelism != 2 {
			t.Errorf("member record pool=%v parallelism=%d, want true and 2", r.Pool, r.Parallelism)
		}
	}

	m := readManifest(t, dir)
	if pools[0].ID != m.ID || !pools[0].OK || pools[0].Parallelism != 2 {
		t.Errorf("pool record = %+v, want id %s, ok, parallelism 2", pools[0], m.ID)
	}
	if want := h.PoolMAC(argvs, m.Repeat, m.Parallelism); pools[0].PoolMAC != want {
		t.Errorf("pool_mac = %q, want %q", pools[0].PoolMAC, want)
	}

	meta, err := ReadMeta(filepath.Join(CaptureRoot(), m.Runs[0].RunID))
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	raw, _ := json.Marshal(meta)
	if bytes.Contains(raw, []byte("parallelism")) {
		t.Errorf("meta.json gained parallelism: %s", raw)
	}
}
