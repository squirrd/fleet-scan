package output

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Writer writes JSONL records and meta.json to a timestamped run directory.
// WriteRecord is guarded by a mutex for concurrent write safety.
type Writer struct {
	mu     sync.Mutex
	runDir string
	meta   RunMeta
	file   *os.File
}

// NewWriter creates a timestamped run directory under baseDir, writes an initial
// meta.json with the provided metadata, and opens results.jsonl for writing.
func NewWriter(baseDir string, meta RunMeta) (*Writer, error) {
	now := time.Now().UTC()
	dirName := now.Format("2006-01-02T150405")
	runDir := filepath.Join(baseDir, dirName)

	if err := os.MkdirAll(runDir, 0755); err != nil {
		return nil, fmt.Errorf("creating run directory: %w", err)
	}

	// Set started_at if not already set.
	if meta.StartedAt.IsZero() {
		meta.StartedAt = now
	}
	if meta.RunID == "" {
		meta.RunID = dirName
	}

	w := &Writer{
		runDir: runDir,
		meta:   meta,
	}

	// Write initial meta.json.
	if err := w.writeMeta(); err != nil {
		return nil, fmt.Errorf("writing initial meta.json: %w", err)
	}

	// Open results.jsonl for appending.
	jsonlPath := filepath.Join(runDir, "results.jsonl")
	f, err := os.OpenFile(jsonlPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening results.jsonl: %w", err)
	}
	w.file = f

	return w, nil
}

// RunDir returns the path to the run directory.
func (w *Writer) RunDir() string {
	return w.runDir
}

// WriteRecord marshals a ClusterRecord to JSON and writes it as a single line
// to results.jsonl. Uses direct os.File.Write for flush-per-line semantics.
// Safe for concurrent use — guarded by a mutex.
func (w *Writer) WriteRecord(rec ClusterRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshalling record: %w", err)
	}
	data = append(data, '\n')

	if _, err := w.file.Write(data); err != nil {
		return fmt.Errorf("writing record: %w", err)
	}

	return nil
}

// Finalize overwrites meta.json with the final status and counts, then closes
// the results file.
func (w *Writer) Finalize(status string, succeeded, failed, skipped int, dur time.Duration) error {
	w.meta.Status = status
	w.meta.ClustersSuccess = succeeded
	w.meta.ClustersFailed = failed
	w.meta.ClustersSkipped = skipped
	w.meta.DurationSeconds = dur.Seconds()
	w.meta.FinishedAt = time.Now().UTC()

	if err := w.writeMeta(); err != nil {
		return fmt.Errorf("writing final meta.json: %w", err)
	}

	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// ResumeWriter opens an existing run directory for appending. It overwrites
// meta.json with the new metadata and opens results.jsonl in append mode.
func ResumeWriter(runDir string, meta RunMeta) (*Writer, error) {
	if meta.RunID == "" {
		meta.RunID = filepath.Base(runDir)
	}

	w := &Writer{
		runDir: runDir,
		meta:   meta,
	}

	if err := w.writeMeta(); err != nil {
		return nil, fmt.Errorf("writing resume meta.json: %w", err)
	}

	jsonlPath := filepath.Join(runDir, "results.jsonl")
	f, err := os.OpenFile(jsonlPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening results.jsonl for resume: %w", err)
	}
	w.file = f

	return w, nil
}

// writeMeta marshals the current RunMeta and writes it to meta.json,
// overwriting any previous content.
func (w *Writer) writeMeta() error {
	data, err := json.MarshalIndent(w.meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling meta: %w", err)
	}
	metaPath := filepath.Join(w.runDir, "meta.json")
	return os.WriteFile(metaPath, data, 0644)
}
