package collector

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	clusterVersionGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusterversions",
	}
	machineConfigurationGVR = schema.GroupVersionResource{
		Group:    "operator.openshift.io",
		Version:  "v1",
		Resource: "machineconfigurations",
	}
	machineSetGVR = schema.GroupVersionResource{
		Group:    "machine.openshift.io",
		Version:  "v1beta1",
		Resource: "machinesets",
	}
)

const machineAPINamespace = "openshift-machine-api"

type bootImagesKubeLister struct {
	dynamicClient dynamic.Interface
}

func (k *bootImagesKubeLister) GetClusterVersion(ctx context.Context) (map[string]interface{}, error) {
	obj, err := k.dynamicClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting ClusterVersion/version: %w", err)
	}
	return obj.Object, nil
}

func (k *bootImagesKubeLister) GetMachineConfiguration(ctx context.Context) (map[string]interface{}, error) {
	obj, err := k.dynamicClient.Resource(machineConfigurationGVR).Get(ctx, "cluster", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting MachineConfiguration/cluster: %w", err)
	}
	return obj.Object, nil
}

func (k *bootImagesKubeLister) ListMachineSets(ctx context.Context) ([]map[string]interface{}, error) {
	list, err := k.dynamicClient.Resource(machineSetGVR).Namespace(machineAPINamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing MachineSets in %s: %w", machineAPINamespace, err)
	}
	result := make([]map[string]interface{}, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, item.Object)
	}
	return result, nil
}

func newBootImagesKubeClientBuilder() bootImagesClientBuilderFunc {
	return func(kubeconfigPath string) (bootImagesLister, error) {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("boot-images: building rest config: %w", err)
		}
		dynClient, err := dynamic.NewForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("boot-images: creating dynamic client: %w", err)
		}
		return &bootImagesKubeLister{dynamicClient: dynClient}, nil
	}
}
