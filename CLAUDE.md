# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`fleet-scan` is a Go CLI tool that scans a fleet of OpenShift clusters via the OCM API. It filters clusters by search criteria, logs into each via backplane, runs pluggable collectors, and outputs structured JSONL results. Design doc: `docs/design/design.md`. Implementation plan: `docs/design/implementation-plan.md`.

## Build & Test Commands

```bash
# Build
make build                    # → bin/fleet-scan
go build -o bin/fleet-scan ./cmd/fleet-scan

# Test
make test                     # unit tests only
go test ./...                 # all unit tests
go test ./internal/cli/...    # single package
go test -run TestParseColl ./internal/cli/  # single test

# Integration tests (require OCM_TOKEN and cluster access)
make test-integration
go test -tags=integration ./...

# Lint
make lint
```

## Architecture

### Module & Entry Point
- Module: `github.com/squirrd/fleet-scan` (Go 1.25)
- Entry: `cmd/fleet-scan/main.go` — blank-imports collector packages to trigger `init()` registration

### Core Packages (`internal/`)
- **cli/** — Cobra command tree. `cli.go` (root + scan commands, signal handling, slog config), `flags.go` (flag types, `ParseCollectorSpecs()` for `name:key=val;key2=val2` syntax)
- **ocm/** — OCM SDK integration. `auth.go` (token from `OCM_TOKEN` env → `~/.config/ocm/ocm.json` fallback), `clusters.go` (paginated listing, page size 100), `types.go` (`ClusterMetadata` with 12 hardcoded fields), `ocm.go` (SDK connection builder)
- **backplane/** — `login.go`: shell-execs `ocm backplane login`, returns `(kubeconfigPath, cleanup, error)` with isolated kubeconfig per cluster. `adaptive_limiter.go` (adaptive rate limiting for concurrent logins), `retry.go` (retry with backoff on transient failures)
- **collector/** — `collector.go` + `registry.go`: `Collector` interface + `Register(name, factory)` pattern. Collectors auto-register via `init()`. Each collector gets `(ctx, clusterID, kubeconfigPath)` and returns `json.RawMessage`. Implementations: `managed_namespaces.go` (resource enumeration), `ns_attribution.go` (per-resource metadata extraction)
- **runner/** — `runner.go` (per-cluster orchestration: login → run collectors → write record), `dispatcher.go` (semaphore concurrency, graceful SIGINT)
- **output/** — `writer.go` (JSONL + `meta.json`, flush-per-line), `resume.go` (loads completed cluster IDs from existing JSONL), `types.go` (record/meta structs)

### Data Flow
```
CLI flags → OCM auth → paginated cluster list → [resume filter] →
  per-cluster: backplane login → run collectors → write JSONL record →
  finalize meta.json
```

### Collector Interface
```go
type Collector interface {
    Name() string
    Configure(params map[string]string) error
    Run(ctx context.Context, clusterID string, kubeconfigPath string) (json.RawMessage, error)
}
```

### Key Dependencies
- `github.com/spf13/cobra` — CLI
- `github.com/openshift-online/ocm-sdk-go` — OCM API
- `k8s.io/client-go`, `k8s.io/apimachinery` — cluster interaction

### External API Reference
OCM clusters endpoint docs: `/Users/dsquirre/Repos/gh-squirrd/claude.claude/resources/api/ocm/clusters-mgmt-v1-clusters.md`

## Testing Strategy — TDD

This project uses test-driven development. Write tests before implementation code.

### Unit Test Targets
Each package has focused, mockable unit tests:
- **cli/flags**: `ParseCollectorSpecs()` — valid specs, malformed input, empty params, special characters
- **ocm/types**: `ClusterMetadata` extraction from OCM response objects
- **ocm/clusters**: Paginated listing logic (mock OCM responses, verify page exhaustion)
- **ocm/auth**: Token resolution order (`OCM_TOKEN` → config file → error)
- **collector/registry**: Register, lookup, duplicate-name error, unknown-name error
- **collector/managed_namespaces**: Namespace pattern matching (`openshift-*`, `kube-system`, etc.), kind filtering, param parsing
- **output/writer**: JSONL serialization format, flush behavior, meta.json structure
- **output/resume**: Load completed set from JSONL, handle corrupt lines, empty file
- **runner/runner**: Per-cluster orchestration with mock backplane + mock collectors — success, backplane failure (all collectors skipped), collector error (captured, other collectors still run), timeout
- **runner/dispatcher**: Concurrency limiting, context cancellation

### Integration Tests
Guarded by `//go:build integration` build tag. Require real OCM credentials and cluster access.
- OCM auth + cluster listing against real API
- Backplane login + kubeconfig isolation
- Full end-to-end: search → login → collect → JSONL output

### TDD Cycle per Phase
1. Write failing tests for the phase's key behaviors
2. Implement until tests pass
3. Refactor while green
4. Verify phase milestone (e.g., `--dry-run` prints count, JSONL is valid)

### Mocking Patterns
- OCM client: interface wrapper around SDK calls, mock in tests
- Backplane login: function variable or interface, mock returns temp kubeconfig path
- Kubernetes client: `fake.NewSimpleClientset()` from `k8s.io/client-go/kubernetes/fake`
- Collector: mock implementing `Collector` interface
- Filesystem (for output): write to `t.TempDir()`

## Implementation Phases

All 7 phases are complete. Phases and their scope (see `docs/design/implementation-plan.md`):
1. **Skeleton** — module, CLI, OCM auth, cluster listing, `--dry-run`
2. **Output layer** — JSONL writer, meta.json, run dirs, resume, iteration loop (stub results)
3. **Backplane login** — isolated kubeconfig per cluster
4. **Collector framework** — interface, registry, wired into runner
5. **managed-namespaces collector** — first real collector using dynamic client
6. **Concurrency + signals** — semaphore dispatcher, graceful SIGINT
7. **Polish** — progress reporting, summary, verbosity levels

## Error Handling Conventions

- Cluster-level errors: skip cluster, capture error in JSONL record, continue to next
- Backplane failure: all collectors for that cluster marked `"status": "skipped"` with error
- Collector errors: captured per-collector, other collectors still run
- SIGINT: first cancels context (30s grace), second force-exits; meta.json status → `"interrupted"`
