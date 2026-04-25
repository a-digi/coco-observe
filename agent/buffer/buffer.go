// Package buffer stores unsent metric batches as JSON files on disk.
// On reconnect the agent replays buffered files in chronological order
// before resuming live pushes.
package buffer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/a-digi/coco-observe/payload"
)

// Buffer manages the local on-disk batch store.
type Buffer struct {
	dir       string
	retention time.Duration
}

// New creates a Buffer rooted at dir with the given retention period.
// The directory is created if it does not exist.
func New(dir string, retention time.Duration) (*Buffer, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("buffer: mkdir %s: %w", dir, err)
	}
	return &Buffer{dir: dir, retention: retention}, nil
}

// Write persists a batch to disk. Called when a push fails.
func (b *Buffer) Write(batch *payload.Batch) error {
	fname := fmt.Sprintf("batch-%d-%d.json", batch.Timestamp, batch.Sequence)
	path := filepath.Join(b.dir, fname)
	data, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("buffer write: marshal: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// Pending returns all buffered batches sorted by filename (chronological).
// Expired files (older than retention) are deleted and excluded.
func (b *Buffer) Pending() ([]*payload.Batch, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return nil, fmt.Errorf("buffer list: %w", err)
	}

	var names []string
	cutoff := time.Now().Add(-b.retention)

	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "batch-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(b.dir, e.Name()))
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var batches []*payload.Batch
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(b.dir, name))
		if err != nil {
			continue
		}
		var batch payload.Batch
		if err := json.Unmarshal(data, &batch); err != nil {
			continue
		}
		batches = append(batches, &batch)
	}
	return batches, nil
}

// Delete removes the on-disk file for a successfully replayed batch.
func (b *Buffer) Delete(batch *payload.Batch) {
	fname := fmt.Sprintf("batch-%d-%d.json", batch.Timestamp, batch.Sequence)
	_ = os.Remove(filepath.Join(b.dir, fname))
}
