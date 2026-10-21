package mcp

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/ripta/rt/pkg/cg/model"
)

const (
	// pendingTTL is how long a pending approval stays redeemable. A dialog left open this long is
	// almost certainly abandoned.
	pendingTTL = 30 * time.Minute

	// maxPending bounds memory when a client abandons many calls in a row.
	maxPending = 32

	// nonceBytes is the entropy of a nonce. The nonce is an opaque lookup key, so it carries no
	// information about the command it guards.
	nonceBytes = 16
)

// ErrPendingApproval is wrapped by every refusal from the pending store. Its text tells the
// caller what to do next, because the retry is the only place the user sees it.
var ErrPendingApproval = fmt.Errorf("approval is no longer pending; call the tool again to be prompted afresh")

// pendingCommand is what the gate worked out for one command in a pending call. The retry
// reuses it instead of resolving and suggesting again, so it applies the same rule the user was
// shown.
type pendingCommand struct {
	Resolution *model.Resolution
	Suggestion []string

	// Prompted reports whether the first round asked the user about this command. The retry
	// refuses a command that now needs a prompt but was not asked about, because there is no
	// answer to read for it.
	Prompted bool

	// Form is the id of the approval form that covered this command, empty when it was not
	// prompted. Several commands share a form, and the retry reads their answer through it.
	Form string
}

// pendingEntry records a call that is waiting on the user. Tool, Argvs, Cwd, Env, and Args are
// what the retry must repeat exactly. The remaining fields carry what the first round showed the
// user.
type pendingEntry struct {
	Tool  string
	Argvs [][]string
	Cwd   string
	Env   map[string]string

	// Args is the canonical JSON of the whole tool input. Argvs holds only the distinct commands
	// the gate asked about, so without Args a retry could keep the answer and change what runs
	// around it, such as a pool's repeat count or duplicate commands.
	Args []byte

	// Commands runs parallel to Argvs.
	Commands []pendingCommand

	// DivergenceSnapshot is the project rules file content on disk when the first round was
	// built, and nil when the file was absent. The retry compares it with the file as it is
	// then, so a change in between skips the remember whether or not the user was shown a diff.
	DivergenceSnapshot []byte

	// Diverged reports whether that content differed from what cg loaded, so the first round
	// carried the trailing divergence form.
	Diverged bool

	// Unreadable reports that the file could not be read when the first round was built, so
	// DivergenceSnapshot says nothing. The retry skips the remember.
	Unreadable bool

	expires time.Time
}

// pendingStore holds approvals that are waiting on the user, keyed by nonce. It lives in memory
// only. A restart drops every entry, which costs one repeated prompt and keeps a stale approval
// from running after the server comes back.
type pendingStore struct {
	mu      sync.Mutex
	entries map[string]*pendingEntry
	order   []string

	// now is a seam for tests. Nil means time.Now.
	now func() time.Time
}

func newPendingStore() *pendingStore {
	return &pendingStore{entries: make(map[string]*pendingEntry)}
}

func (s *pendingStore) clock() time.Time {
	if s.now != nil {
		return s.now()
	}

	return time.Now()
}

// add stores a copy of the entry and returns the nonce that redeems it. Expired entries are swept
// first. When the store is still full, the oldest entries are evicted to make room.
func (s *pendingStore) add(e pendingEntry) (string, error) {
	nonce, err := newNonce()
	if err != nil {
		return "", err
	}

	stored := e
	stored.Argvs = cloneArgvs(e.Argvs)
	stored.Env = maps.Clone(e.Env)
	stored.Args = slices.Clone(e.Args)
	stored.Commands = cloneCommands(e.Commands)
	stored.DivergenceSnapshot = slices.Clone(e.DivergenceSnapshot)
	stored.expires = s.clock().Add(pendingTTL)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sweepLocked()
	for len(s.order) >= maxPending {
		delete(s.entries, s.order[0])
		s.order = s.order[1:]
	}

	s.entries[nonce] = &stored
	s.order = append(s.order, nonce)

	return nonce, nil
}

// take redeems a nonce. The entry is deleted on lookup whether or not the call matches, so a
// nonce can be tried once and a wrong guess cannot be retried with corrected arguments.
func (s *pendingStore) take(nonce, tool string, argvs [][]string, cwd string, env map[string]string, args []byte) (*pendingEntry, error) {
	s.mu.Lock()
	e, ok := s.entries[nonce]
	if ok {
		delete(s.entries, nonce)
		s.order = slices.DeleteFunc(s.order, func(n string) bool { return n == nonce })
	}
	s.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("%w: no pending approval found", ErrPendingApproval)
	}

	if !s.clock().Before(e.expires) {
		return nil, fmt.Errorf("%w: the approval expired", ErrPendingApproval)
	}

	// A nil map and an empty map both mean no overrides, and a JSON round trip can turn one into
	// the other. maps.Equal treats them as equal.
	if e.Tool != tool || e.Cwd != cwd || !maps.Equal(e.Env, env) || !slices.EqualFunc(e.Argvs, argvs, slices.Equal) || !bytes.Equal(e.Args, args) {
		return nil, fmt.Errorf("%w: the call differs from the one that was approved", ErrPendingApproval)
	}

	return e, nil
}

func (s *pendingStore) sweepLocked() {
	now := s.clock()
	kept := s.order[:0]
	for _, nonce := range s.order {
		if now.Before(s.entries[nonce].expires) {
			kept = append(kept, nonce)
			continue
		}

		delete(s.entries, nonce)
	}

	s.order = kept
}

func newNonce() (string, error) {
	b := make([]byte, nonceBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("minting approval nonce: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

func cloneCommands(cmds []pendingCommand) []pendingCommand {
	if cmds == nil {
		return nil
	}

	out := make([]pendingCommand, len(cmds))
	for i, c := range cmds {
		out[i].Prompted = c.Prompted
		out[i].Form = c.Form
		out[i].Suggestion = slices.Clone(c.Suggestion)

		if c.Resolution != nil {
			r := *c.Resolution
			r.Argv = slices.Clone(c.Resolution.Argv)
			out[i].Resolution = &r
		}
	}

	return out
}

func cloneArgvs(argvs [][]string) [][]string {
	if argvs == nil {
		return nil
	}

	out := make([][]string, len(argvs))
	for i, argv := range argvs {
		out[i] = slices.Clone(argv)
	}

	return out
}
