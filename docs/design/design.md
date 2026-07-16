# fleet-scan — Design Document

**Status:** Approved
**Date:** 2026-06-16
**Repo:** github.com/squirrd/fleet-scan
**Location:** /Users/dsquirre/Repos/gh-squirrd/fleet-scan

## Purpose

Batch tool for scanning a fleet of OpenShift clusters via OCM API. Filters clusters by search criteria, logs into each via backplane, runs pluggable collectors, and outputs structured JSONL results.

## Decisions

| Decision | Answer |
|----------|--------|
| **Name** | `fleet-scan` |
| **Language** | Go 1.25 |
| **Module** | `github.com/squirrd/fleet-scan` |
| **CLI framework** | Cobra |
| **OCM auth** | Offline/refresh token via `OCM_TOKEN` env var, fallback to `refresh_token` from `~/.config/ocm/ocm.json`. No client credentials support. |
| **OCM SDK** | `openshift-online/ocm-sdk-go` |
| **Search filter** | Raw OCM search string via `--search` flag |
| **Cluster login** | `ocm backplane login` via shell exec, isolated `KUBECONFIG` per cluster |
| **Cluster interaction** | `client-go` for collectors |
| **Collectors** | Go interface, `init()` auto-registration, per-collector params via `--collector=name:key=val;key2=val2` |
| **Multiple collectors** | Supported, results keyed by collector name |
| **Concurrency** | Sequential by default (`--concurrency=1`), architected for parallelism with isolated kubeconfigs |
| **Error handling** | Skip and continue, errors captured per-cluster per-collector |
| **Backplane failure** | Skips all collectors for that cluster |
| **Timeouts** | `--cluster-timeout` defaulting to 120s, graceful shutdown on SIGINT |
| **Resume** | `--resume=<run-dir>`, re-attempts failed and unattempted clusters |
| **Output dir** | `--output-dir` defaulting to `./output/` |
| **Output format** | Directory per run, `results.jsonl` + `meta.json` |
| **Metadata fields** | Hardcoded default set (id, name, external_id, product, cloud_provider, region, state, health_state, openshift_version, multi_az, managed, creation_timestamp) |
| **Dry run** | `--dry-run` flag, runs filter only, reports cluster count |
| **Logging** | `slog`, progress counter by default, `--verbose` and `--debug` for more |

## Output Structure

### Run directory
```
output/2026-06-16T143022-abc123/
├── results.jsonl
└── meta.json
```

### JSONL record format
```json
{
  "cluster_search": {"search": "product.id = 'rosa'"},
  "cluster_metadata": {
    "id": "1q2w3e4r5t",
    "name": "my-prod-cluster",
    "external_id": "abc-123-def",
    "product": "rosa",
    "cloud_provider": "aws",
    "region": "us-east-1",
    "state": "ready",
    "health_state": "healthy",
    "openshift_version": "4.16.3",
    "multi_az": true,
    "managed": true,
    "creation_timestamp": "2025-01-15T10:30:00Z"
  },
  "cluster_result": {
    "managed-namespaces": {"status": "ok", "data": { ... }},
    "high-restarts": {"status": "error", "error": "timeout after 120s"}
  }
}
```

### meta.json format
```json
{
  "run_id": "2026-06-16T143022-abc123",
  "status": "completed",
  "search": "product.id = 'rosa' and state = 'ready'",
  "collectors": ["managed-namespaces", "high-restarts:threshold=10"],
  "cluster_timeout_seconds": 120,
  "started_at": "2026-06-16T14:30:22Z",
  "finished_at": "2026-06-16T15:42:18Z",
  "duration_seconds": 4316,
  "clusters_total": 1500,
  "clusters_succeeded": 1487,
  "clusters_failed": 13,
  "clusters_skipped": 0
}
```

## Collector Interface

```go
type Collector interface {
    Name() string
    Configure(params map[string]string) error
    Run(ctx context.Context, clusterID string, kubeconfigPath string) (json.RawMessage, error)
}
```

Registration via `init()` auto-registration pattern.

## First Collector: managed-namespaces

Enumerates resources in ROSA-managed namespaces to detect customer-added or modified resources.

**Default namespace patterns:** `openshift-*`, `kube-system`, `kube-public`, `default`, `redhat-*`

**Default resource kinds:** Pods, Deployments, StatefulSets, DaemonSets, Jobs, CronJobs, Services, ConfigMaps, Secrets, NetworkPolicies, Routes, ServiceAccounts, Roles, RoleBindings

**Params:** `patterns` (namespace patterns), `kinds` (resource kinds to enumerate)

## CLI Usage Examples

```bash
# Basic scan
fleet-scan scan --search="product.id = 'rosa' and state = 'ready'" --collector=managed-namespaces

# Dry run
fleet-scan scan --search="product.id = 'rosa'" --collector=managed-namespaces --dry-run

# Multiple collectors with params
fleet-scan scan --search="product.id = 'rosa'" \
  --collector=managed-namespaces \
  --collector=high-restarts:threshold=10

# Resume interrupted run
fleet-scan scan --resume=output/2026-06-16T143022-abc123/

# Verbose output
fleet-scan scan --search="product.id = 'rosa'" --collector=managed-namespaces --verbose

# Custom output directory
fleet-scan scan --search="product.id = 'rosa'" --collector=managed-namespaces --output-dir=/tmp/investigation/
```

## Future Enhancements (noted, not built)
- `--metadata-fields` flag to customize cluster metadata extraction
