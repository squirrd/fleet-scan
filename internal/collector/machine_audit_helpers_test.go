package collector

import "context"

type fakeMachineAuditData struct {
	clusterVersionObj     map[string]interface{}
	infrastructureObj     map[string]interface{}
	cpmsObj               map[string]interface{}
	cpmsNotFound          bool
	machineConfigObj      map[string]interface{}
	machineConfigNotFound bool
	machineSets           []map[string]interface{}
	coreosBootimagesCMObj map[string]interface{}
	masterNodes           []map[string]interface{}
}

type fakeMachineAuditLister struct {
	data fakeMachineAuditData
}

func (f *fakeMachineAuditLister) GetClusterVersion(ctx context.Context) (map[string]interface{}, error) {
	return f.data.clusterVersionObj, nil
}

func (f *fakeMachineAuditLister) GetInfrastructure(ctx context.Context) (map[string]interface{}, error) {
	return f.data.infrastructureObj, nil
}

func (f *fakeMachineAuditLister) GetControlPlaneMachineSet(ctx context.Context) (map[string]interface{}, error) {
	if f.data.cpmsNotFound {
		return nil, nil
	}
	return f.data.cpmsObj, nil
}

func (f *fakeMachineAuditLister) GetMachineConfiguration(ctx context.Context) (map[string]interface{}, error) {
	if f.data.machineConfigNotFound {
		return nil, nil
	}
	return f.data.machineConfigObj, nil
}

func (f *fakeMachineAuditLister) ListMachineSets(ctx context.Context) ([]map[string]interface{}, error) {
	return f.data.machineSets, nil
}

func (f *fakeMachineAuditLister) GetCoreOSBootImagesConfigMap(ctx context.Context) (map[string]interface{}, error) {
	return f.data.coreosBootimagesCMObj, nil
}

func (f *fakeMachineAuditLister) ListMasterNodes(ctx context.Context) ([]map[string]interface{}, error) {
	return f.data.masterNodes, nil
}

func fakeMachineAuditClientBuilder(data fakeMachineAuditData) machineAuditClientBuilderFunc {
	return func(kubeconfigPath string) (machineAuditLister, error) {
		return &fakeMachineAuditLister{data: data}, nil
	}
}
