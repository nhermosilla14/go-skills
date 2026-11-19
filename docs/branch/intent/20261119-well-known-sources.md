# Well-known skill providers

- Status: Accepted
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

A host that publishes a machine-readable skill index under its well-known path can be installed from
with digest verification, without a clone and without trusting an unverified download.

## Acceptance criteria

- For an HTTP(S) source, the tool probes the documented well-known index locations (the scoped index
  first, then the host root) and installs the skills the index lists.
- Every downloaded skill is verified against the index digest (`sha256:<hex>`); a mismatch is fatal
  and never falls back to another source.
- A scoped index URL (`/…/.well-known/skills/index.json`) never widens to the host root index.
- Lock entries record `sourceType: "well-known"`, `sourceBaseUrl` and `wellKnownDigest`; the global
  lock's folder hash is empty for these entries, as upstream does.
- `update` and `check` re-fetch through the provider and compare the digest; a changed digest offers
  an update, an unreachable provider is a failure.
- Index parsing is defensive: unknown fields ignored, malformed entries skipped, a malformed index is
  an error.
- Provider probing has a single 10-second budget, bounded retries and no unbounded redirect chains.

## Spec changes

- Adds: `sources` / well-known provider discovery
- Adds: `lockfile` / well-known entries

## Out of scope

- Hosts without a well-known index (they can still be used through downloads or git).
- Publishing an index from this tool.

## Open questions

- None blocking; the endpoint candidate list follows `olddocs/behavior/02-sources-and-fetching.md`.
