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
