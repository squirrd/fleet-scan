# fleet-scan

A CLI tool for batch-scanning fleets of OpenShift clusters via the OCM API. Filters clusters by search criteria, logs into each via backplane, runs pluggable collectors, and outputs structured JSONL results.

> Development happens on [GitHub](https://github.com/squirrd/fleet-scan). This project is also mirrored read-only to GitLab for Red Hat internal visibility — please open issues and PRs on GitHub.

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

### boot-images

Collects per-MachineSet AMI inventory from AWS clusters to enable fleet-wide boot image risk tiering. See `docs/collectors/boot-images.md`.

**Parameters:** None.

**Example:**

```bash
$ fleet-scan scan \
  --search "managed='true' AND cloud_provider.id='aws'" \
  --collector boot-images
```

### machine-audit

Collects the raw objects needed to assess CPMS (ControlPlaneMachineSet) presence and boot-image drift on masters (ClusterVersion, Infrastructure, ControlPlaneMachineSet, MachineConfiguration, MachineSets, the `coreos-bootimages` ConfigMap, and master Nodes), without pre-computing a verdict. See `docs/collectors/machine-audit.md`.

**Parameters:** None.

**Example:**

```bash
$ fleet-scan scan \
  --search "managed='true' AND (cloud_provider.id='aws' OR cloud_provider.id='gcp') AND hypershift.enabled='false'" \
  --collector machine-audit
```

### cco-tight-loop

Detects OCPBUGS-132683 — the GCP Cloud Credential Operator tight reconcile loop on clusters running `credentialsMode: Manual` with Workload Identity Federation enabled. Read-only, no elevated access required.

**Parameters:**

| Parameter | Default | Description |
|-----------|---------|-------------|
| `window` | `30m` | Go duration string — how far back to inspect CCO logs |
| `threshold` | `5` | Minimum resyncs-per-CredentialsRequest within the window to flag a loop |

**Example:**

```bash
$ fleet-scan scan \
  --search "managed='true' AND cloud_provider.id='gcp'" \
  --collector "cco-tight-loop:window=1h;threshold=10"
```
