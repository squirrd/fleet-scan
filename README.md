# fleet-scan

A CLI tool for batch-scanning fleets of OpenShift clusters via the OCM API. Filters clusters by search criteria, logs into each via backplane, runs pluggable collectors, and outputs structured JSONL results.

## Prerequisites

- Go 1.25+
- [OCM CLI](https://github.com/openshift-online/ocm-cli) installed and configured
- [Backplane CLI](https://github.com/openshift/backplane-cli) installed
- `OCM_TOKEN` environment variable set, or a valid token in `~/.config/ocm/ocm.json`

## Build
To make the fleet-scan CLI available locally, clone this repo locally.
```bash
$ cd <location-for-fleetscan-repo>
$ git clone git@github.com:squirrd/fleet-scan.git
$ cd fleet-scan
```
Then build and deploy

```bash
$ make deploy
go build -o bin/fleet-scan ./cmd/fleet-scan
mkdir -p ~/bin
ln -sf ~/Repos/fleet-scan/bin/fleet-scan ~/bin/fleet-scan

# Built version
bin/fleet-scan version
fleet-scan 0.1.5

# Locally available version
fleet-scan version
fleet-scan 0.1.5
```

OR
```bash
$ make build
go build -o bin/fleet-scan ./cmd/fleet-scan
```
Then add the executable to your PATH environment variable or move `bin/fleet-scan` to a suitable executable location

## Usage

```bash
# Scan all managed clusters, collecting namespace info with concurrency of 5
$ fleet-scan scan \
  --search "managed='true'" \
  --collector "managed-namespaces:patterns=openshift-*;kinds=Pods,Deployments" \
  --concurrency 5

# Dry run — just count matching clusters
$ fleet-scan scan --search "managed='true'" --dry-run

# Resume an interrupted run
$ fleet-scan scan --resume output/2024-06-15T143000/
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--search` | (required) | OCM search query string |
| `--collector` | (required) | Collector spec in `name:key=val;key2=val2` format (repeatable) |
| `--concurrency` | `1` | Number of clusters to process concurrently |
| `--cluster-timeout` | `120` | Per-cluster timeout in seconds |
| `--output-dir` | `./output/` | Output directory for results |
| `--resume` | | Resume a previous run from a run directory path |
| `--dry-run` | `false` | Report matching cluster count without scanning |

## Output Format

Each scan run creates a timestamped directory under the output directory containing:

- **results.jsonl** -- One JSON record per cluster, with metadata and collector results. Each line is a complete JSON object:
  ```json
  {"cluster_metadata":{"id":"abc123","name":"my-cluster",...},"results":{"managed-namespaces":{"status":"success","data":{...}}}}
  ```

- **meta.json** -- Run metadata including status, search query, collectors used, timing, and outcome counts:
  ```json
  {
    "status": "completed",
    "search": "managed='true'",
    "collectors": ["managed-namespaces:patterns=openshift-*"],
    "clusters_total": 100,
    "clusters_success": 95,
    "clusters_failed": 3,
    "clusters_skipped": 2,
    "duration_seconds": 120.5
  }
  ```

## Available Collectors

### managed-namespaces

Enumerates resources in managed namespaces across a cluster.

**Parameters:**

| Parameter | Default | Description |
|-----------|---------|-------------|
| `patterns` | `openshift-*,kube-system,kube-public,default,redhat-*` | Comma-separated namespace patterns to match |
| `kinds` | `Pods,Deployments,StatefulSets,DaemonSets,Jobs,CronJobs,Services,ConfigMaps,Secrets,NetworkPolicies,Routes,ServiceAccounts,Roles,RoleBindings` | Comma-separated resource kinds to enumerate |

**Example:**

```bash
$ fleet-scan scan \
  --search "managed='true'" \
  --collector "managed-namespaces:patterns=openshift-*;kinds=Pods,Deployments"
```

### ns-attribution

Extracts focused per-resource metadata from managed namespaces — ownership, labels, annotations, and selected fields — as a flat list.

**Parameters:**

| Parameter | Default | Description |
|-----------|---------|-------------|
| `patterns` | `openshift-*,kube-system,kube-public,default,redhat-*` | Comma-separated namespace patterns to match |
| `kinds` | `Pods,Deployments,StatefulSets,DaemonSets,Jobs,CronJobs,Services,ConfigMaps,Secrets,NetworkPolicies,Routes,ServiceAccounts,Roles,RoleBindings` | Comma-separated resource kinds to enumerate |
| `fields` | `namespace,name,kind,apiVersion,creationTimestamp,ownerReferences,labels,annotations,managedFields` | Comma-separated metadata fields to extract per resource |

**Example:**

```bash
$ fleet-scan scan \
  --search "managed='true'" \
  --collector "ns-attribution:patterns=openshift-*;kinds=Deployments,Pods;fields=namespace,name,labels,ownerReferences"
```
