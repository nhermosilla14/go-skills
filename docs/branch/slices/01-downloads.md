# Download and archive sources

- Status: In progress
- Intents: 20261119-download-and-archive-sources
- ADRs: 20261119-download-and-archive-safety

## Scope

HTTP(S) sources that are not git: a raw `SKILL.md`, `.zip`, `.tar`, `.tar.gz` or `.tgz` archive,
sniffed by content, fetched under the documented limits, extracted safely, collapsed to one top-level
directory, installed and recorded as `sourceType: "download"`. `update`, `check` and restore re-fetch
the recorded URL; URLs with query strings or userinfo are not recorded.

## End-to-end test

`TestE2EDownloadSingleFile`, `TestE2EDownloadArchive`, `TestE2EDownloadLimits`,
`TestE2EDownloadTraversal` against an `httptest` server: install, lock fields, update after the
server content changes, check exit codes, oversized bodies rejected, traversal archive rejected.

## Out of scope

- Well-known, Notion and blob paths (later slices).
