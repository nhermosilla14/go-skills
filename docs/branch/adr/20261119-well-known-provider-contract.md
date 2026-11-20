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

- **Discovery:** probe, in order, `<base>/.well-known/agent-skills/index.json`,
  `<origin>/.well-known/agent-skills/index.json`, `<base>/.well-known/skills/index.json` and
  `<origin>/.well-known/skills/index.json`, all candidates under one 10-second budget, and use the
  highest-priority successful index. Update checks send `X-Skills-Update-Check: 1`. A path-scoped
  index never widens to the root index.
- **Verification:** schema v0.2.0 entries must carry `sha256:<hex>` and a mismatch is fatal, with no
  fallback to git or to another provider; malformed entries are skipped individually. The legacy
  schema (v0.1.0) has no digest and is accepted as upstream does; its file list is all-or-nothing.
- **Fetch:** v0.2.0 artifacts are single files (`skill-md` or an archive); archives use the provider
  limits of 50 MiB unpacked and 1000 entries, and links are refused. v0.1.0 lists individual files
  that are fetched one by one; a listed file that cannot be fetched fails that skill. Declared paths
  follow the same traversal rules.
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
