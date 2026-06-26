package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

type kubeLister struct {
	dynamicClient   dynamic.Interface
	discoveryClient discovery.DiscoveryInterface
	gvrCache        map[string]schema.GroupVersionResource
}

func (k *kubeLister) ListNamespaces(ctx context.Context) ([]string, error) {
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	list, err := k.dynamicClient.Resource(nsGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing namespaces: %w", err)
	}

	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.GetName())
	}
	return names, nil
}

func (k *kubeLister) ListResources(ctx context.Context, namespace, kind string) ([]json.RawMessage, error) {
	gvr, err := k.resolveGVR(kind)
	if err != nil {
		return nil, err
	}

	list, err := k.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing %s in %s: %w", kind, namespace, err)
	}

	items := make([]json.RawMessage, 0, len(list.Items))
	for _, item := range list.Items {
		raw, err := item.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("marshalling %s item: %w", kind, err)
		}
		items = append(items, json.RawMessage(raw))
	}
	return items, nil
}

func (k *kubeLister) resolveGVR(kind string) (schema.GroupVersionResource, error) {
	key := strings.ToLower(kind)
	if gvr, ok := k.gvrCache[key]; ok {
		return gvr, nil
	}

	apiResourceLists, err := k.discoveryClient.ServerPreferredResources()
	// Process partial results even on error — standard client-go pattern.

	for _, list := range apiResourceLists {
		if list == nil {
			continue
		}
		gv, parseErr := schema.ParseGroupVersion(list.GroupVersion)
		if parseErr != nil {
			continue
		}
		for _, resource := range list.APIResources {
			if strings.Contains(resource.Name, "/") {
				continue
			}
			if strings.EqualFold(resource.Name, kind) || strings.EqualFold(resource.Kind, kind) {
				gvr := schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: resource.Name,
				}
				k.gvrCache[key] = gvr
				return gvr, nil
			}
		}
	}

	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("resolving GVR for %q: discovery error: %w", kind, err)
	}
	return schema.GroupVersionResource{}, fmt.Errorf("resolving GVR for %q: resource not found", kind)
}

func newKubeClientBuilder() clientBuilderFunc {
	return func(kubeconfigPath string) (namespaceLister, error) {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("building rest config: %w", err)
		}

		dynClient, err := dynamic.NewForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("creating dynamic client: %w", err)
		}

		discoClient, err := discovery.NewDiscoveryClientForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("creating discovery client: %w", err)
		}

		return &kubeLister{
			dynamicClient:   dynClient,
			discoveryClient: discoClient,
			gvrCache:        make(map[string]schema.GroupVersionResource),
		}, nil
	}
}
