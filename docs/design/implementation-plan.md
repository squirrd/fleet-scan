# fleet-scan Implementation Plan

## Context

Building `fleet-scan`, a Go CLI tool that scans a fleet of OpenShift clusters via the OCM API. It filters clusters by search criteria, logs into each via backplane, runs pluggable collectors against each cluster, and outputs structured JSONL results. The design doc is at `docs/design/design.md`.

## Project Layout

```
fleet-scan/
├── cmd/fleet-scan/main.go
├── internal/
│   ├── cli/
│   │   ├── root.go              # Cobra root command
│   │   ├── scan.go              # Scan subcommand + signal handling + slog config
│   │   └── flags.go             # Flag types, collector spec parsing
│   ├── ocm/
│   │   ├── auth.go              # OCM token resolution + connection
│   │   ├── clusters.go          # Paginated cluster listing
│   │   └── types.go             # ClusterMetadata struct
│   ├── backplane/
│   │   └── login.go             # Shell exec backplane login
│   ├── collector/
│   │   ├── registry.go          # Collector interface + registry
│   │   └── managed_namespaces.go # First collector
│   ├── runner/
│   │   ├── runner.go            # Per-cluster orchestration
│   │   └── dispatcher.go        # Concurrency + semaphore
│   └── output/
│       ├── types.go             # ClusterRecord, RunMeta structs
│       ├── writer.go            # JSONL + meta.json writer
│       └── resume.go            # Resume-set loader
├── go.mod
├── Makefile
└── CLAUDE.md
```

## Implementation Phases

### Phase 1: Skeleton — compiles, dry run works

Create the repo, Go module, Cobra CLI, OCM auth, paginated cluster listing. After this phase, `fleet-scan scan --search="..." --dry-run` authenticates to OCM and reports the cluster count.

**Files:** `go.mod`, `Makefile`, `CLAUDE.md`, `cmd/fleet-scan/main.go`, `internal/cli/flags.go`, `internal/cli/root.go`, `internal/cli/scan.go`, `internal/ocm/auth.go`, `internal/ocm/types.go`, `internal/ocm/clusters.go`

**Key details:**
- OCM auth: check `OCM_TOKEN` env var first, fallback to `refresh_token` field from `~/.config/ocm/ocm.json`. Both paths use `connection.Tokens()`. No client credentials support.
- Pagination: page size 100, use `response.Total()` to know target count. Break when collected >= total. For `--dry-run`, single `size=1` request to get `Total()` without fetching all pages
- `ClusterMetadata`: 12 hardcoded fields with `json` tags. Comment: `// TODO: add --metadata-fields flag`
- `ParseCollectorSpecs()`: parses `name:key=val,key2=val2` syntax
- Validation: at least one `--collector` required unless `--dry-run`

**Verify:** `fleet-scan scan --search="managed='true'" --dry-run` → prints cluster count

### Phase 2: Output layer + iteration loop

Add JSONL writer, meta.json, run directories, resume support, and the per-cluster iteration loop (no login/collection yet — writes stub results).

**Files:** `internal/output/types.go`, `internal/output/writer.go`, `internal/output/resume.go`, `internal/runner/runner.go`

**Key details:**
- Run directory: `output/{timestamp}-{8-char-random-id}/`
- `Writer.WriteRecord()` flushes per-line (crash-safe)
- `LoadCompletedSet()` scans existing JSONL for cluster IDs
- Progress counter: `[N/Total] Processing cluster-name...` on stderr

**Verify:** Run without `--dry-run` → produces `output/*/results.jsonl` with stub records + valid `meta.json`

### Phase 3: Backplane login

Add isolated backplane login per cluster with temp kubeconfig.

**Files:** `internal/backplane/login.go`, `internal/runner/runner.go` (add login step), `internal/cli/cli.go` (wire login into RunOptions)

**Key details:**
- `Login()` returns `(kubeconfigPath, cleanup func(), error)`
- Shell exec via `exec.CommandContext(ctx, "ocm", "backplane", "login", clusterID, "--kube-path", path)` — uses context for timeout/cancellation
- Kubeconfigs stored in `<run-dir>/kubeconfigs/` (not system temp dir) — self-contained, debuggable; subdirectory created via `os.MkdirAll` on first login call
- `BackplaneLogin func(ctx, clusterID) (string, func(), error)` injected as a function field on `RunOptions` — nil means skip login (backward compatible with existing tests)
- On failure: write a record with all collectors marked `"status": "skipped"` with error, continue to next cluster
- Cleanup deferred per-cluster inside the runner loop — kubeconfig file removed after collectors (or stub) finish
- POC: minimal error handling on the shell exec — capture combined output for the skip record but no stderr parsing

