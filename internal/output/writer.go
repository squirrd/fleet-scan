package output

import (
	"time"
)

// Writer writes JSONL records and meta.json to a timestamped run directory.
type Writer struct {
	// stub — will be implemented in slice 2
}

// NewWriter creates a new Writer. Stub for compilation.
func NewWriter(baseDir string, meta RunMeta) (*Writer, error) {
	panic("not implemented")
}

// RunDir returns the path to the run directory.
func (w *Writer) RunDir() string {
	panic("not implemented")
}

// WriteRecord writes a single ClusterRecord as a JSONL line.
func (w *Writer) WriteRecord(rec ClusterRecord) error {
	panic("not implemented")
}

// Finalize overwrites meta.json with final status and counts.
func (w *Writer) Finalize(status string, succeeded, failed, skipped int, dur time.Duration) error {
	panic("not implemented")
}
