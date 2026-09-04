# managed-namespaces collector

**Status:** Stable
**Date:** 2026-09-04
**Collector name:** `managed-namespaces`

## Purpose

Enumerates resources by kind in managed OpenShift namespaces, returning full raw JSON objects per item. Useful for inventory and compliance checks where the complete resource spec is needed. For lightweight attribution analysis (who owns a resource), use the `ns-attribution` collector instead — it extracts focused metadata fields and is significantly smaller per result.

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `patterns` | `openshift-*,kube-system,kube-public,default,redhat-*` | Comma-separated glob patterns for namespace names to include |
| `kinds` | see below | Comma-separated Kubernetes resource kinds to collect |

**Default kinds:** `Pods,Deployments,StatefulSets,DaemonSets,Jobs,CronJobs,Services,ConfigMaps,Secrets,NetworkPolicies,Routes,ServiceAccounts,Roles,RoleBindings`

Patterns use `filepath.Match` glob syntax (`*` matches within a name segment). Kind names are matched case-insensitively against the API server's resource names and Kind fields.

## Output Schema

```json
{
  "namespaces": [
    {
      "name": "openshift-monitoring",
      "resources": {
        "Pods": {
          "count": 12,
          "items": [
            { "apiVersion": "v1", "kind": "Pod", "metadata": { ... }, "spec": { ... }, "status": { ... } }
          ]
        },
        "Deployments": {
          "count": 3,
          "items": [ ... ]
        }
      }
    }
  ]
}
```

Each `items` entry is the full raw Kubernetes JSON for that resource. Kinds that return an error for a namespace are included with `count: 0` and an empty `items` array.

## Usage Example

```bash
# Default — all managed namespaces, all default kinds
fleet-scan scan \
  --search="managed='true' AND state='ready'" \
  --collector managed-namespaces \
  --concurrency 10 \
  --cluster-timeout 300

# Scoped — specific namespace pattern and kinds
fleet-scan scan \
  --search="managed='true' AND state='ready'" \
  --collector "managed-namespaces:patterns=kube-system,openshift-*;kinds=ServiceAccounts,RoleBindings" \
  --concurrency 10 \
  --cluster-timeout 300

RESULTS="output/$(ls -1 output/ | tail -1)/results.jsonl"
```

## Analysis Queries

### Resource count by kind across fleet

```bash
jq -r '
  .cluster_result["managed-namespaces"].data.namespaces[]?.resources
  | to_entries[]
  | [.key, (.value.count | tostring)]
  | @tsv
' "$RESULTS" | sort | awk '{sum[$1]+=$2} END {for(k in sum) print sum[k], k}' | sort -rn
```

### Namespaces with the most resources per cluster

```bash
jq -r '
  .cluster_metadata.name as $cluster |
  .cluster_result["managed-namespaces"].data.namespaces[]? |
  [$cluster, .name,
   ([.resources | to_entries[] | .value.count] | add | tostring)] | @tsv
' "$RESULTS" | sort -t$'\t' -k3 -rn | head -20
```

### All ServiceAccounts in kube-system

```bash
jq -r '
  .cluster_metadata.name as $cluster |
  .cluster_result["managed-namespaces"].data.namespaces[]?
  | select(.name == "kube-system")
  | .resources.ServiceAccounts.items[]?
  | [$cluster, .metadata.name] | @tsv
' "$RESULTS" | sort
```

### Export flat resource list to CSV

```bash
echo "cluster,namespace,kind,name" > managed-namespaces-summary.csv
jq -r '
  .cluster_metadata.name as $cluster |
  .cluster_result["managed-namespaces"].data.namespaces[]? |
  .name as $ns |
  .resources | to_entries[] |
  .key as $kind |
  .value.items[]? |
  [$cluster, $ns, $kind, .metadata.name] | @csv
' "$RESULTS" >> managed-namespaces-summary.csv
```

## Notes

- Output volume is large — full resource JSON per item per kind per namespace. Use `--collector ns-attribution` when you only need metadata fields.
- `ConfigMaps` and `Secrets` can contain sensitive data. Consider scoping `kinds` appropriately before sharing results.
- Kinds that the API server does not recognize or that lack list permissions are silently collected as `count: 0, items: []`.
