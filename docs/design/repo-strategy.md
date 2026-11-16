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

GitLab merge requests/issues are not accepted. If someone opens one:
- Close it with a comment pointing to the GitHub repo.
- Do not merge or triage it in GitLab.
- GitLab's README notes it is a mirror and links to GitHub.

## Mirror mechanics

- Sync trigger: push/merge to `main` on GitHub (and tag pushes for releases).
- Implementation: GitHub Action pushes `main` + tags to the GitLab remote
  (`git push --force-with-lease gitlab-mirror main --tags`).
- GitLab repo has merge requests disabled/restricted to prevent accidental
  contribution drift.
