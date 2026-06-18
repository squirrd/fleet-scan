# fleet-scan

Batch tool for scanning a fleet of OpenShift clusters via OCM API, running pluggable collectors against each, and producing structured JSONL results.

## Language

**Search**:
The OCM API query string used to select clusters from the fleet. Passed via `--search` flag, forwarded directly to the OCM clusters endpoint `search` parameter.
_Avoid_: Filter, query, selector

**Cluster Metadata**:
The fixed set of fields extracted from each OCM cluster object and included in every JSONL result record.
_Avoid_: Cluster info, cluster data

**Collector**:
A pluggable unit of work that runs against a single cluster and returns structured JSON data. Registered via `init()` auto-registration.
_Avoid_: Plugin, scanner, probe

**Collector Spec**:
The CLI syntax for selecting and parameterizing a collector: `name:key=val,key2=val2`. Parsed from `--collector` flags.
_Avoid_: Collector config, collector arg

**Record**:
A single cluster's output within a Run — one line in `results.jsonl`, containing cluster metadata and collector results.
_Avoid_: Result, entry, row

**Run**:
A single execution of fleet-scan, identified by its timestamp and materialised as a Run Directory containing `results.jsonl` and `meta.json`.
_Avoid_: Scan, job, execution

**Run Directory**:
The filesystem directory holding a Run's output. Located under `--output-dir`, named by timestamp (`YYYY-MM-DDTHHMMSS`).
_Avoid_: Output folder, results dir
