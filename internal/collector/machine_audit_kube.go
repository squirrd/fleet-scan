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
	infrastructureGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "infrastructures",
	}
	controlPlaneMachineSetGVR = schema.GroupVersionResource{
		Group:    "machine.openshift.io",
		Version:  "v1",
		Resource: "controlplanemachinesets",
	}
	configMapGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "configmaps",
	}
	nodeGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "nodes",
	}
)

const (
	mcoNamespace       = "openshift-machine-config-operator"
	coreosBootimagesCM = "coreos-bootimages"
	masterNodeLabel    = "node-role.kubernetes.io/master"
)

type machineAuditKubeLister struct {
	dynamicClient dynamic.Interface
}

func (k *machineAuditKubeLister) GetClusterVersion(ctx context.Context) (map[string]interface{}, error) {
	obj, err := k.dynamicClient.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting ClusterVersion/version: %w", err)
	}
	return obj.Object, nil
}

func (k *machineAuditKubeLister) GetInfrastructure(ctx context.Context) (map[string]interface{}, error) {
	obj, err := k.dynamicClient.Resource(infrastructureGVR).Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting Infrastructure/cluster: %w", err)
	}
	return obj.Object, nil
}

func (k *machineAuditKubeLister) GetControlPlaneMachineSet(ctx context.Context) (map[string]interface{}, error) {
	obj, err := k.dynamicClient.Resource(controlPlaneMachineSetGVR).Namespace(machineAPINamespace).Get(ctx, "cluster", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting ControlPlaneMachineSet/cluster: %w", err)
	}
	return obj.Object, nil
}

func (k *machineAuditKubeLister) GetMachineConfiguration(ctx context.Context) (map[string]interface{}, error) {
	obj, err := k.dynamicClient.Resource(machineConfigurationGVR).Get(ctx, "cluster", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting MachineConfiguration/cluster: %w", err)
	}
	return obj.Object, nil
}

func (k *machineAuditKubeLister) ListMachineSets(ctx context.Context) ([]map[string]interface{}, error) {
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

func (k *machineAuditKubeLister) GetCoreOSBootImagesConfigMap(ctx context.Context) (map[string]interface{}, error) {
	obj, err := k.dynamicClient.Resource(configMapGVR).Namespace(mcoNamespace).Get(ctx, coreosBootimagesCM, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting ConfigMap/%s in %s: %w", coreosBootimagesCM, mcoNamespace, err)
	}
	return obj.Object, nil
}

func (k *machineAuditKubeLister) ListMasterNodes(ctx context.Context) ([]map[string]interface{}, error) {
	list, err := k.dynamicClient.Resource(nodeGVR).List(ctx, metav1.ListOptions{LabelSelector: masterNodeLabel})
	if err != nil {
		return nil, fmt.Errorf("listing master Nodes: %w", err)
	}
	result := make([]map[string]interface{}, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, item.Object)
	}
	return result, nil
}

func newMachineAuditKubeClientBuilder() machineAuditClientBuilderFunc {
	return func(kubeconfigPath string) (machineAuditLister, error) {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("machine-audit: building rest config: %w", err)
		}
		dynClient, err := dynamic.NewForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("machine-audit: creating dynamic client: %w", err)
		}
		return &machineAuditKubeLister{dynamicClient: dynClient}, nil
	}
}
