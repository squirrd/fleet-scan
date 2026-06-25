# Parking Lot

Open questions and deferred decisions. Items here are acknowledged but not yet resolved.

## Backplane Login Error Handling

- **Context:** Phase 3 backplane login uses shell exec with minimal error handling (POC scope). Combined output is captured for skip records but stderr is not parsed for structured error classification.
- **Revisit when:** Moving from POC to production tooling. Specific triggers: need to distinguish auth failures from network timeouts, or need retry logic for transient backplane errors.
- **Options:** Parse stderr for known error patterns, switch to backplane Go SDK (`github.com/openshift/backplane-cli`), or add retry with backoff.

## Shell Exec Dependency on `ocm` CLI

- **Context:** Phase 3 requires `ocm` binary in `$PATH`. No runtime check or helpful error message if it's missing.
- **Revisit when:** Distributing the tool to users who may not have `ocm` installed, or running in CI/container environments.
- **Options:** Add a preflight check at startup, vendor the backplane SDK, or document the dependency clearly.

## `--verbose`/`--debug` Progress Detail (deferred from Phase 7, 2026-06-25)

- **Context:** Phase 7 grilling session decided to leave `--verbose` and `--debug` wired to `slog` levels only. The plan originally called for `--verbose` to show per-collector status lines beneath each cluster's after-line, and `--debug` to inline raw collector JSON output.
- **Revisit when:** Users need mid-run visibility into which collectors are failing without reading the JSONL after the fact.
- **Options:** Per-collector sub-lines under the after-line (e.g. `managed-namespaces: ok`, `node-health: error: context deadline exceeded`), or structured logging via `slog.Info`/`slog.Debug` calls in the dispatcher.

## Dead Code: Serial `runner.Run` Function (deferred from Phase 7, 2026-06-25)

- **Context:** `runner.Run()` in `runner.go` is the Phase 2 serial runner, fully superseded by `Dispatcher.Dispatch()` with `concurrency=1`. No callers in production code — only referenced by `runner_test.go` tests.
- **Revisit when:** Next cleanup pass. Removing it requires migrating tests to use `Dispatcher` directly.
- **Options:** Delete `runner.Run` and migrate tests to `Dispatcher`, or leave it as tested reference implementation.
