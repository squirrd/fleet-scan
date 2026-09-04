package collector

import (
	"context"
	"encoding/json"
	"fmt"
)

type bootImagesClientBuilderFunc func(kubeconfigPath string) (bootImagesLister, error)

type bootImagesLister interface {
	GetClusterVersion(ctx context.Context) (map[string]interface{}, error)
	// GetMachineConfiguration returns (nil, nil) when the resource is not found.
	GetMachineConfiguration(ctx context.Context) (map[string]interface{}, error)
	ListMachineSets(ctx context.Context) ([]map[string]interface{}, error)
}

type machineSetEntry struct {
	Name          string `json:"name"`
	AMIID         string `json:"ami_id"`
	Replicas      int64  `json:"replicas"`
	ReadyReplicas int64  `json:"ready_replicas"`
}

type bootImagesResult struct {
	ClusterVersion       string            `json:"cluster_version"`
	BootImageMgmtEnabled *bool             `json:"boot_image_mgmt_enabled"`
	MachineSets          []machineSetEntry `json:"machine_sets"`
}

type bootImagesCollector struct {
	clientBuilder bootImagesClientBuilderFunc
}

func newBootImagesCollector() Collector {
	return &bootImagesCollector{
		clientBuilder: newBootImagesKubeClientBuilder(),
	}
}

func (c *bootImagesCollector) Name() string { return "boot-images" }

func (c *bootImagesCollector) Configure(_ map[string]string) error { return nil }

func (c *bootImagesCollector) Run(ctx context.Context, _, kubeconfigPath string) (json.RawMessage, error) {
	lister, err := c.clientBuilder(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("boot-images: building client: %w", err)
	}

	cvObj, err := lister.GetClusterVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("boot-images: getting ClusterVersion: %w", err)
	}

	mcObj, err := lister.GetMachineConfiguration(ctx)
	if err != nil {
		return nil, fmt.Errorf("boot-images: getting MachineConfiguration: %w", err)
	}

	msObjs, err := lister.ListMachineSets(ctx)
	if err != nil {
		return nil, fmt.Errorf("boot-images: listing MachineSets: %w", err)
	}

	sets := make([]machineSetEntry, 0, len(msObjs))
	for _, ms := range msObjs {
		sets = append(sets, extractMachineSetEntry(ms))
	}

	return json.Marshal(bootImagesResult{
		ClusterVersion:       extractClusterVersion(cvObj),
		BootImageMgmtEnabled: extractBootImageMgmtEnabled(mcObj),
		MachineSets:          sets,
	})
}

func extractClusterVersion(obj map[string]interface{}) string {
	status, _ := obj["status"].(map[string]interface{})
	history, _ := status["history"].([]interface{})
	for _, entry := range history {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		if state, _ := m["state"].(string); state == "Completed" {
			if version, _ := m["version"].(string); version != "" {
				return version
			}
		}
	}
	return ""
}

func extractBootImageMgmtEnabled(obj map[string]interface{}) *bool {
	if obj == nil {
		return nil
	}
	spec, ok := obj["spec"].(map[string]interface{})
	if !ok {
		t := true
		return &t
	}
	mbi, ok := spec["managedBootImages"]
	if !ok || mbi == nil {
		t := true
		return &t
	}
	mbiMap, ok := mbi.(map[string]interface{})
	if !ok {
		t := true
		return &t
	}
	mm, ok := mbiMap["machineManagers"]
	if !ok || mm == nil {
		t := true
		return &t
	}
	managers, ok := mm.([]interface{})
	if !ok {
		t := true
		return &t
	}
	if len(managers) == 0 {
		f := false
		return &f
	}
	t := true
	return &t
}

func extractMachineSetEntry(obj map[string]interface{}) machineSetEntry {
	meta, _ := obj["metadata"].(map[string]interface{})
	name, _ := meta["name"].(string)
	amiID := nestedStringFromMap(obj, "spec", "template", "spec", "providerSpec", "value", "ami", "id")
	replicas := nestedInt64FromMap(obj, "spec", "replicas")
	readyReplicas := nestedInt64FromMap(obj, "status", "readyReplicas")
	return machineSetEntry{
		Name:          name,
		AMIID:         amiID,
		Replicas:      replicas,
		ReadyReplicas: readyReplicas,
	}
}

// nestedStringFromMap traverses obj following fields and returns the string at the leaf.
// Returns "" if any step is absent, wrong type, or not a string at the leaf.
func nestedStringFromMap(obj map[string]interface{}, fields ...string) string {
	curr := obj
	for i, field := range fields {
		val, ok := curr[field]
		if !ok {
			return ""
		}
		if i == len(fields)-1 {
			s, _ := val.(string)
			return s
		}
		next, ok := val.(map[string]interface{})
		if !ok {
			return ""
		}
		curr = next
	}
	return ""
}

// nestedInt64FromMap traverses obj and returns the int64 at the leaf.
// JSON numbers unmarshal to float64, so this converts float64 → int64.
func nestedInt64FromMap(obj map[string]interface{}, fields ...string) int64 {
	curr := obj
	for i, field := range fields {
		val, ok := curr[field]
		if !ok {
			return 0
		}
		if i == len(fields)-1 {
			f, _ := val.(float64)
			return int64(f)
		}
		next, ok := val.(map[string]interface{})
		if !ok {
			return 0
		}
		curr = next
	}
	return 0
}

func init() {
	Register("boot-images", newBootImagesCollector)
}
