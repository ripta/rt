//go:build !darwin && !linux

package model

func readProcess(int) (processInfo, error) {
	return processInfo{}, errProcessLookupUnsupported
}
