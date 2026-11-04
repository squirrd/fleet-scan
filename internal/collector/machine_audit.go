package collector

import (
	"context"
	"encoding/json"
	"fmt"
)

type machineAuditClientBuilderFunc func(kubeconfigPath string) (machineAuditLister, error)

type machineAuditLister interface {
	GetClusterVersion(ctx context.Context) (map[string]interface{}, error)
	GetInfrastructure(ctx context.Context) (map[string]interface{}, error)
	// GetControlPlaneMachineSet returns (nil, nil) when the resource is not found.
	GetControlPlaneMachineSet(ctx context.Context) (map[string]interface{}, error)
	GetMachineConfiguration(ctx context.Context) (map[string]interface{}, error)
	ListMachineSets(ctx context.Context) ([]map[string]interface{}, error)
	GetCoreOSBootImagesConfigMap(ctx context.Context) (map[string]interface{}, error)
	ListMasterNodes(ctx context.Context) ([]map[string]interface{}, error)
}

type machineAuditResult struct {
	ClusterVersion       map[string]interface{}   `json:"cluster_version"`
	Infrastructure       map[string]interface{}   `json:"infrastructure"`
	CPMS                 map[string]interface{}   `json:"cpms"`
	MachineConfiguration map[string]interface{}   `json:"machine_configuration"`
	MachineSets          []map[string]interface{} `json:"machine_sets"`
	CoreOSBootImages     map[string]interface{}   `json:"coreos_bootimages"`
	MasterNodes          []map[string]interface{} `json:"master_nodes"`
}

type machineAuditCollector struct {
	clientBuilder machineAuditClientBuilderFunc
}

func newMachineAuditCollector() Collector {
	return &machineAuditCollector{
		clientBuilder: newMachineAuditKubeClientBuilder(),
	}
}

func (c *machineAuditCollector) Name() string { return "machine-audit" }

func (c *machineAuditCollector) Configure(_ map[string]string) error { return nil }

func (c *machineAuditCollector) Run(ctx context.Context, _, kubeconfigPath string) (json.RawMessage, error) {
	lister, err := c.clientBuilder(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("machine-audit: building client: %w", err)
	}

	cvObj, err := lister.GetClusterVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("machine-audit: getting ClusterVersion: %w", err)
	}

	infraObj, err := lister.GetInfrastructure(ctx)
	if err != nil {
		return nil, fmt.Errorf("machine-audit: getting Infrastructure: %w", err)
	}

	cpmsObj, err := lister.GetControlPlaneMachineSet(ctx)
	if err != nil {
		return nil, fmt.Errorf("machine-audit: getting ControlPlaneMachineSet: %w", err)
	}

	mcObj, err := lister.GetMachineConfiguration(ctx)
	if err != nil {
		return nil, fmt.Errorf("machine-audit: getting MachineConfiguration: %w", err)
	}

	msObjs, err := lister.ListMachineSets(ctx)
	if err != nil {
		return nil, fmt.Errorf("machine-audit: listing MachineSets: %w", err)
	}

	cmObj, err := lister.GetCoreOSBootImagesConfigMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("machine-audit: getting coreos-bootimages ConfigMap: %w", err)
	}

	masterNodes, err := lister.ListMasterNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("machine-audit: listing master Nodes: %w", err)
	}

	if msObjs == nil {
		msObjs = []map[string]interface{}{}
	}
	if masterNodes == nil {
		masterNodes = []map[string]interface{}{}
	}

	return json.Marshal(machineAuditResult{
		ClusterVersion:       cvObj,
		Infrastructure:       infraObj,
		CPMS:                 cpmsObj,
		MachineConfiguration: mcObj,
		MachineSets:          msObjs,
		CoreOSBootImages:     cmObj,
		MasterNodes:          masterNodes,
	})
}

func init() {
	Register("machine-audit", newMachineAuditCollector)
}