**Verify:** Run against a real cluster, confirm kubeconfig isolation and temp file cleanup

### Phase 4: Collector framework

Define the interface, registry, and wire it into the runner.

**File:** `internal/collector/registry.go`

**Key details:**
- Interface: `Name()`, `Configure(params map[string]string) error`, `Run(ctx, clusterID, kubeconfigPath) (json.RawMessage, error)`
- `kubeconfigPath` passed to `Run()` so each collector builds its own client-go client
- `init()` auto-registration: `Register(name, factory func() Collector)`
- Collectors live in same package — blank import in `main.go` triggers all `init()` functions
- Validate collector names exist at CLI parse time

**Verify:** Register a trivial test collector returning `{"hello":"world"}`, confirm it appears in JSONL keyed by name

### Phase 5: managed-namespaces collector

First real collector using client-go dynamic client.

**File:** `internal/collector/managed_namespaces.go`

**Key details:**
- Default namespace patterns: `openshift-*`, `kube-system`, `kube-public`, `default`, `redhat-*`
- Default kinds: Pods, Deployments, StatefulSets, DaemonSets, Jobs, CronJobs, Services, ConfigMaps, Secrets, NetworkPolicies, Routes, ServiceAccounts, Roles, RoleBindings
- Params: `patterns=...`, `kinds=...`
- Uses `clientcmd.BuildConfigFromFlags()` → `dynamic.NewForConfig()` + `discovery.NewDiscoveryClientForConfig()`
- GVR resolution via discovery client with hardcoded fallback for core resources
- Output shape: `{"namespaces": [{"name": "...", "resources": {"Pods": {"count": N, "items": [...]}}}]}`

**Verify:** Full end-to-end scan against a real cluster, confirm resource enumeration in JSONL

### Phase 6: Concurrency + signal handling

Add dispatcher with semaphore-based concurrency and graceful SIGINT shutdown.

**Files:** `internal/runner/dispatcher.go`, updates to `internal/cli/scan.go`

**Key details:**
- Buffered channel of size N as semaphore
- First SIGINT: cancel context, stop dispatching, 30s grace for in-flight
- Second SIGINT: force exit
- `meta.json` status: `"interrupted"` if cancelled
- `Writer.WriteRecord()` mutex ensures concurrent write safety

**Verify:** `--concurrency=5`, confirm correct results. SIGINT mid-run, confirm partial output + interrupted meta

### Phase 7: Polish

Progress reporting, summary output, accurate finalize counts, README.

**Files:** `internal/runner/dispatcher.go`, `internal/cli/cli.go`, `README.md`

**Key details:**
- **Progress lines — before + after:** Keep existing "before" line (`[N/Total] cluster-name (cluster-id)`) for liveness. Add "after" line with outcome: `[N/Total] cluster-name (ok|error|skipped, 2.3s)`. Counter `N` is dispatch index (stable on both lines), not completion order.
- **Outcome ternary:** `ok` = all collectors succeeded. `error` = at least one collector errored. `skipped` = backplane login failed (all collectors marked skipped).
- **Dispatcher owns counts:** Atomic counters on `Dispatcher` — `Succeeded()`, `Failed()`, `Skipped()`. Replaces stubbed counts in `cli.go:205-207`.
- **End-of-run summary:** Compact single-line to stderr, printed in `cli.go` before `Finalize`. Format: `Done: 1500 clusters (1480 ok, 15 error, 5 skipped) in 12m34s → output/2026-06-25T143022/`. Use `Interrupted:` prefix when cancelled.
- **`--verbose`/`--debug`:** Deferred — flags stay wired to `slog` levels only. No additional progress output behavior.
- **README.md:** Minimal — one-liner description, prerequisites (Go, `ocm` CLI, OCM token), build (`make build`), usage example, output format (run directory contents), available collectors with params.

**Verify:** `--concurrency=5` with mixed success/failure clusters shows correct before/after lines, accurate summary counts, and correct `meta.json`.

## Dependencies

- `github.com/spf13/cobra`
- `github.com/openshift-online/ocm-sdk-go`
- `k8s.io/client-go`
- `k8s.io/apimachinery`

## Testing Strategy

- Unit tests for: flag/spec parsing, namespace pattern matching, resume-set loading, metadata extraction, JSONL serialization
- Integration tests (build tag `//go:build integration`): OCM API, backplane login, collector execution
- Makefile targets: `test`, `test-integration`, `lint`

## API Reference

The OCM clusters endpoint reference is stored at:
`/Users/dsquirre/Repos/gh-squirrd/claude.claude/resources/api/ocm/clusters-mgmt-v1-clusters.md`
