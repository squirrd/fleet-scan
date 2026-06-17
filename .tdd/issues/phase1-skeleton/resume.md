---
completed_phases: [setup]
current_phase: red
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
