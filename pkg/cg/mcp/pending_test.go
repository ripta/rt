package mcp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ripta/rt/pkg/cg/model"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

func newTestPendingStore() (*pendingStore, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	s := newPendingStore()
	s.now = clock.Now

	return s, clock
}

func baseEntry() pendingEntry {
	return pendingEntry{
		Tool:  "cg_run_many",
		Argvs: [][]string{{"git", "status"}, {"go", "test", "./..."}},
		Cwd:   "/work",
		Env:   map[string]string{"A": "1"},
	}
}

type pendingTakeTest struct {
	Name    string
	Tool    string
	Argvs   [][]string
	Cwd     string
	Env     map[string]string
	Advance time.Duration
	Nonce   string
	WantErr bool
}

var pendingTakeTests = []pendingTakeTest{
	{
		Name:  "exact match",
		Tool:  "cg_run_many",
		Argvs: [][]string{{"git", "status"}, {"go", "test", "./..."}},
		Cwd:   "/work",
		Env:   map[string]string{"A": "1"},
	},
	{
		Name:    "tool differs",
		Tool:    "cg_run",
		Argvs:   [][]string{{"git", "status"}, {"go", "test", "./..."}},
		Cwd:     "/work",
		Env:     map[string]string{"A": "1"},
		WantErr: true,
	},
	{
		Name:    "argv token differs",
		Tool:    "cg_run_many",
		Argvs:   [][]string{{"git", "status"}, {"go", "test", "./pkg"}},
		Cwd:     "/work",
		Env:     map[string]string{"A": "1"},
		WantErr: true,
	},
	{
		Name:    "argv order differs",
		Tool:    "cg_run_many",
		Argvs:   [][]string{{"go", "test", "./..."}, {"git", "status"}},
		Cwd:     "/work",
		Env:     map[string]string{"A": "1"},
		WantErr: true,
	},
	{
		Name:    "argv extra command",
		Tool:    "cg_run_many",
		Argvs:   [][]string{{"git", "status"}, {"go", "test", "./..."}, {"rm", "-rf", "/"}},
		Cwd:     "/work",
		Env:     map[string]string{"A": "1"},
		WantErr: true,
	},
	{
		Name:    "argv missing command",
		Tool:    "cg_run_many",
		Argvs:   [][]string{{"git", "status"}},
		Cwd:     "/work",
		Env:     map[string]string{"A": "1"},
		WantErr: true,
	},
	{
		Name:    "cwd differs",
		Tool:    "cg_run_many",
		Argvs:   [][]string{{"git", "status"}, {"go", "test", "./..."}},
		Cwd:     "/elsewhere",
		Env:     map[string]string{"A": "1"},
		WantErr: true,
	},
	{
		Name:    "env value differs",
		Tool:    "cg_run_many",
		Argvs:   [][]string{{"git", "status"}, {"go", "test", "./..."}},
		Cwd:     "/work",
		Env:     map[string]string{"A": "2"},
		WantErr: true,
	},
	{
		Name:    "env key added",
		Tool:    "cg_run_many",
		Argvs:   [][]string{{"git", "status"}, {"go", "test", "./..."}},
		Cwd:     "/work",
		Env:     map[string]string{"A": "1", "B": "2"},
		WantErr: true,
	},
	{
		Name:    "env dropped",
		Tool:    "cg_run_many",
		Argvs:   [][]string{{"git", "status"}, {"go", "test", "./..."}},
		Cwd:     "/work",
		WantErr: true,
	},
	{
		Name:    "expired",
		Tool:    "cg_run_many",
		Argvs:   [][]string{{"git", "status"}, {"go", "test", "./..."}},
		Cwd:     "/work",
		Env:     map[string]string{"A": "1"},
		Advance: pendingTTL,
		WantErr: true,
	},
	{
		Name:    "just before expiry",
		Tool:    "cg_run_many",
		Argvs:   [][]string{{"git", "status"}, {"go", "test", "./..."}},
		Cwd:     "/work",
		Env:     map[string]string{"A": "1"},
		Advance: pendingTTL - time.Nanosecond,
	},
	{
		Name:    "unknown nonce",
		Tool:    "cg_run_many",
		Argvs:   [][]string{{"git", "status"}, {"go", "test", "./..."}},
		Cwd:     "/work",
		Env:     map[string]string{"A": "1"},
		Nonce:   "not-a-real-nonce",
		WantErr: true,
	},
}

func TestPendingTake(t *testing.T) {
	for _, test := range pendingTakeTests {
		t.Run(test.Name, func(t *testing.T) {
			s, clock := newTestPendingStore()

			nonce, err := s.add(baseEntry())
			if err != nil {
				t.Fatalf("add: %v", err)
			}

			if test.Nonce != "" {
				nonce = test.Nonce
			}

			clock.Advance(test.Advance)

			got, err := s.take(nonce, test.Tool, test.Argvs, test.Cwd, test.Env)
			if test.WantErr {
				if !errors.Is(err, ErrPendingApproval) {
					t.Fatalf("take err = %v, want ErrPendingApproval", err)
				}

				if got != nil {
					t.Errorf("take returned an entry alongside an error")
				}

				return
			}

			if err != nil {
				t.Fatalf("take: %v", err)
			}

			if got.Tool != "cg_run_many" || got.Cwd != "/work" {
				t.Errorf("take returned %+v", got)
			}
		})
	}
}

