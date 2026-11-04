# machine-audit collector

**Status:** Draft
**Date:** 2026-11-04
**Collector name:** `machine-audit`

## Purpose

Collects the raw objects needed to assess CPMS (ControlPlaneMachineSet) presence and boot-image drift on masters, without pre-computing a verdict. Returns full object bodies so later analysis can answer questions that weren't anticipated at collection time, rather than re-scanning the fleet.

Developed for `workspaces/cre/ami-boot-node-failures` — specifically the open question of master/CPMS boot-image exposure (see `node-types-and-mc-incident.md` and `tech-assessment.md` in that workspace). Complements the existing `boot-images` collector (which captures a condensed worker/infra MachineSet AMI inventory); this collector is the superset for control-plane drift analysis plus full object context.

## Parameters

None. `--collector machine-audit` takes no key=val parameters.

## Kubernetes Resources Queried

| Resource | GVK | Scope | Notes |
|---|---|---|---|
| `ClusterVersion/version` | `config.openshift.io/v1` | cluster-scoped | Full object — `status.history` (install/current version) and `status.conditions` (`Upgradeable`/`ClusterBootImageSkewError`) |
| `Infrastructure/cluster` | `config.openshift.io/v1` | cluster-scoped | Full object — provides `status.platformStatus.{aws,gcp}.region` needed to resolve expected boot image |
| `ControlPlaneMachineSet/cluster` | `machine.openshift.io/v1` | namespace: `openshift-machine-api` | Full object. **Not found is expected** on pre-4.12 / never-adopted clusters — not an error |
| `MachineConfiguration/cluster` | `machine.openshift.io/v1` | cluster-scoped | Full object — `spec.managedBootImages` (opt-in state for both workers and CPMS) |
| `MachineSet` (list) | `machine.openshift.io/v1beta1` | namespace: `openshift-machine-api` | Full objects — all worker/infra MachineSets |
| `ConfigMap/coreos-bootimages` | core/v1 | namespace: `openshift-machine-config-operator` | Full object — `data.stream` holds the expected boot image per platform/region for the release |
| `Node` (list, `node-role.kubernetes.io/master` label) | core/v1 | cluster-scoped | Full objects — `status.nodeInfo.osImage` is ground truth for what's actually booted; also usable to infer ignition era without reading the ignition secret |

No Secrets are read. Ignition version is inferred from `osImage` on master nodes (per `node-types-and-mc-incident.md`), not from decoding `master-user-data`.

## Output Schema

Raw objects only — no derived/summary fields. Analysis happens downstream against the full bodies.

```json
{
  "cluster_version": { "...": "full ClusterVersion object" },
  "infrastructure": { "...": "full Infrastructure object" },
  "cpms": { "...": "full ControlPlaneMachineSet object, or null if not found" },
  "machine_configuration": { "...": "full MachineConfiguration object" },
  "machine_sets": [ { "...": "full MachineSet object" } ],
  "coreos_bootimages": { "...": "full ConfigMap object" },
  "master_nodes": [ { "...": "full Node object" } ]
}
```

`cpms` is `null` (not an error) when the resource doesn't exist — this is itself the signal for "no CPMS / pre-4.12 / never adopted."

## Recommended Search Query

Extends the `boot-images` AWS-only scan to also cover GCP, since `tech-assessment.md` documents OSD-GCP as an unmitigated platform (ROSAENG-66166). HCP is excluded — CPMS and classic `MachineSet` don't apply to the HCP data plane (NodePool/CAPI-based instead), so this collector's schema doesn't fit there; HCP needs its own collector as a separate follow-up.

```bash
fleet-scan scan \
  --search="managed='true' AND state='ready' \
    AND (cloud_provider.id='aws' OR cloud_provider.id='gcp') \
    AND (product.id='rosa' OR product.id='osd') \
    AND hypershift.enabled='false'" \
  --collector machine-audit \
  --concurrency 20 \
  --cluster-timeout 120
```

> See `boot-images.md` for the two-step fallback if `hypershift.enabled` isn't a valid OCM search field.

## Usage Example

