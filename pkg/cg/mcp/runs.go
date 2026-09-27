package mcp

import "sync"

// Run state strings shared by the cg_meta, cg_list, and cg_wait outputs.
const (
	stateRunning   = "running"
	stateFinished  = "finished"
	stateFailed    = "failed"
	stateAbandoned = "abandoned"
	stateUnknown   = "unknown"
)

// runRegistry tracks the capture runs and pools that this MCP server process
// started. cg_wait consults it for a fast path out of filesystem polling; runs
// started by the shell capture path or by an earlier server process are not in
// the registry and fall back to polling.
//
// cg_cancel signals only pids held here. The pid comes from the supervisor's
// status pipe, so no file under the capture root can redirect a signal.
//
// Entries are added by handleRun and handleRunMany once the supervisor acks,
// and removed by a janitor goroutine when the Done channel closes, so the map
// size tracks the in-flight set.
type runRegistry struct {
	mu      sync.Mutex
	entries map[string]registryEntry
}

// registryEntry is one in-flight run or pool. For a run, pid is the child's
// process-group ID; for a pool, it is the supervisor's pid.
type registryEntry struct {
	done <-chan struct{}
	pid  int
	pool bool
}

func newRunRegistry() *runRegistry {
	return &runRegistry{entries: make(map[string]registryEntry)}
}

// Add registers a run or pool under id and spawns a goroutine that removes the
// entry once done closes. Calling Add with an id that already exists
// overwrites the previous entry; the previous janitor still cleans up only its
// own entry, so the new entry survives.
func (r *runRegistry) Add(id string, done <-chan struct{}, pid int, pool bool) {
	r.mu.Lock()
	r.entries[id] = registryEntry{done: done, pid: pid, pool: pool}
	r.mu.Unlock()

	go func() {
		<-done
		r.mu.Lock()
		if cur, ok := r.entries[id]; ok && cur.done == done {
			delete(r.entries, id)
		}
		r.mu.Unlock()
	}()
}

// Done returns the Done channel for id, or (nil, false) if id is not tracked.
func (r *runRegistry) Done(id string) (<-chan struct{}, bool) {
	e, ok := r.lookup(id)
	return e.done, ok
}

func (r *runRegistry) lookup(id string) (registryEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	return e, ok
}
