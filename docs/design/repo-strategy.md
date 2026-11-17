# Repository Strategy

## Decision

- **GitHub is canonical.** All development, issues, PRs, and CI run on GitHub.
- **GitLab hosts a read-only mirror** of `main` (and tags), kept for Red Hat internal
  discoverability and potential future InnerSource visibility.
- The mirror is **one-directional** (GitHub → GitLab) and **identical** — no content
  divergence between the two. There is currently no known Red Hat InnerSource process
  this needs to comply with; revisit if one emerges.

## Why

- Development is kept GitHub-only to avoid dual-maintenance and sync conflicts.
- GitLab is Red Hat's expected location for internally-used tools, so the project
  should be visible there, but without splitting contributor workflow across two hosts.
- GitHub also doubles as the open-source showcase for this work.

## How contributions on GitLab are handled

GitLab merge requests/issues are disabled at the project level
(`merge_requests_access_level` / `issues_access_level` = `disabled`), so
contributions can't be opened there in the first place. The shared README
notes that development happens on GitHub.

## Repo location

- GitHub: `squirrd/fleet-scan` (canonical)
- GitLab: `dsquirre/fleet-scan` on `gitlab.cee.redhat.com`
  (`ocm-resources` was already taken by an unrelated existing project)

## Mirror mechanics

- `gitlab.cee.redhat.com` is Red Hat-internal only (no public DNS resolution),
  so GitHub-hosted Actions runners cannot reach it — mirroring cannot be
  automated via GitHub Actions without a self-hosted runner on the Red Hat
  network.
- For now, mirroring is **manual**: after a stable point on `main`, push to
  the `gitlab` remote:
  ```
  git push gitlab main --tags
  ```
- Revisit automation (e.g. a self-hosted runner) if manual pushes become a
  bottleneck.
