package collector

import (
	"context"
	"encoding/json"
	"testing"
)

// TestMachineAudit_ParamConfig_Acceptance verifies that:
// 1. A "machine-audit" collector can be constructed via newMachineAuditCollector()
// 2. Name() returns "machine-audit"
// 3. Configure() accepts nil or empty params and returns nil (no params needed)
// 4. Configure() ignores unknown params without error
// 5. The collector registers itself via init() so Get("machine-audit") works
// 6. The default collector has a non-nil clientBuilder
func TestMachineAudit_ParamConfig_Acceptance(t *testing.T) {
	t.Run("constructor and Name", func(t *testing.T) {
		c := newMachineAuditCollector()
		if c.Name() != "machine-audit" {
			t.Errorf("Name() = %q, want %q", c.Name(), "machine-audit")
		}
	})

	t.Run("Configure with nil params returns nil", func(t *testing.T) {
		c := newMachineAuditCollector()
		if err := c.Configure(nil); err != nil {
			t.Errorf("Configure(nil) error: %v", err)
		}
	})

	t.Run("Configure with empty params returns nil", func(t *testing.T) {
		c := newMachineAuditCollector()
		if err := c.Configure(map[string]string{}); err != nil {
			t.Errorf("Configure({}) error: %v", err)
		}
	})

	t.Run("Configure ignores unknown params", func(t *testing.T) {
		c := newMachineAuditCollector()
		if err := c.Configure(map[string]string{"unknown": "val"}); err != nil {
			t.Errorf("Configure with unknown params error: %v", err)
		}
	})

	t.Run("registered via init so Get works", func(t *testing.T) {
		c, err := Get("machine-audit")
		if err != nil {
			t.Fatalf("Get(%q) returned error: %v", "machine-audit", err)
		}
		if c.Name() != "machine-audit" {
			t.Errorf("Name() = %q, want %q", c.Name(), "machine-audit")
		}
	})

	t.Run("default collector has non-nil clientBuilder", func(t *testing.T) {
		c := newMachineAuditCollector()
		ma := c.(*machineAuditCollector)
		if ma.clientBuilder == nil {
			t.Error("default collector should have a non-nil clientBuilder")
		}
	})
}

