package collector

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// nsAttrKubeLister implements attrNamespaceLister using Kubernetes dynamic+discovery clients.
type nsAttrKubeLister struct {
	dynamicClient   dynamic.Interface
	discoveryClient discovery.DiscoveryInterface
}

func (k *nsAttrKubeLister) ListNamespaces(ctx context.Context) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}

func (k *nsAttrKubeLister) ListResources(ctx context.Context, namespace, kind string) ([]json.RawMessage, error) {
	return nil, fmt.Errorf("not implemented")
}

// newNsAttrKubeClientBuilder returns an attrClientBuilderFunc that builds
// real Kubernetes dynamic+discovery clients from a kubeconfig path.
func newNsAttrKubeClientBuilder() attrClientBuilderFunc {
	return func(kubeconfigPath string) (attrNamespaceLister, error) {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("ns-attribution: building rest config: %w", err)
		}

		dynClient, err := dynamic.NewForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("ns-attribution: creating dynamic client: %w", err)
		}

		discoClient, err := discovery.NewDiscoveryClientForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("ns-attribution: creating discovery client: %w", err)
		}

		return &nsAttrKubeLister{
			dynamicClient:   dynClient,
			discoveryClient: discoClient,
		}, nil
	}
}
