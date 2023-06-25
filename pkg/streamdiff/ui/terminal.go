package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/r3labs/diff/v3"
)

func NewTerminalView(w io.Writer) Viewer {
	return &terminalView{
		hist:  map[string]struct{}{},
		start: time.Now(),
		w:     w,
	}
}

type terminalView struct {
	hist  map[string]struct{}
	start time.Time
	w     io.Writer
}

func (view *terminalView) Update(key string, changes diff.Changelog) {
	if _, seen := view.hist[key]; !seen {
		view.hist[key] = struct{}{}
		fmt.Fprintf(view.w, "T+%s %s (NEW)\n", time.Since(view.start).Truncate(time.Second), key)
		return
	}
	if len(changes) == 0 {
		return
	}

	fmt.Fprintf(view.w, "T+%s %s\n", time.Since(view.start).Truncate(time.Second), key)
	for i, change := range changes {
		path := strings.Join(change.Path, ".")
		switch change.Type {
		case diff.CREATE:
			fmt.Fprintf(view.w, "  (%d/%d): %s \\ -> %v\n", i+1, len(changes), path, change.To)
		case diff.DELETE:
			fmt.Fprintf(view.w, "  (%d/%d): %s %v -> \\\n", i+1, len(changes), path, change.From)
		case diff.UPDATE:
			fmt.Fprintf(view.w, "  (%d/%d): %s %v -> %v\n", i+1, len(changes), path, change.From, change.To)
		default:
			fmt.Fprintf(view.w, "  (%d/%d): %+v\n", i+1, len(changes), change)
		}
	}
	fmt.Fprint(view.w, "\n")
}
