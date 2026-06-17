package ocm

import "time"

// ClusterMetadata holds the 12 hardcoded fields extracted from OCM cluster responses.
type ClusterMetadata struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	ExternalID        string    `json:"external_id"`
	Product           string    `json:"product"`
	CloudProvider     string    `json:"cloud_provider"`
	Region            string    `json:"region"`
	State             string    `json:"state"`
	HealthState       string    `json:"health_state"`
	OpenshiftVersion  string    `json:"openshift_version"`
	MultiAZ           bool      `json:"multi_az"`
	Managed           bool      `json:"managed"`
	CreationTimestamp time.Time `json:"creation_timestamp"`
}
