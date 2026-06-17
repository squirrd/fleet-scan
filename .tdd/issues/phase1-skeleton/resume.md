---
completed_phases: [setup, red, green]
current_phase: close
---

## State

short_feat_name: phase1-skeleton
feature_scope: cli-only
feature_summary: Phase 1 of fleet-scan — Go module skeleton, Cobra CLI, OCM auth, paginated cluster listing, and --dry-run
source: ad-hoc
jira_key: 
slices: ["module-cli-scaffold", "collector-spec-parsing", "ocm-auth", "cluster-listing-metadata", "dry-run-integration"]
slice_criteria:
  module-cli-scaffold: go mod init, Makefile, cmd/fleet-scan/main.go, Cobra root + scan subcommand compile and fleet-scan scan --help prints usage
  collector-spec-parsing: ParseCollectorSpecs() parses name:key=val,key2=val2 syntax correctly; flag validation requires at least one --collector unless --dry-run
  ocm-auth: OCM token resolution from OCM_TOKEN env var then ~/.config/ocm/ocm.json refresh_token fallback; connection builder returns working OCM connection
  cluster-listing-metadata: 12-field ClusterMetadata struct, paginated cluster listing using response.Total(), OCM client behind interface per ADR-0001
  dry-run-integration: --dry-run wired end-to-end: authenticates to OCM, sends size=1 request, prints cluster count; full scan subcommand execution path

## Acceptance Tests

| Slice | Test File | Test Function(s) | RED Failure Reason |
|-------|-----------|-------------------|--------------------|
| module-cli-scaffold | internal/cli/cli_test.go | TestScanCommandHelp | scan subcommand not found on root command |
| collector-spec-parsing | internal/cli/flags_test.go | TestParseCollectorSpecs | returns 0 specs (nil) instead of parsed results; no errors for malformed input |
| collector-spec-parsing | internal/cli/flags_test.go | TestCollectorRequiredUnlessDryRun | returns nil instead of error when no collectors and not dry-run |
| ocm-auth | internal/ocm/auth_test.go | TestTokenResolution | returns empty string instead of token; returns nil instead of error |
| ocm-auth | internal/ocm/auth_test.go | TestParseOCMConfig | returns nil config and nil error instead of parsed config or errors |
| cluster-listing-metadata | internal/ocm/clusters_test.go | TestListClusters_Pagination | returns 0 clusters, 0 API calls made |
| cluster-listing-metadata | internal/ocm/clusters_test.go | TestClusterMetadata_Fields | ExtractClusterMetadata returns nil metadata |
| dry-run-integration | internal/cli/scan_integration_test.go | TestDryRunPrintsClusterCount | scan subcommand missing, --search flag unknown |
