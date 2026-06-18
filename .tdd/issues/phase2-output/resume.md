---
completed_phases: [setup, red, green]
current_phase: close
---

## State

short_feat_name: phase2-output
feature_scope: cli-only
feature_summary: Phase 2 — Output layer + iteration loop. JSONL writer, meta.json, run directories, resume support, per-cluster iteration loop with stub results.
source: ad-hoc
jira_key: 
slices: ["output-types", "jsonl-writer", "resume-loader", "runner-loop", "cli-wiring"]
completed_slices: ["output-types", "jsonl-writer", "resume-loader", "runner-loop", "cli-wiring"]
current_slice: done
units_added: 0
slice_criteria:
  output-types: ClusterRecord, RunMeta, and CollectorResult structs are defined in internal/output/types.go and serialize to JSON with the expected schema (nested ClusterMetadata, CollectorResult envelope with Status/Data/Error, RunMeta with all required fields).
  jsonl-writer: Writer creates a timestamped run directory (YYYY-MM-DDTHHMMSS format, no random suffix), writes meta.json at start with status "running", writes JSONL records with flush-per-line via WriteRecord() using direct os.File.Write(), and overwrites meta.json at finalization with final status and counts.
  resume-loader: LoadCompletedSet() reads an existing results.jsonl, extracts cluster IDs of succeeded records, and returns a set for filtering. Handles corrupt lines and empty files gracefully.
  runner-loop: runner.Run(ctx, clusters, writer) iterates clusters, writes stub ClusterRecord per cluster with empty cluster_result, prints [N/Total] progress to stderr, respects context cancellation, and uses --cluster-timeout per cluster via context.WithTimeout.
  cli-wiring: scan subcommand wires runner + writer for non-dry-run mode. Adds --resume flag taking full path to run directory. --resume and --search/--collector are mutually exclusive (error if combined). Resume re-runs search from meta.json, subtracts succeeded clusters, and appends to the same directory. Adds resumed_at field to meta.json for resumed runs.

## Acceptance Tests

| Slice | Test File | Test Function(s) | RED Failure Reason |
|-------|-----------|-------------------|--------------------|
| output-types | internal/output/types_test.go | TestPhase2Output_OutputTypes_Acceptance | undefined: CollectorResult, ClusterRecord, RunMeta |
| jsonl-writer | internal/output/writer_test.go | TestPhase2Output_JsonlWriter_Acceptance | undefined: RunMeta, NewWriter, ClusterRecord, CollectorResult |
| resume-loader | internal/output/resume_test.go | TestPhase2Output_ResumeLoader_Acceptance | undefined: ClusterRecord, CollectorResult, LoadCompletedSet |
| runner-loop | internal/runner/runner_test.go | TestPhase2Output_RunnerLoop_Acceptance | broken import: internal/output package not yet created |
| cli-wiring | internal/cli/cli_integration_test.go | TestPhase2Output_CliWiring_Acceptance | broken import: internal/output package not yet created |

## Design Decisions

1. Run directory name: YYYY-MM-DDTHHMMSS, no random suffix
2. JSONL records: cluster_metadata + cluster_result only (no cluster_search — search lives in meta.json)
3. Stub results: "cluster_result": {} (empty object)
4. Writer: direct os.File.Write() per record, no bufio, no fsync
5. meta.json: written at start with status "running", overwritten at end with final status
6. Resume: re-runs search from meta.json, subtracts succeeded clusters, appends to same directory
7. --resume + --search/--collector: mutually exclusive, error if combined
8. --resume: takes full path to run directory
9. Progress: [N/Total] on stderr, N increments on start, one line per cluster
10. clusters_total: current search count; success/failed/skipped cumulative across JSONL
11. meta.json collectors: raw spec strings
12. CLI/runner boundary: CLI wires components, runner.Run(ctx, clusters, writer) owns iteration
13. --cluster-timeout: wired with context.WithTimeout per cluster from Phase 2
14. ClusterRecord: nested ClusterMetadata field (not embedded), ClusterResult is map[string]CollectorResult
15. CollectorResult struct: Status/Data/Error envelope in output/types.go
16. duration_seconds: this execution's wall clock only
17. resumed_at field added to meta.json for resumed runs