func TestPendingTakeErrorSaysCallAgain(t *testing.T) {
	s, _ := newTestPendingStore()

	_, err := s.take("missing", "cg_run", nil, "", nil)
	if err == nil {
		t.Fatal("take of a missing nonce succeeded")
	}

	if !errors.Is(err, ErrPendingApproval) {
		t.Fatalf("err = %v, want ErrPendingApproval", err)
	}

	if want := "call the tool again"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to contain %q", err, want)
	}
}

func TestPendingNilAndEmptyEnvMatch(t *testing.T) {
	for _, name := range []string{"nil stored, empty presented", "empty stored, nil presented"} {
		t.Run(name, func(t *testing.T) {
			s, _ := newTestPendingStore()

			e := baseEntry()
			e.Env = nil
			presented := map[string]string{}
			if name == "empty stored, nil presented" {
				e.Env = map[string]string{}
				presented = nil
			}

			nonce, err := s.add(e)
			if err != nil {
				t.Fatalf("add: %v", err)
			}

			if _, err := s.take(nonce, e.Tool, e.Argvs, e.Cwd, presented); err != nil {
				t.Errorf("take: %v", err)
			}
		})
	}
}

type pendingReplayTest struct {
	Name string
	Args func(e pendingEntry) (string, [][]string, string, map[string]string)
}

var pendingReplayTests = []pendingReplayTest{
	{
		Name: "replay after a match",
		Args: func(e pendingEntry) (string, [][]string, string, map[string]string) {
			return e.Tool, e.Argvs, e.Cwd, e.Env
		},
	},
	{
		Name: "replay after a mismatch",
		Args: func(e pendingEntry) (string, [][]string, string, map[string]string) {
			return "cg_run", e.Argvs, e.Cwd, e.Env
		},
	},
}

func TestPendingReplayRefused(t *testing.T) {
	for _, test := range pendingReplayTests {
		t.Run(test.Name, func(t *testing.T) {
			s, _ := newTestPendingStore()
			e := baseEntry()

			nonce, err := s.add(e)
			if err != nil {
				t.Fatalf("add: %v", err)
			}

			tool, argvs, cwd, env := test.Args(e)
			_, _ = s.take(nonce, tool, argvs, cwd, env)

			if _, err := s.take(nonce, e.Tool, e.Argvs, e.Cwd, e.Env); !errors.Is(err, ErrPendingApproval) {
				t.Errorf("second take err = %v, want ErrPendingApproval", err)
			}

			if len(s.entries) != 0 || len(s.order) != 0 {
				t.Errorf("store not empty: %d entries, %d ordered", len(s.entries), len(s.order))
			}
		})
	}
}

func richEntry() pendingEntry {
	e := baseEntry()
	e.Commands = []pendingCommand{
		{
			Resolution: &model.Resolution{Argv: []string{"git", "status"}, Resolved: "/usr/bin/git", Canonical: "/usr/bin/git"},
			Suggestion: []string{"/usr/bin/git", "status"},
			Prompted:   true,
		},
		{
			Resolution: &model.Resolution{Argv: []string{"go", "test", "./..."}, Resolved: "/usr/bin/go"},
			Suggestion: []string{"/usr/bin/go", "test"},
		},
	}
	e.DivergenceSnapshot = []byte("rules: []\n")
	e.Diverged = true

	return e
}

func TestPendingStoredCopyIsIsolated(t *testing.T) {
	s, _ := newTestPendingStore()
	e := richEntry()

	nonce, err := s.add(e)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	e.Argvs[0][0] = "rm"
	e.Env["A"] = "changed"
	e.Commands[0].Suggestion[0] = "rm"
	e.Commands[0].Resolution.Argv[0] = "rm"
	e.Commands[0].Resolution.Resolved = "/bin/rm"
	e.Commands[1] = pendingCommand{}
	e.DivergenceSnapshot[0] = 'X'

	want := richEntry()
	got, err := s.take(nonce, want.Tool, want.Argvs, want.Cwd, want.Env)
	if err != nil {
		t.Fatalf("take after caller mutated its copy: %v", err)
	}

	if !reflect.DeepEqual(got.Commands, want.Commands) {
		t.Errorf("Commands = %+v, want %+v", got.Commands, want.Commands)
	}

	if !bytes.Equal(got.DivergenceSnapshot, want.DivergenceSnapshot) {
		t.Errorf("DivergenceSnapshot = %q, want %q", got.DivergenceSnapshot, want.DivergenceSnapshot)
	}
}

