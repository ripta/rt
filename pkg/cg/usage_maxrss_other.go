//go:build unix && !linux

package cg

// maxrssBytes normalizes ru_maxrss to bytes. Darwin and the BSDs already report
// it in bytes, so the value passes through unchanged.
func maxrssBytes(v int64) int64 {
	return v
}
