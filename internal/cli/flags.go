package cli

// CollectorSpec represents a parsed --collector flag value.
// Format: name:key=val,key2=val2
type CollectorSpec struct {
	Name   string
	Params map[string]string
}

// ParseCollectorSpecs parses a slice of --collector flag values into CollectorSpecs.
// TODO: implement parsing logic.
func ParseCollectorSpecs(specs []string) ([]CollectorSpec, error) {
	return nil, nil
}

// ValidateCollectors checks that at least one collector is specified unless dry-run is true.
// TODO: implement validation logic.
func ValidateCollectors(collectors []CollectorSpec, dryRun bool) error {
	return nil
}
