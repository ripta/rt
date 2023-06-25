package ui

import "github.com/r3labs/diff/v3"

type ShutdownFunc func()

type Viewer interface {
	Update(string, diff.Changelog)
}
