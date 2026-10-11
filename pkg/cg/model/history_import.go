package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// HistoryImportedFilename is the marker next to the log that records the first-query import.
const HistoryImportedFilename = "history.imported"

type importRecord struct {
	startedAt time.Time
	record    any
}

// EnsureImported seeds the log from the finished runs and pools still in the capture root.
//
// Runs append to the log from their first release, so a missing log cannot trigger the import.
// A marker next to the log does. It keeps the import on the query side, off the finish path of a
// run, and later calls return at once.
//
// A run or pool whose ID is already in the log is skipped, so a repeat is safe.
//
// A pooled member takes its parallelism from its pool's manifest.
//
// A member whose pool directory is gone is skipped. Its parallelism is unknown, so its duration
// is comparable to no entry.
func (h *History) EnsureImported() error {
	if h.imported() {
		return nil
	}

	unlock, err := h.lock()
	if err != nil {
		return err
	}
	defer unlock()

	if h.imported() {
		return nil
	}

	known := make(map[string]bool)
	err = h.scanLogLocked(func(header historyHeader, _ []byte) {
		known[header.ID] = true
	})
	if err != nil {
		return err
	}

	records, err := h.captureRecords(known)
	if err != nil {
		return err
	}

	for _, r := range records {
		line, err := json.Marshal(r.record)
		if err != nil {
			return fmt.Errorf("encoding history record: %w", err)
		}

		if err := h.appendLocked(append(line, '\n')); err != nil {
			return err
		}
	}

	marker, err := openHistoryFile(filepath.Join(h.dir, HistoryImportedFilename), os.O_WRONLY|os.O_CREATE)
	if err != nil {
		return fmt.Errorf("writing history import marker: %w", err)
	}

	return marker.Close()
}

func (h *History) imported() bool {
	_, err := os.Lstat(filepath.Join(h.dir, HistoryImportedFilename))

	return err == nil
}

// captureRecords builds history records for every finished run and pool under the capture root
// that is not already known, oldest first.
func (h *History) captureRecords(known map[string]bool) ([]importRecord, error) {
	entries, err := ReadCaptureRoot()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading capture root: %w", err)
	}

	root := CaptureRoot()
	manifests := make(map[string]*PoolManifest)

	// A member's pool directory may be listed after the member, so manifests load on demand.
	manifest := func(id string) *PoolManifest {
		if m, ok := manifests[id]; ok {
			return m
		}

		var m *PoolManifest
		if IsValidRunID(id) {
			m, _ = ReadPoolManifest(filepath.Join(root, id))
		}
		manifests[id] = m

		return m
	}

	var records []importRecord
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !IsValidRunID(name) || known[name] {
			continue
		}

		dir := filepath.Join(root, name)

		if m, err := ReadMeta(dir); err == nil {
			if m.ID != name || m.Cwd == "" {
				continue
			}

			parallelism := 0
			if m.Pool != "" {
				pool := manifest(m.Pool)
				if pool == nil {
					continue
				}
				parallelism = pool.Parallelism
			}

			records = append(records, importRecord{m.StartedAt, h.NewRunRecord(m, parallelism)})

			continue
		}

		if p, err := ReadPoolManifest(dir); err == nil {
			if p.ID != name || p.Cwd == "" {
				continue
			}

			if record, ok := h.NewPoolRecord(p); ok {
				records = append(records, importRecord{p.StartedAt, record})
			}
		}
	}

	sort.SliceStable(records, func(a, b int) bool { return records[a].startedAt.Before(records[b].startedAt) })

	return records, nil
}