func TestPendingRoundTrip(t *testing.T) {
	s, _ := newTestPendingStore()
	want := richEntry()

	nonce, err := s.add(want)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err := s.take(nonce, want.Tool, want.Argvs, want.Cwd, want.Env)
	if err != nil {
		t.Fatalf("take: %v", err)
	}

	if !reflect.DeepEqual(got.Commands, want.Commands) {
		t.Errorf("Commands = %+v, want %+v", got.Commands, want.Commands)
	}

	if !bytes.Equal(got.DivergenceSnapshot, want.DivergenceSnapshot) {
		t.Errorf("DivergenceSnapshot = %q, want %q", got.DivergenceSnapshot, want.DivergenceSnapshot)
	}

	if got.Diverged != want.Diverged {
		t.Errorf("Diverged = %v, want %v", got.Diverged, want.Diverged)
	}

	if !reflect.DeepEqual(got.Argvs, want.Argvs) || got.Cwd != want.Cwd || !maps.Equal(got.Env, want.Env) {
		t.Errorf("call fields = %+v, want %+v", got, want)
	}
}

type pendingEvictionTest struct {
	Name        string
	Adds        int
	WantMissing []int
}

var pendingEvictionTests = []pendingEvictionTest{
	{Name: "at the cap", Adds: maxPending},
	{Name: "one past the cap", Adds: maxPending + 1, WantMissing: []int{0}},
	{Name: "three past the cap", Adds: maxPending + 3, WantMissing: []int{0, 1, 2}},
}

func TestPendingEvictsOldest(t *testing.T) {
	for _, test := range pendingEvictionTests {
		t.Run(test.Name, func(t *testing.T) {
			s, _ := newTestPendingStore()

			nonces := make([]string, test.Adds)
			for i := range nonces {
				e := baseEntry()
				e.Cwd = fmt.Sprintf("/work/%d", i)

				nonce, err := s.add(e)
				if err != nil {
					t.Fatalf("add %d: %v", i, err)
				}

				nonces[i] = nonce
			}

			if len(s.entries) > maxPending {
				t.Fatalf("store holds %d entries, cap is %d", len(s.entries), maxPending)
			}

			missing := make(map[int]bool)
			for _, i := range test.WantMissing {
				missing[i] = true
			}

			for i, nonce := range nonces {
				e := baseEntry()
				_, err := s.take(nonce, e.Tool, e.Argvs, fmt.Sprintf("/work/%d", i), e.Env)
				if missing[i] && !errors.Is(err, ErrPendingApproval) {
					t.Errorf("entry %d survived eviction: err = %v", i, err)
				}

				if !missing[i] && err != nil {
					t.Errorf("entry %d was evicted: %v", i, err)
				}
			}
		})
	}
}

func TestPendingSweepsExpiredOnAdd(t *testing.T) {
	s, clock := newTestPendingStore()

	for i := 0; i < 5; i++ {
		if _, err := s.add(baseEntry()); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	clock.Advance(pendingTTL)

	if _, err := s.add(baseEntry()); err != nil {
		t.Fatalf("add: %v", err)
	}

	if len(s.entries) != 1 || len(s.order) != 1 {
		t.Errorf("after sweep: %d entries, %d ordered; want 1 each", len(s.entries), len(s.order))
	}
}

func TestPendingSweepBeforeEviction(t *testing.T) {
	s, clock := newTestPendingStore()

	var first string
	for i := 0; i < maxPending; i++ {
		nonce, err := s.add(baseEntry())
		if err != nil {
			t.Fatalf("add: %v", err)
		}

		if i == 0 {
			first = nonce
		}

		clock.Advance(time.Minute)
	}

	// Entry i was added at minute i and expires at minute i+30. The clock reads minute 32, so
	// entries 0 to 2 are expired. Entries 3 onward are live, so adding must not evict any.
	if _, err := s.add(baseEntry()); err != nil {
		t.Fatalf("add: %v", err)
	}

	e := baseEntry()
	if _, err := s.take(first, e.Tool, e.Argvs, e.Cwd, e.Env); !errors.Is(err, ErrPendingApproval) {
		t.Errorf("expired entry redeemed: %v", err)
	}

	if want := maxPending - 3 + 1; len(s.entries) != want {
		t.Errorf("store holds %d entries, want %d", len(s.entries), want)
	}
}

func TestPendingNonces(t *testing.T) {
	s, _ := newTestPendingStore()

	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		nonce, err := s.add(baseEntry())
		if err != nil {
			t.Fatalf("add: %v", err)
		}

		if seen[nonce] {
			t.Fatalf("nonce %q repeated", nonce)
		}

		seen[nonce] = true

		raw, err := base64.RawURLEncoding.DecodeString(nonce)
		if err != nil {
			t.Fatalf("nonce %q is not base64url: %v", nonce, err)
		}

		if len(raw) < 16 {
			t.Fatalf("nonce %q carries %d bytes, want at least 16", nonce, len(raw))
		}
	}
}

func TestPendingConcurrentUse(t *testing.T) {
	s, _ := newTestPendingStore()
	e := baseEntry()

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			nonce, err := s.add(e)
			if err != nil {
				t.Errorf("add: %v", err)
				return
			}

			_, _ = s.take(nonce, e.Tool, e.Argvs, e.Cwd, e.Env)
		}()
	}

	wg.Wait()
}
