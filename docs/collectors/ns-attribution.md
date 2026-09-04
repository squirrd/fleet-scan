# ns-attribution collector

**Status:** Stable
**Date:** 2026-09-04
**Collector name:** `ns-attribution`

## Purpose

Extracts focused ownership and metadata fields per resource in managed OpenShift namespaces. Returns a flat list of resources with slim metadata rather than full JSON — significantly smaller than `managed-namespaces` and optimised for attribution analysis: who created a resource, when, and via what manager. Designed for fleet-scale scans where full resource specs are unnecessary.

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `patterns` | `openshift-*,kube-system,kube-public,default,redhat-*` | Comma-separated glob patterns for namespace names to include |
| `kinds` | see below | Comma-separated Kubernetes resource kinds to collect |
| `fields` | see below | Comma-separated metadata fields to extract per resource |

**Default kinds:** `Pods,Deployments,StatefulSets,DaemonSets,Jobs,CronJobs,Services,ConfigMaps,Secrets,NetworkPolicies,Routes,ServiceAccounts,Roles,RoleBindings`

**Default fields:** `namespace,name,kind,apiVersion,creationTimestamp,ownerReferences,labels,annotations,managedFields`

Patterns use `filepath.Match` glob syntax. Kind names are matched case-insensitively. Fields `kind` and `apiVersion` are read from the top-level object; all other fields are read from `metadata`.

`managedFields` is automatically slimmed to `manager`, `operation`, `time`, `apiVersion`, and `subresource` — the bulk `fieldsV1` data is stripped.

## Output Schema

```json
{
  "resources": [
    {
      "namespace": "kube-system",
      "name": "argocd-sa",
      "kind": "ServiceAccount",
      "apiVersion": "v1",
      "creationTimestamp": "2024-03-15T10:22:00Z",
      "ownerReferences": [],
      "labels": { "app": "argocd" },
      "annotations": {},
      "managedFields": [
        {
          "manager": "kubectl-client-side-apply",
          "operation": "Update",
          "time": "2024-03-15T10:22:00Z"
        }
      ]
    }
  ]
}
```

All resources from all matched namespaces are collected into a single flat `resources` array. Fields not present on a resource are omitted from the output object.

## Usage Example

```bash
# Default — all managed namespaces, all default kinds, all default fields
fleet-scan scan \
  --search="managed='true' AND state='ready'" \
  --collector ns-attribution \
  --concurrency 20 \
  --cluster-timeout 300

# Scoped — specific kinds only
fleet-scan scan \
  --search="managed='true' AND state='ready'" \
  --collector "ns-attribution:patterns=kube-system;kinds=ServiceAccounts,RoleBindings" \
  --concurrency 20 \
  --cluster-timeout 300

# IBM fleet example (scope to known org)
IN_CLAUSE=$(ocm get '/api/accounts_mgmt/v1/subscriptions?search=organization_id%3D%27<org-id>%27%20AND%20status%3D%27Active%27&size=100&fields=cluster_id' \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
ids = [item['cluster_id'] for item in d.get('items', []) if item.get('cluster_id')]
print(','.join(f\"'{cid}'\" for cid in ids))
")

fleet-scan scan \
  --search="id in ($IN_CLAUSE)" \
  --collector "ns-attribution:kinds=Deployments,StatefulSets,DaemonSets,Services,Routes,CronJobs,NetworkPolicies" \
  --concurrency 20 \
  --cluster-timeout 900

RESULTS="output/$(ls -1 output/ | tail -1)/results.jsonl"
```

## Analysis Queries

### Resource count by kind across fleet

```bash
jq -r '.cluster_result["ns-attribution"].data.resources[]?.kind' "$RESULTS" | sort | uniq -c | sort -rn
```

### Resources by namespace across fleet

```bash
jq -r '.cluster_result["ns-attribution"].data.resources[]?.namespace' "$RESULTS" | sort | uniq -c | sort -rn | head -20
```

### Non-platform managed resources (likely customer-created)

Platform managers match: `kube-`, `openshift-`, `operator`, `controller`, `system`.

```bash
jq -r '
  .cluster_result["ns-attribution"].data.resources[]?
  | select(.managedFields[]?.manager | test("kube-|openshift-|operator|controller|system") | not)
  | [.namespace, .kind, .name, .managedFields[0].manager]
  | @tsv
' "$RESULTS" | sort | uniq -c | sort -rn | head -20
```

### Non-platform resources per cluster (count)

```bash
jq -r '
  [.cluster_metadata.name,
   ([.cluster_result["ns-attribution"].data.resources[]?
     | select(.managedFields[]?.manager | test("kube-|openshift-|operator|controller|system") | not)]
    | length | tostring)]
  | join(": ")
' "$RESULTS" | sort -t: -k2 -rn | head -20
```

### Resources in a specific namespace

```bash
jq -r '
  .cluster_metadata.name as $cluster |
  .cluster_result["ns-attribution"].data.resources[]?
  | select(.namespace == "kube-system")
  | [$cluster, .kind, .name, (.managedFields[0].manager // "unknown")]
  | @tsv
' "$RESULTS" | sort | uniq -c | sort -rn
```

### Export summary CSV

```bash
echo "cluster,namespace,kind,name,manager,created" > ns-attribution-summary.csv
jq -r '
  .cluster_metadata.name as $cluster |
  .cluster_result["ns-attribution"].data.resources[]? |
  [$cluster, .namespace, .kind, .name,
   (.managedFields[0].manager // "unknown"), .creationTimestamp] |
  @csv
' "$RESULTS" >> ns-attribution-summary.csv
```

## Comparison with managed-namespaces

| | `ns-attribution` | `managed-namespaces` |
|---|---|---|
| Output per resource | Selected metadata fields only | Full raw JSON |
| Output size | Small — suitable for fleet-scale scans | Large |
| Use case | Attribution: who owns this resource | Inventory: what is the full resource spec |
| `managedFields` | Slimmed (manager/operation/time only) | Full (includes fieldsV1 bulk data) |
| Results structure | Flat `resources` array | Nested by namespace and kind |
