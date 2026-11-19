# GitHub blob fast path

- Intents: 20261119-github-blob-fast-path
- ADRs: 20261119-github-blob-fast-path

## Scope

Read public GitHub repositories through the API for sources without an explicit ref: tree and blobs,
completeness verification against tree SHAs, the anonymous → token → `gh api` ladder, fallback to the
clone path on any failure, and the root-skill `SKILL.md`-only exception. Parity of files, hashes and
lock entries with the clone path.

## End-to-end test

`TestE2EBlobInstallMatchesClone`, `TestE2EBlobFallsBackOnIncompleteTree`,
`TestE2EBlobTokenLadder`, `TestE2EBlobRootSkill` with an `httptest` GitHub API stub: identical results,
one-line fallback, token never printed, root skill payload trim.

## Out of scope

- GitLab or other hosts' APIs; write endpoints; the upstream owner allowlist.
