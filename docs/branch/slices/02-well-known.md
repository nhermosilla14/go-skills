# Well-known providers

- Intents: 20261119-well-known-sources
- ADRs: 20261119-well-known-provider-contract

## Scope

Provider discovery for HTTP(S) sources: scoped and root index probing under one budget, defensive
index parsing, per-file digest verification, `sourceType: "well-known"` lock entries with
`sourceBaseUrl`/`wellKnownDigest`, and provider-aware update/check/restore.

## End-to-end test

`TestE2EWellKnownInstall`, `TestE2EWellKnownDigestMismatch`, `TestE2EWellKnownScopedIndex` with an
`httptest` host: valid index installs, mismatch is fatal without fallback, scoped index never widens,
update detects a changed digest, unreachable provider is a failure.

## Out of scope

- Publishing an index.
