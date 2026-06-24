package cli

import (
	"fmt"
	"strings"

	"github.com/squirrd/fleet-scan/internal/collector"
)

type CollectorSpec struct {
	Name   string
	Params map[string]string
}

func ParseCollectorSpecs(specs []string) ([]CollectorSpec, error) {
	var result []CollectorSpec
	for _, raw := range specs {
		if raw == "" {
			continue
		}

		name := raw
		params := make(map[string]string)

		if idx := strings.Index(raw, ":"); idx >= 0 {
			name = raw[:idx]
			paramStr := raw[idx+1:]
			for _, kv := range strings.Split(paramStr, ",") {
				eqIdx := strings.Index(kv, "=")
				if eqIdx < 0 {
					return nil, fmt.Errorf("invalid collector param %q: missing = separator", kv)
				}
				params[kv[:eqIdx]] = kv[eqIdx+1:]
			}
		}

		if name == "" {
			return nil, fmt.Errorf("invalid collector spec %q: name cannot be empty", raw)
		}

		result = append(result, CollectorSpec{Name: name, Params: params})
	}
	return result, nil
}

func ValidateCollectors(collectors []CollectorSpec, dryRun bool) error {
	if !dryRun && len(collectors) == 0 {
		return fmt.Errorf("at least one --collector is required (or use --dry-run)")
	}

	for _, spec := range collectors {
		c, err := collector.Get(spec.Name)
		if err != nil {
			return fmt.Errorf("unknown collector %q", spec.Name)
		}
		if err := c.Configure(spec.Params); err != nil {
			return fmt.Errorf("collector %q configuration error: %w", spec.Name, err)
		}
	}

	return nil
}
