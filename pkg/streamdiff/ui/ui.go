package ui

import (
	"fmt"
	"github.com/containerd/console"
	"github.com/gosuri/uilive"
	"io"
	"sort"
	"sync"
	"time"
)

type Handle struct {
	w   io.Writer
	lw  *uilive.Writer
	mut *sync.RWMutex

	done chan struct{}

	width int
	lines map[string]string
	order []string
}

func New(w io.Writer) (*Handle, error) {
	cur := console.Current()
	ws, err := cur.Size()
	if err != nil {
		return nil, err
	}

	lw := uilive.New()
	lw.Out = w

	return &Handle{
		w:   w,
		lw:  lw,
		mut: &sync.RWMutex{},

		done: make(chan struct{}),

		width: int(ws.Width),
		lines: map[string]string{},
		order: []string{},
	}, nil
}

func (h *Handle) Get(key string) string {
	h.mut.RLock()
	defer h.mut.RUnlock()
	return h.lines[key]
}

func (h *Handle) Set(key, line string) {
	if x := len(line); h.width > 40 && x > h.width-40 {
		line = line[:h.width-40] + "... " + fmt.Sprintf("(%d bytes)", x)
	}

	h.mut.Lock()
	defer h.mut.Unlock()

	if _, ok := h.lines[key]; !ok {
		h.order = append(h.order, key)
		sort.Strings(h.order)
	}
	h.lines[key] = line
}

func (h *Handle) Setf(key, format string, a ...interface{}) {
	h.Set(key, fmt.Sprintf(format, a...))
}

func (h *Handle) Stop() {
	h.done <- struct{}{}
	<-h.done
}

func (h *Handle) UpdateEvery(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			h.paint()
		case <-h.done:
			h.paint()
			close(h.done)
			return
		}
	}
}

func (h *Handle) paint() {
	h.mut.Lock()
	defer h.mut.Unlock()

	for _, key := range h.order {
		fmt.Fprintln(h.lw, h.lines[key])
	}
	h.lw.Flush()
}
