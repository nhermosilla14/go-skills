# Well-known provider contract

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `sources`, `lockfile`

## Context

Some hosts publish a skill catalog at a fixed well-known path with digests. It is the only source
type that can install a skill without cloning and without a GitHub-specific API, but it also means
trusting a remote index. Upstream probes several candidate locations, verifies digests and refuses to
fall back on a mismatch.

## Decision

- **Discovery:** for an HTTP(S) source, probe the documented scoped index first
  (`<path>/.well-known/skills/index.json`) and then the host root
  (`/.well-known/skills/index.json`), all candidates under one 10-second budget. A scoped index never
  widens to the root index.
- **Verification:** every skill file is checked against the index digest (`sha256:<hex>`); a mismatch
  is fatal, with no fallback to git or to another provider. A missing digest rejects that entry.
- **Fetch:** files are downloaded individually with the download limits; the same traversal rules as
  archives apply to the declared paths.
- **Lock:** `sourceType: "well-known"`, `sourceBaseUrl`, `wellKnownDigest`; `source` is the provider
  identifier. The global lock stores an empty folder hash for these entries, matching upstream.
- **Re-fetch:** `update`, `check` and restore go through the provider again and compare digests; an
  unreachable provider is a failure, not a silent skip.
- **Parsing:** unknown fields ignored, malformed entries skipped, an index that is not JSON with a
  recognizable shape is an error.

## Alternatives considered

- Use git only — rejected: hosts without a repository cannot be used, and this is the documented
  format for such hosts.
- Skip digest verification — rejected: the index is the only integrity signal.
- Cache the index — rejected: freshness matters, and an extra cache introduces staleness bugs.

## Consequences

Hosts that publish the index become first-class sources with strong integrity guarantees. When a
host misconfigures the index, the failure is loud and does not silently install unverified content.
