package output

import (
	"bufio"
	"encoding/json"
	"os"
)

// LoadCompletedSet reads an existing results.jsonl file and returns a set of
// cluster IDs whose records all succeeded (no collector has status "error").
// Corrupt lines are silently skipped. An empty file returns an empty set.
func LoadCompletedSet(jsonlPath string) (map[string]bool, error) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	completed := make(map[string]bool)
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var rec ClusterRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			// Skip corrupt lines.
			continue
		}

		clusterID := rec.ClusterMetadata.ID
		if clusterID == "" {
			continue
		}

		// A cluster is "succeeded" if none of its collectors have status "error".
		succeeded := true
		for _, cr := range rec.ClusterResult {
			if cr.Status == "error" {
				succeeded = false
				break
			}
		}

		if succeeded {
			completed[clusterID] = true
		}
	}

	return completed, nil
}
