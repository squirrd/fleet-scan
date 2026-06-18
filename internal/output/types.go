package output

import (
	"encoding/json"
	"time"

	"github.com/squirrd/fleet-scan/internal/ocm"
)

// CollectorResult is the per-collector envelope in a ClusterRecord.
// Status is one of "success", "error", or "skipped".
type CollectorResult struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error"`
}

// ClusterRecord is one JSONL line — cluster metadata plus per-collector results.
type ClusterRecord struct {
	ClusterMetadata ocm.ClusterMetadata    `json:"cluster_metadata"`
	ClusterResult   map[string]CollectorResult `json:"cluster_result"`
}

// RunMeta is the top-level metadata written to meta.json.
type RunMeta struct {
	RunID                 string    `json:"run_id,omitempty"`
	Status                string    `json:"status"`
	Search                string    `json:"search"`
	Collectors            []string  `json:"collectors"`
	ClusterTimeoutSeconds int       `json:"cluster_timeout_seconds,omitempty"`
	StartedAt             time.Time `json:"started_at"`
	FinishedAt            time.Time `json:"finished_at,omitempty"`
	DurationSeconds       float64   `json:"duration_seconds"`
	ClustersTotal         int       `json:"clusters_total"`
	ClustersSuccess       int       `json:"clusters_success"`
	ClustersFailed        int       `json:"clusters_failed"`
	ClustersSkipped       int       `json:"clusters_skipped"`
	ResumedAt             time.Time `json:"resumed_at,omitempty"`
}
