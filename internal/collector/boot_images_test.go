package collector

import (
	"context"
	"encoding/json"
	"testing"
)

// TestBootImages_ParamConfig_Acceptance verifies that:
// 1. A "boot-images" collector can be constructed via newBootImagesCollector()
// 2. Name() returns "boot-images"
// 3. Configure() accepts nil or empty params and returns nil (no params needed)
// 4. Configure() ignores unknown params without error
// 5. The collector registers itself via init() so Get("boot-images") works
// 6. The default collector has a non-nil clientBuilder
//
// Phase: RED — the boot-images collector does not exist yet.
func TestBootImages_ParamConfig_Acceptance(t *testing.T) {
	t.Run("constructor and Name", func(t *testing.T) {
		c := newBootImagesCollector()
		if c.Name() != "boot-images" {
			t.Errorf("Name() = %q, want %q", c.Name(), "boot-images")
		}
	})

	t.Run("Configure with nil params returns nil", func(t *testing.T) {
		c := newBootImagesCollector()
		if err := c.Configure(nil); err != nil {
			t.Errorf("Configure(nil) error: %v", err)
		}
	})

	t.Run("Configure with empty params returns nil", func(t *testing.T) {
		c := newBootImagesCollector()
		if err := c.Configure(map[string]string{}); err != nil {
			t.Errorf("Configure({}) error: %v", err)
		}
	})

	t.Run("Configure ignores unknown params", func(t *testing.T) {
		c := newBootImagesCollector()
		if err := c.Configure(map[string]string{"unknown": "val"}); err != nil {
			t.Errorf("Configure with unknown params error: %v", err)
		}
	})

	t.Run("registered via init so Get works", func(t *testing.T) {
		c, err := Get("boot-images")
		if err != nil {
			t.Fatalf("Get(%q) returned error: %v", "boot-images", err)
		}
		if c.Name() != "boot-images" {
			t.Errorf("Name() = %q, want %q", c.Name(), "boot-images")
		}
	})

	t.Run("default collector has non-nil clientBuilder", func(t *testing.T) {
		c := newBootImagesCollector()
		bi := c.(*bootImagesCollector)
		if bi.clientBuilder == nil {
			t.Error("default collector should have a non-nil clientBuilder")
		}
	})
}

