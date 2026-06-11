package mcp

import "sync"

// Run state strings shared by the cg_meta, cg_list, and cg_wait outputs.
const (
	stateRunning  = "running"
	stateFinished = "finished"
	stateFailed   = "failed"
)

// runRegistry tracks the Done channels of capture runs that this MCP server
// process started. cg_wait (and later cg_cancel) consult it for a fast path
// out of filesystem polling; runs started by the shell capture path or by an
// earlier server process are not in the registry and fall back to polling.
//
// Entries are added by handleRun once the child is started and removed by a
// janitor goroutine when the Done channel closes, so the map size tracks the
// in-flight set.
type runRegistry struct {
	mu   sync.Mutex
	done map[string]<-chan struct{}
}

func newRunRegistry() *runRegistry {
	return &runRegistry{done: make(map[string]<-chan struct{})}
}

// Add registers done under id and spawns a goroutine that removes the entry
// once done closes. Calling Add with an id that already exists overwrites the
// previous channel; the previous janitor still cleans up its own entry, so the
// new entry survives.
func (r *runRegistry) Add(id string, done <-chan struct{}) {
	r.mu.Lock()
	r.done[id] = done
	r.mu.Unlock()

	go func() {
		<-done
		r.mu.Lock()
		if cur, ok := r.done[id]; ok && cur == done {
			delete(r.done, id)
		}
		r.mu.Unlock()
	}()
}

// Done returns the Done channel for id, or (nil, false) if id is not tracked.
func (r *runRegistry) Done(id string) (<-chan struct{}, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	done, ok := r.done[id]
	return done, ok
}