// TestMachineAudit_DataExtraction_Acceptance verifies that Run() passes raw
// objects straight through to the output schema with no flattening, and
// that cpms/machine_configuration are null (not an error) when not found.
func TestMachineAudit_DataExtraction_Acceptance(t *testing.T) {
	mustUnmarshal := func(s string) map[string]interface{} {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatalf("test fixture JSON invalid: %v\n%s", err, s)
		}
		return m
	}

	clusterVersionObj := mustUnmarshal(`{
		"metadata": {"name": "version"},
		"status": {
			"history": [{"state": "Completed", "version": "4.22.5"}],
			"conditions": [{"type": "Upgradeable", "reason": "ClusterBootImageSkewError"}]
		}
	}`)
	infrastructureObj := mustUnmarshal(`{
		"metadata": {"name": "cluster"},
		"status": {"platformStatus": {"aws": {"region": "us-east-1"}}}
	}`)
	cpmsObj := mustUnmarshal(`{
		"metadata": {"name": "cluster", "namespace": "openshift-machine-api"},
		"spec": {"template": {"machines_v1beta1_machine_openshift_io": {"spec": {"providerSpec": {"value": {"ami": {"id": "ami-master1"}}}}}}}
	}`)
	machineConfigObj := mustUnmarshal(`{
		"spec": {"managedBootImages": {"machineManagers": []}}
	}`)
	ms1 := mustUnmarshal(`{"metadata": {"name": "worker-us-east-1a"}, "spec": {"replicas": 3}}`)
	cmObj := mustUnmarshal(`{
		"metadata": {"name": "coreos-bootimages", "namespace": "openshift-machine-config-operator"},
		"data": {"stream": "{\"architectures\":{}}"}
	}`)
	masterNode := mustUnmarshal(`{
		"metadata": {"name": "master-0", "labels": {"node-role.kubernetes.io/master": ""}},
		"status": {"nodeInfo": {"osImage": "Red Hat Enterprise Linux CoreOS 422.5"}}
	}`)

	runWith := func(t *testing.T, data fakeMachineAuditData) machineAuditResult {
		t.Helper()
		c := newMachineAuditCollector()
		ma := c.(*machineAuditCollector)
		ma.clientBuilder = fakeMachineAuditClientBuilder(data)

		raw, err := ma.Run(context.Background(), "cluster-123", "/fake/kubeconfig")
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
		var result machineAuditResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal output: %v\nraw: %s", err, raw)
		}
		return result
	}

	t.Run("full output with all resources present", func(t *testing.T) {
		result := runWith(t, fakeMachineAuditData{
			clusterVersionObj:     clusterVersionObj,
			infrastructureObj:     infrastructureObj,
			cpmsObj:               cpmsObj,
			machineConfigObj:      machineConfigObj,
			machineSets:           []map[string]interface{}{ms1},
			coreosBootimagesCMObj: cmObj,
			masterNodes:           []map[string]interface{}{masterNode},
		})

		if result.ClusterVersion["metadata"].(map[string]interface{})["name"] != "version" {
			t.Errorf("cluster_version not passed through raw: %v", result.ClusterVersion)
		}
		if result.Infrastructure["status"] == nil {
			t.Errorf("infrastructure not passed through raw: %v", result.Infrastructure)
		}
		if result.CPMS == nil {
			t.Fatal("cpms should not be nil when present")
		}
		if result.MachineConfiguration == nil {
			t.Fatal("machine_configuration should not be nil when present")
		}
		if len(result.MachineSets) != 1 {
			t.Fatalf("len(machine_sets) = %d, want 1", len(result.MachineSets))
		}
		if result.MachineSets[0]["metadata"].(map[string]interface{})["name"] != "worker-us-east-1a" {
			t.Errorf("machine_sets[0] not passed through raw: %v", result.MachineSets[0])
		}
		if result.CoreOSBootImages["metadata"].(map[string]interface{})["name"] != "coreos-bootimages" {
			t.Errorf("coreos_bootimages not passed through raw: %v", result.CoreOSBootImages)
		}
		if len(result.MasterNodes) != 1 {
			t.Fatalf("len(master_nodes) = %d, want 1", len(result.MasterNodes))
		}
	})

	t.Run("cpms is null when not found (not an error)", func(t *testing.T) {
		result := runWith(t, fakeMachineAuditData{
			clusterVersionObj:     clusterVersionObj,
			infrastructureObj:     infrastructureObj,
			cpmsNotFound:          true,
			machineConfigObj:      machineConfigObj,
			coreosBootimagesCMObj: cmObj,
		})

		if result.CPMS != nil {
			t.Errorf("cpms = %v, want nil when not found", result.CPMS)
		}
	})

	t.Run("machine_configuration is null when not found (not an error)", func(t *testing.T) {
		result := runWith(t, fakeMachineAuditData{
			clusterVersionObj:     clusterVersionObj,
			infrastructureObj:     infrastructureObj,
			cpmsObj:               cpmsObj,
			machineConfigNotFound: true,
			coreosBootimagesCMObj: cmObj,
		})

		if result.MachineConfiguration != nil {
			t.Errorf("machine_configuration = %v, want nil when not found", result.MachineConfiguration)
		}
	})

	t.Run("no MachineSets yields empty, not null, list", func(t *testing.T) {
		raw, err := func() (json.RawMessage, error) {
			c := newMachineAuditCollector()
			ma := c.(*machineAuditCollector)
			ma.clientBuilder = fakeMachineAuditClientBuilder(fakeMachineAuditData{
				clusterVersionObj:     clusterVersionObj,
				infrastructureObj:     infrastructureObj,
				cpmsObj:               cpmsObj,
				machineConfigObj:      machineConfigObj,
				coreosBootimagesCMObj: cmObj,
				machineSets:           nil,
			})
			return ma.Run(context.Background(), "cluster-123", "/fake/kubeconfig")
		}()
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
		var raw2 struct {
			MachineSets []map[string]interface{} `json:"machine_sets"`
		}
		if err := json.Unmarshal(raw, &raw2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if raw2.MachineSets == nil {
			t.Error("machine_sets should be [], not null, when empty")
		}
	})
}

