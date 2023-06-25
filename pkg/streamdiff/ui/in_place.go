package ui

import (
	"io"
	"strings"
	"time"

	"github.com/r3labs/diff/v3"
)

func NewInPlaceView(w io.Writer, ts *ThrobberSet, refreshEvery time.Duration) (Viewer, ShutdownFunc, error) {
	hnd, err := New(w)
	if err != nil {
		return nil, nil, err
	}

	view := &inPlaceView{
		hnd:  hnd,
		hist: map[string]struct{}{},
		ts:   ts,
	}

	go hnd.UpdateEvery(refreshEvery)
	return view, hnd.Stop, nil
}

type inPlaceView struct {
	hnd  *Handle
	hist map[string]struct{}
	ts   *ThrobberSet
}

func (view *inPlaceView) Update(key string, changes diff.Changelog) {
	if _, seen := view.hist[key]; !seen {
		view.hnd.Setf(key, "%s %s\t(new)", view.ts.Next(key), key)
		return
	}

	if len(changes) > 0 {
		change := changes[0]
		view.hnd.Setf(key, "%s %s\t%s: %+v -> %+v", view.ts.Next(key), key, strings.Join(change.Path, "."), change.From, change.To)
		return
	}

	if msg := view.hnd.Get(key); msg != "" {
		view.hnd.Set(key, view.ts.Next(key)+msg[1:])
	}
}
