package collector

import (
	"context"
	"encoding/json"
	"sort"
)

// fakeNsAttrClientBuilder creates an attrClientBuilderFunc for testing.
// It takes a map of namespace -> kind -> full JSON objects and returns a
// builder that produces a fake lister returning those objects verbatim.
func fakeNsAttrClientBuilder(data map[string]map[string][]json.RawMessage) attrClientBuilderFunc {
	return func(kubeconfigPath string) (attrNamespaceLister, error) {
		return &fakeAttrLister{data: data}, nil
	}
}

// fakeAttrLister implements attrNamespaceLister for testing.
type fakeAttrLister struct {
	data map[string]map[string][]json.RawMessage
}

func (f *fakeAttrLister) ListNamespaces(ctx context.Context) ([]string, error) {
	names := make([]string, 0, len(f.data))
	for ns := range f.data {
		names = append(names, ns)
	}
	sort.Strings(names)
	return names, nil
}

func (f *fakeAttrLister) ListResources(ctx context.Context, namespace, kind string) ([]json.RawMessage, error) {
	kinds, ok := f.data[namespace]
	if !ok {
		return nil, nil
	}
	items, ok := kinds[kind]
	if !ok {
		return nil, nil
	}
	return items, nil
}