```bash
# 1. Dry run — verify query and cluster count
fleet-scan scan \
  --search="managed='true' AND state='ready' \
    AND (cloud_provider.id='aws' OR cloud_provider.id='gcp') \
    AND (product.id='rosa' OR product.id='osd') \
    AND hypershift.enabled='false'" \
  --dry-run

# 2. Full scan
fleet-scan scan \
  --search="managed='true' AND state='ready' \
    AND (cloud_provider.id='aws' OR cloud_provider.id='gcp') \
    AND (product.id='rosa' OR product.id='osd') \
    AND hypershift.enabled='false'" \
  --collector machine-audit \
  --concurrency 20 \
  --cluster-timeout 120

# 3. Point to results
RESULTS="output/$(ls -1 output/ | tail -1)/results.jsonl"
```

## Analysis Queries

All queries use `$RESULTS` set to the `results.jsonl` path.

### Status breakdown

```bash
jq -r '.cluster_result["machine-audit"].status' "$RESULTS" | sort | uniq -c | sort -rn
```

### CPMS presence

```bash
jq -r '
  [.cluster_metadata.name, .cluster_metadata.product,
   (.cluster_result["machine-audit"].data.cpms != null | tostring)]
  | @tsv
' "$RESULTS"
```

### Clusters with no CPMS (pre-4.12 / never adopted)

```bash
jq -r '
  select(.cluster_result["machine-audit"].data.cpms == null)
  | [.cluster_metadata.name, .cluster_metadata.product, .cluster_metadata.region]
  | @tsv
' "$RESULTS"
```

### CPMS AMI vs. worker AMI vs. booted master osImage (per cluster)

```bash
jq -r '
  .cluster_metadata.name as $cluster |
  .cluster_result["machine-audit"].data as $d |
  ($d.cpms.spec.template.machines_v1beta1_machine_openshift_io.spec.providerSpec.value.ami.id // "NO_CPMS") as $cpms_ami |
  ($d.machine_sets[0].spec.template.spec.providerSpec.value.ami.id // "NONE") as $worker_ami |
  [$d.master_nodes[]?.status.nodeInfo.osImage] as $booted |
  [$cluster, $cpms_ami, $worker_ami, ($booted | join(";"))]
  | @tsv
' "$RESULTS"
```

### ClusterBootImageSkewError present (direct enforcement signal)

```bash
jq -r '
  select(
    .cluster_result["machine-audit"].data.cluster_version.status.conditions[]?
    | select(.reason == "ClusterBootImageSkewError")
  )
  | .cluster_metadata.name
' "$RESULTS"
```

## Implementation Notes

Follow the `boot-images` collector pattern:

- **Files:** `internal/collector/machine_audit.go`, `internal/collector/machine_audit_kube.go`
- **Registration:** `init()` registers under name `"machine-audit"`
- **Client pattern:** define a `machineAuditLister` interface (mirrors `bootImagesLister`); production impl in `machine_audit_kube.go`; mock in tests
- **CPMS not-found handling:** `GetControlPlaneMachineSet` should return `(nil, nil)` on not-found (same convention as `GetMachineConfiguration` in `boot-images`), not an error — absence is the signal, not a failure
- **No field extraction/flattening** — unlike `boot-images`, this collector returns full object bodies as-is (`map[string]interface{}` passed straight through to JSON), except for the master node list which is filtered by label before inclusion
- **Master node filter:** `node-role.kubernetes.io/master` label selector
- **Infrastructure and ClusterVersion** are both cluster-scoped singletons (`config.openshift.io/v1`), fetched the same way `ClusterVersion` already is in `boot-images`
- **TDD:** write failing tests before implementation per project convention (see `CLAUDE.md`)

## Related

- `boot-images.md` — condensed worker/infra AMI inventory, run alongside this collector for cross-validation
- `workspaces/cre/ami-boot-node-failures/node-types-and-mc-incident.md` — confirmed CPMS template AMI matches live master AMI (closes the "does CPMS refresh at replacement time" question — it doesn't)
- `workspaces/cre/ami-boot-node-failures/tech-assessment.md` — platform-level MCO-MSBIC status, `ClusterBootImageSkewError` enforcement details
