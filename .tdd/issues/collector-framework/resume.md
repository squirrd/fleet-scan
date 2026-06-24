---
completed_phases: [setup, red, green]
current_phase: close
---

## State

short_feat_name: collector-framework
feature_scope: internal
feature_summary: Define Collector interface, registry with init()-based auto-registration, and wire collectors into the runner loop so each cluster's record contains real collector results instead of stubs
source: roadmap-item
language: go
slices: ["interface-and-registry", "cli-validation", "runner-wiring"]
completed_slices: ["interface-and-registry", "cli-validation", "runner-wiring"]
current_slice: done
slice_criteria:
  interface-and-registry: Collector interface (Name/Configure/Run) and registry (Register/Get/List) exist with proper error handling for duplicate and unknown names
  cli-validation: ValidateCollectors rejects unknown collector names by checking the registry; Configure is called with parsed params before the run starts
  runner-wiring: Runner calls each configured collector per cluster, captures results (success with data or error with message) in ClusterRecord, handles nil backplane-login gracefully
acceptance_tests:
  interface-and-registry: internal/collector/registry_test.go::TestCollectorFramework_InterfaceAndRegistry_Acceptance
  cli-validation: internal/cli/flags_test.go::TestCollectorFramework_CliValidation_Acceptance
  runner-wiring: internal/runner/runner_test.go::TestCollectorFramework_RunnerWiring_Acceptance
units_added: 0
