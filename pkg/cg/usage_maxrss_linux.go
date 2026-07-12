//go:build linux

package cg

// maxrssBytes normalizes ru_maxrss to bytes. Linux reports it in kibibytes.
func maxrssBytes(v int64) int64 {
	return v * 1024
}
