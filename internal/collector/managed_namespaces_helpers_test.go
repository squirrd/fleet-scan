package collector

import (
	"context"
	"encoding/json"
	"sort"
)

// fakeManagedNSClientBuilder creates a clientBuilderFunc for testing.
// It takes a map of namespace -> kind -> item names and returns a builder
// that produces a fake lister.
func fakeManagedNSClientBuilder(data map[string]map[string][]string) clientBuilderFunc {
	return func(kubeconfigPath string) (namespaceLister, error) {
		return &fakeLister{data: data}, nil
	}
}

// fakeLister implements namespaceLister for testing.
type fakeLister struct {
	data map[string]map[string][]string
}

func (f *fakeLister) ListNamespaces(ctx context.Context) ([]string, error) {
	names := make([]string, 0, len(f.data))
	for ns := range f.data {
		names = append(names, ns)
	}
	sort.Strings(names)
	return names, nil
}

func (f *fakeLister) ListResources(ctx context.Context, namespace, kind string) ([]json.RawMessage, error) {
	kinds, ok := f.data[namespace]
	if !ok {
		return nil, nil
	}
	itemNames, ok := kinds[kind]
	if !ok {
		return nil, nil
	}
	items := make([]json.RawMessage, len(itemNames))
	for i, name := range itemNames {
		item := map[string]string{"name": name}
		data, _ := json.Marshal(item)
		items[i] = data
	}
	return items, nil
}
