# boot-images collector

**Status:** Draft
**Date:** 2026-09-04
**Collector name:** `boot-images`

## Purpose

Collects per-MachineSet AMI inventory from AWS clusters to enable fleet-wide boot image risk tiering. Targets the failure mode where MachineSets referencing stale RHCOS AMIs (≤ 4.11) cause new node provisioning to fail on clusters ≥ 4.20 due to `ClusterImagePolicy` parsing incompatibility.

AMI IDs cannot be resolved to OCP versions from inside the cluster. This collector produces the raw AMI inventory; version resolution is a separate out-of-band step using `aws ec2 describe-images` (see [AMI → OCP version resolution](#ami--ocp-version-resolution)).

## Parameters

None. `--collector boot-images` takes no key=val parameters.

## Kubernetes Resources Queried

| Resource | GVK | Scope | Fields extracted |
|---|---|---|---|
| `ClusterVersion/version` | `config.openshift.io/v1` | cluster-scoped | `status.history[0].version` |
| `MachineConfiguration/cluster` | `machine.openshift.io/v1` | cluster-scoped | `spec.managedBootImages.machineManagers` |
| `MachineSet` (list) | `machine.openshift.io/v1beta1` | namespace: `openshift-machine-api` | name, replicas, readyReplicas, `spec.template.spec.providerSpec.value.ami.id` |

## Output Schema

```json
{
  "cluster_version": "4.22.5",
  "boot_image_mgmt_enabled": false,
  "machine_sets": [
    {
      "name": "cluster-abc123-worker-us-east-1a",
      "ami_id": "ami-0abc12345",
      "replicas": 3,
      "ready_replicas": 3
    }
  ]
}
```

**`boot_image_mgmt_enabled` values:**

| Value | Meaning |
|---|---|
| `true` | `spec.managedBootImages` is null/unset — MCO manages boot images |
| `false` | `spec.managedBootImages.machineManagers` is `[]` — explicitly disabled (ROSA Classic) |
| `null` | `MachineConfiguration/cluster` not found or unreadable |

## Recommended Search Query

```bash
fleet-scan scan \
  --search="managed='true' AND state='ready' AND cloud_provider.id='aws' \
    AND (product.id='rosa' OR product.id='osd') \
    AND hypershift.enabled='false'" \
  --collector boot-images \
  --concurrency 20 \
  --cluster-timeout 120
```

> **Note:** `hypershift.enabled` is not yet confirmed as a valid OCM search field. Test with `--dry-run` before running the full scan. If it returns an error, use the two-step fallback below to exclude HCP clusters.

### Fallback: exclude HCP via two-step

```bash
# Step 1: get IDs of HCP clusters
HCP_IDS=$(ocm get "/api/clusters_mgmt/v1/clusters?search=$(python3 -c \
  "import urllib.parse; print(urllib.parse.quote(\"managed='true' AND state='ready' AND cloud_provider.id='aws' AND (product.id='rosa' OR product.id='osd') AND hypershift.enabled='true'\"))")&fields=id&size=500" \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
ids = [item['id'] for item in d.get('items', []) if item.get('id')]
print(','.join(f\"'{cid}'\" for cid in ids))
")

# Step 2: scan non-HCP clusters
fleet-scan scan \
  --search="managed='true' AND state='ready' AND cloud_provider.id='aws' \
    AND (product.id='rosa' OR product.id='osd') \
    AND id not in (${HCP_IDS})" \
  --collector boot-images \
  --concurrency 20 \
  --cluster-timeout 120
```

## Usage Example

```bash
# 1. Dry run — verify query and cluster count
fleet-scan scan \
  --search="managed='true' AND state='ready' AND cloud_provider.id='aws' \
    AND (product.id='rosa' OR product.id='osd') \
    AND hypershift.enabled='false'" \
  --dry-run

# 2. Full scan
fleet-scan scan \
  --search="managed='true' AND state='ready' AND cloud_provider.id='aws' \
    AND (product.id='rosa' OR product.id='osd') \
    AND hypershift.enabled='false'" \
  --collector boot-images \
  --concurrency 20 \
  --cluster-timeout 120

# 3. Point to results
RESULTS="output/$(ls -1 output/ | tail -1)/results.jsonl"
```

## Analysis Queries

All queries use `$RESULTS` set to the `results.jsonl` path.

### Status breakdown

```bash
jq -r '.cluster_result["boot-images"].status' "$RESULTS" | sort | uniq -c | sort -rn
```

### All MachineSet AMI IDs (flat list for AWS resolution)

```bash
jq -r '
  .cluster_metadata.name as $cluster |
  .cluster_metadata.region as $region |
  .cluster_result["boot-images"].data.machine_sets[]? |
  [$cluster, $region, .name, .ami_id] | @tsv
' "$RESULTS"
```

### Clusters with boot image management disabled

```bash
jq -r '
  select(.cluster_result["boot-images"].data.boot_image_mgmt_enabled == false)
  | [.cluster_metadata.name, .cluster_metadata.product,
     .cluster_result["boot-images"].data.cluster_version]
  | @tsv
' "$RESULTS"
```

### Summary CSV (for AMI resolution handoff)

```bash
echo "cluster,product,region,cluster_version,boot_mgmt_enabled,machineset,ami_id,replicas" > boot-images-summary.csv
jq -r '
  .cluster_metadata.name as $cluster |
  .cluster_metadata.product as $product |
  .cluster_metadata.region as $region |
  (.cluster_result["boot-images"].data | . as $d |
    .machine_sets[]? |
    [$cluster, $product, $region, $d.cluster_version,
     ($d.boot_image_mgmt_enabled | tostring), .name, .ami_id, (.replicas | tostring)]
  ) | @csv
' "$RESULTS" >> boot-images-summary.csv
```

### After AMI resolution — filter by risk tier

Once AMI IDs have been resolved to OCP versions (see below), join on AMI ID to apply risk tiering:

```bash
# Critical: cluster >= 4.20, MachineSet AMI <= 4.11
# Requires AMI version data merged into results — apply after aws ec2 describe-images step
```

## AMI → OCP Version Resolution

AMI IDs must be resolved to OCP versions using the AWS CLI after the scan completes. Results span multiple regions — run once per region with the relevant AMI subset.

```bash
# Extract unique AMI IDs per region from the summary CSV, then resolve:
aws ec2 describe-images \
  --region <region> \
  --image-ids <space-separated ami-ids> \
  --query 'Images[].{AMI:ImageId,Name:Name}' \
  --output text \
| awk '{
    match($2, /rhcos-([0-9]+)/, a)
    v = a[1]; sub(/^4/, "4.", v)
    print $1 "\t" v
  }'
# Output: ami-xxxxxxxx   4.9
```

See also: `raw/investigations/old-boot-images-check-steps.md` for the full procedure.

## Risk Tiering Reference

Applies per MachineSet, not per cluster. A cluster can span multiple tiers.

| Tier | Cluster version | MachineSet AMI version | Status |
|---|---|---|---|
| Safe | Any | ≥ 4.13 | RHEL 9, parses modern ClusterImagePolicies |
| Watch | < 4.20 | ≤ 4.12 (RHEL 8) | Not failing today; RHEL 8 will block 5.0 upgrade path |
| Watch | < 4.20 | ≤ 4.11 | Not failing today; becomes Critical on upgrade to 4.20 |
| Critical | ≥ 4.20 | ≤ 4.11 | New node provisioning fails — old AMI cannot parse ClusterImagePolicy |

Source: `raw/investigations/old-boot-images-cluster-upgrade-failures.md`

## Implementation Notes

Follow the `ns-attribution` collector pattern:

- **Files:** `internal/collector/boot_images.go`, `internal/collector/boot_images_kube.go`
- **Registration:** `init()` registers under name `"boot-images"`
- **Client pattern:** define a `bootImagesLister` interface; production impl in `boot_images_kube.go`; mock in tests
- **MachineConfiguration** is cluster-scoped — call dynamic client with namespace `""`
- **AMI path** in MachineSet JSON: `spec.template.spec.providerSpec.value.ami.id`
- **`boot_image_mgmt_enabled` logic:**
  - MachineConfiguration not found → `null`
  - `spec.managedBootImages` absent or null → `true` (MCO-managed)
  - `spec.managedBootImages.machineManagers` is empty array `[]` → `false` (disabled)
- **ClusterVersion** is cluster-scoped (`config.openshift.io/v1`); use `status.history` array, take first entry with `state: "Completed"` for the current version
- **TDD:** write failing tests before implementation per project convention (see `CLAUDE.md`)
