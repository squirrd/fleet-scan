package collector

import "context"

type fakeBootImagesData struct {
	clusterVersionObj     map[string]interface{}
	machineConfigObj      map[string]interface{}
	machineConfigNotFound bool
	machineSets           []map[string]interface{}
}

type fakeBootImagesLister struct {
	data fakeBootImagesData
}

func (f *fakeBootImagesLister) GetClusterVersion(ctx context.Context) (map[string]interface{}, error) {
	return f.data.clusterVersionObj, nil
}

func (f *fakeBootImagesLister) GetMachineConfiguration(ctx context.Context) (map[string]interface{}, error) {
	if f.data.machineConfigNotFound {
		return nil, nil
	}
	return f.data.machineConfigObj, nil
}

func (f *fakeBootImagesLister) ListMachineSets(ctx context.Context) ([]map[string]interface{}, error) {
	return f.data.machineSets, nil
}

func fakeBootImagesClientBuilder(data fakeBootImagesData) bootImagesClientBuilderFunc {
	return func(kubeconfigPath string) (bootImagesLister, error) {
		return &fakeBootImagesLister{data: data}, nil
	}
}