// TestMachineAudit_KubeClientWiring_Acceptance verifies that:
// 1. The real kube client builder exists and is non-nil
// 2. machineAuditKubeLister implements machineAuditLister (compile-time check)
// 3. With an invalid kubeconfig, Run() returns a descriptive error (not a panic)
// 4. The default collector uses the real builder
// 5. GVRs use the correct groups/versions
func TestMachineAudit_KubeClientWiring_Acceptance(t *testing.T) {
	t.Run("newMachineAuditKubeClientBuilder returns non-nil", func(t *testing.T) {
		builder := newMachineAuditKubeClientBuilder()
		if builder == nil {
			t.Fatal("newMachineAuditKubeClientBuilder() returned nil")
		}
	})

	t.Run("machineAuditKubeLister satisfies machineAuditLister interface", func(t *testing.T) {
		var _ machineAuditLister = (*machineAuditKubeLister)(nil)
	})

	t.Run("invalid kubeconfig returns descriptive error", func(t *testing.T) {
		c := newMachineAuditCollector()
		ma := c.(*machineAuditCollector)
		ma.clientBuilder = newMachineAuditKubeClientBuilder()

		_, err := ma.Run(context.Background(), "cluster-789", "/nonexistent/kubeconfig/path")
		if err == nil {
			t.Fatal("expected error for invalid kubeconfig, got nil")
		}
		if err.Error() == "" {
			t.Fatal("error message should not be empty")
		}
	})

	t.Run("default collector uses real kube client builder", func(t *testing.T) {
		c := newMachineAuditCollector()
		ma := c.(*machineAuditCollector)
		if ma.clientBuilder == nil {
			t.Error("default collector should have a non-nil clientBuilder")
		}
	})

	t.Run("infrastructureGVR uses correct config.openshift.io group", func(t *testing.T) {
		if infrastructureGVR.Group != "config.openshift.io" || infrastructureGVR.Version != "v1" || infrastructureGVR.Resource != "infrastructures" {
			t.Errorf("infrastructureGVR = %+v, want config.openshift.io/v1 infrastructures", infrastructureGVR)
		}
	})

	t.Run("controlPlaneMachineSetGVR uses correct machine.openshift.io/v1 group", func(t *testing.T) {
		if controlPlaneMachineSetGVR.Group != "machine.openshift.io" || controlPlaneMachineSetGVR.Version != "v1" || controlPlaneMachineSetGVR.Resource != "controlplanemachinesets" {
			t.Errorf("controlPlaneMachineSetGVR = %+v, want machine.openshift.io/v1 controlplanemachinesets", controlPlaneMachineSetGVR)
		}
	})

	t.Run("configMapGVR uses core v1 group", func(t *testing.T) {
		if configMapGVR.Group != "" || configMapGVR.Version != "v1" || configMapGVR.Resource != "configmaps" {
			t.Errorf("configMapGVR = %+v, want core/v1 configmaps", configMapGVR)
		}
	})

	t.Run("nodeGVR uses core v1 group", func(t *testing.T) {
		if nodeGVR.Group != "" || nodeGVR.Version != "v1" || nodeGVR.Resource != "nodes" {
			t.Errorf("nodeGVR = %+v, want core/v1 nodes", nodeGVR)
		}
	})
}
