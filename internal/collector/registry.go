package collector

import (
	"fmt"
	"sort"
	"sync"
)

// registry holds collector factories keyed by name.
var (
	mu        sync.Mutex
	factories = map[string]func() Collector{}
)

// Register adds a collector factory to the global registry.
// It is intended to be called from init() functions.
// It panics if a factory with the same name is already registered.
func Register(name string, factory func() Collector) {
	mu.Lock()
	defer mu.Unlock()

	if _, exists := factories[name]; exists {
		panic(fmt.Sprintf("collector: duplicate registration for %q", name))
	}
	factories[name] = factory
}

// Get returns a new Collector instance from the registered factory.
// It returns an error if no factory is registered for the given name.
func Get(name string) (Collector, error) {
	mu.Lock()
	defer mu.Unlock()

	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("collector: unknown collector %q", name)
	}
	return factory(), nil
}

// List returns the names of all registered collectors in sorted order.
func List() []string {
	mu.Lock()
	defer mu.Unlock()

	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResetRegistry clears all registered factories. Exported for use by
// tests in other packages.
func ResetRegistry() {
	resetRegistry()
}

// resetRegistry clears all registered factories. Used in tests
// within this package.
func resetRegistry() {
	mu.Lock()
	defer mu.Unlock()

	factories = map[string]func() Collector{}
}