// TestBootImages_DataExtraction_Acceptance verifies that Run() correctly:
// 1. Extracts cluster_version from the first Completed history entry
// 2. Sets boot_image_mgmt_enabled to true/false/null based on MachineConfiguration
// 3. Extracts name, ami_id, replicas, ready_replicas from each MachineSet
//
// Phase: RED — the boot-images collector does not exist yet.
func TestBootImages_DataExtraction_Acceptance(t *testing.T) {
	mustUnmarshal := func(s string) map[string]interface{} {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatalf("test fixture JSON invalid: %v\n%s", err, s)
		}
		return m
	}
	mustUnmarshalList := func(items ...string) []map[string]interface{} {
		result := make([]map[string]interface{}, 0, len(items))
		for _, s := range items {
			result = append(result, mustUnmarshal(s))
		}
		return result
	}

	clusterVersionTwoEntries := mustUnmarshal(`{
		"status": {
			"history": [
				{"state": "Partial", "version": "4.21.0"},
				{"state": "Completed", "version": "4.22.5"}
			]
		}
	}`)

	clusterVersionCompleted := mustUnmarshal(`{
		"status": {
			"history": [
				{"state": "Completed", "version": "4.22.5"}
			]
		}
	}`)

	machineConfigDisabled := mustUnmarshal(`{
		"spec": {
			"managedBootImages": {
				"machineManagers": []
			}
		}
	}`)

	machineConfigNoMBI := mustUnmarshal(`{"spec": {}}`)

	ms1 := `{
		"metadata": {"name": "worker-us-east-1a"},
		"spec": {
			"replicas": 3,
			"template": {
				"spec": {
					"providerSpec": {
						"value": {
							"ami": {"id": "ami-aaa111"}
						}
					}
				}
			}
		},
		"status": {"readyReplicas": 3}
	}`
	ms2 := `{
		"metadata": {"name": "worker-us-east-1b"},
		"spec": {
			"replicas": 2,
			"template": {
				"spec": {
					"providerSpec": {
						"value": {
							"ami": {"id": "ami-bbb222"}
						}
					}
				}
			}
		},
		"status": {"readyReplicas": 2}
	}`

	runWith := func(t *testing.T, data fakeBootImagesData) bootImagesResult {
		t.Helper()
		c := newBootImagesCollector()
		bi := c.(*bootImagesCollector)
		bi.clientBuilder = fakeBootImagesClientBuilder(data)

		raw, err := bi.Run(context.Background(), "cluster-123", "/fake/kubeconfig")
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
		var result bootImagesResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal output: %v\nraw: %s", err, raw)
		}
		return result
	}

	t.Run("full output with two MachineSets and boot_image_mgmt_enabled=true", func(t *testing.T) {
		result := runWith(t, fakeBootImagesData{
			clusterVersionObj: clusterVersionCompleted,
			machineConfigObj:  machineConfigNoMBI,
			machineSets:       mustUnmarshalList(ms1, ms2),
		})

		if result.ClusterVersion != "4.22.5" {
			t.Errorf("cluster_version = %q, want %q", result.ClusterVersion, "4.22.5")
		}
		if result.BootImageMgmtEnabled == nil || *result.BootImageMgmtEnabled != true {
			t.Errorf("boot_image_mgmt_enabled = %v, want true", result.BootImageMgmtEnabled)
		}
		if len(result.MachineSets) != 2 {
			t.Fatalf("len(machine_sets) = %d, want 2", len(result.MachineSets))
		}
		if result.MachineSets[0].Name != "worker-us-east-1a" {
			t.Errorf("machine_sets[0].name = %q, want %q", result.MachineSets[0].Name, "worker-us-east-1a")
		}
		if result.MachineSets[0].AMIID != "ami-aaa111" {
			t.Errorf("machine_sets[0].ami_id = %q, want %q", result.MachineSets[0].AMIID, "ami-aaa111")
		}
		if result.MachineSets[0].Replicas != 3 {
			t.Errorf("machine_sets[0].replicas = %d, want 3", result.MachineSets[0].Replicas)
		}
		if result.MachineSets[0].ReadyReplicas != 3 {
			t.Errorf("machine_sets[0].ready_replicas = %d, want 3", result.MachineSets[0].ReadyReplicas)
		}
		if result.MachineSets[1].AMIID != "ami-bbb222" {
			t.Errorf("machine_sets[1].ami_id = %q, want %q", result.MachineSets[1].AMIID, "ami-bbb222")
		}
	})

	t.Run("boot_image_mgmt_enabled=false when machineManagers is empty array", func(t *testing.T) {
		result := runWith(t, fakeBootImagesData{
			clusterVersionObj: clusterVersionCompleted,
			machineConfigObj:  machineConfigDisabled,
			machineSets:       nil,
		})

		if result.BootImageMgmtEnabled == nil {
			t.Fatal("boot_image_mgmt_enabled should not be null")
		}
		if *result.BootImageMgmtEnabled != false {
			t.Errorf("boot_image_mgmt_enabled = %v, want false", *result.BootImageMgmtEnabled)
		}
	})

	t.Run("boot_image_mgmt_enabled=null when MachineConfiguration not found", func(t *testing.T) {
		c := newBootImagesCollector()
		bi := c.(*bootImagesCollector)
		bi.clientBuilder = fakeBootImagesClientBuilder(fakeBootImagesData{
			clusterVersionObj:     clusterVersionCompleted,
			machineConfigNotFound: true,
			machineSets:           nil,
		})

		raw, err := bi.Run(context.Background(), "cluster-123", "/fake/kubeconfig")
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}

		// Check the *bool pointer is nil (JSON null)
		var result struct {
			BootImageMgmtEnabled *bool `json:"boot_image_mgmt_enabled"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if result.BootImageMgmtEnabled != nil {
			t.Errorf("boot_image_mgmt_enabled = %v, want null", *result.BootImageMgmtEnabled)
		}
	})

	t.Run("cluster_version uses first Completed history entry (skips Partial)", func(t *testing.T) {
		result := runWith(t, fakeBootImagesData{
			clusterVersionObj: clusterVersionTwoEntries,
			machineConfigObj:  machineConfigNoMBI,
			machineSets:       nil,
		})

		if result.ClusterVersion != "4.22.5" {
			t.Errorf("cluster_version = %q, want %q (should skip Partial)", result.ClusterVersion, "4.22.5")
		}
	})

	t.Run("AMI ID extracted from seven-level-deep nested path", func(t *testing.T) {
		result := runWith(t, fakeBootImagesData{
			clusterVersionObj: clusterVersionCompleted,
			machineConfigObj:  machineConfigNoMBI,
			machineSets:       mustUnmarshalList(ms1),
		})

		if len(result.MachineSets) != 1 {
			t.Fatalf("expected 1 machineSet, got %d", len(result.MachineSets))
		}
		if result.MachineSets[0].AMIID != "ami-aaa111" {
			t.Errorf("ami_id = %q, want %q", result.MachineSets[0].AMIID, "ami-aaa111")
		}
	})

	t.Run("MachineSet with missing providerSpec yields empty ami_id", func(t *testing.T) {
		msNoAMI := mustUnmarshal(`{
			"metadata": {"name": "worker-noami"},
			"spec": {
				"replicas": 1,
				"template": {}
			},
			"status": {"readyReplicas": 1}
		}`)
		result := runWith(t, fakeBootImagesData{
			clusterVersionObj: clusterVersionCompleted,
			machineConfigObj:  machineConfigNoMBI,
			machineSets:       []map[string]interface{}{msNoAMI},
		})

		if len(result.MachineSets) != 1 {
			t.Fatalf("expected 1 machineSet, got %d", len(result.MachineSets))
		}
		if result.MachineSets[0].AMIID != "" {
			t.Errorf("ami_id = %q, want empty string for missing providerSpec", result.MachineSets[0].AMIID)
		}
	})
}

// TestBootImages_KubeClientWiring_Acceptance verifies that:
// 1. The real kube client builder exists and is non-nil
// 2. bootImagesKubeLister implements bootImagesLister (compile-time check)
// 3. With an invalid kubeconfig, Run() returns a descriptive error (not a panic)
// 4. The default collector uses the real builder
//
// Phase: RED — the boot-images collector does not exist yet.
func TestBootImages_KubeClientWiring_Acceptance(t *testing.T) {
	t.Run("newBootImagesKubeClientBuilder returns non-nil", func(t *testing.T) {
		builder := newBootImagesKubeClientBuilder()
		if builder == nil {
			t.Fatal("newBootImagesKubeClientBuilder() returned nil")
		}
	})

	t.Run("bootImagesKubeLister satisfies bootImagesLister interface", func(t *testing.T) {
		var _ bootImagesLister = (*bootImagesKubeLister)(nil)
	})

	t.Run("invalid kubeconfig returns descriptive error", func(t *testing.T) {
		c := newBootImagesCollector()
		bi := c.(*bootImagesCollector)
		bi.clientBuilder = newBootImagesKubeClientBuilder()

		_, err := bi.Run(context.Background(), "cluster-789", "/nonexistent/kubeconfig/path")
		if err == nil {
			t.Fatal("expected error for invalid kubeconfig, got nil")
		}
		if err.Error() == "" {
			t.Fatal("error message should not be empty")
		}
	})

	t.Run("default collector uses real kube client builder", func(t *testing.T) {
		c := newBootImagesCollector()
		bi := c.(*bootImagesCollector)
		if bi.clientBuilder == nil {
			t.Error("default collector should have a non-nil clientBuilder")
		}
	})
}
