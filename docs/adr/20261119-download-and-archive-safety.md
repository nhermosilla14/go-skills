# Download and archive safety

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `sources`, `lockfile`

## Context

A URL can serve anything. Upstream accepts a raw `SKILL.md` or an archive, sniffs the format instead
of trusting the extension, bounds the work and defends against path traversal. Installing from the
internet into a project directory demands the same discipline here, and the lock must be able to
re-fetch the content later without storing credentials.

## Decision

- **Fetch:** HTTPS and HTTP only, redirects followed up to a small limit, TLS verification never
  disabled, one 10-second-per-request budget. A response that is not a valid skill or archive is an
  error naming the content type.
- **Limits:** 10 MiB body, 25 MiB extracted, 1000 entries by default. `SKILLS_DOWNLOAD_MAX_BYTES`,
  `SKILLS_EXTRACT_MAX_BYTES`, `SKILLS_EXTRACT_MAX_FILES` may raise them; nothing can lower the
  defaults. Limits are enforced while streaming and while extracting, not after the fact.
- **Archives:** format by magic bytes (`zip`, `tar`, gzip-wrapped tar), `.tar.gz`/`.tgz` accepted.
  Extraction goes into a fresh temporary directory and rejects absolute paths, `..`, drive letters,
  backslashes used as separators, entries escaping the root, and links pointing outside it. Tar
  symlinks are skipped; zip symlinks are refused. A single top-level directory is collapsed so
  `pkg/skill/SKILL.md` installs as `skill`.
- **Lock:** `sourceType: "download"`, `source`/`sourceUrl` and `skillPath`. A URL containing a query
  string or userinfo is not recorded; the install succeeds but the entry is marked non-updatable and
  the user is told to re-add the pinned URL.
- **Re-fetch:** `update`, `check` and restore use the recorded URL; a changed body means an update, an
  unreachable URL is a failure. Hashing is the normal content hash of the installed folder.

## Alternatives considered

- Trust the URL extension and skip sniffing — rejected: `.tgz`/`.tar.gz`/extensionless endpoints are
  common and attacker-controlled.
- No size limits — rejected: a URL is untrusted input.
- Store signed URLs in the lock — rejected: the lock is usually committed and those URLs carry
  credentials.

## Consequences

Downloads become a first-class, reproducible source with the same update semantics as git. Some
hosted URLs cannot be restored later by design; the tool says so at install time instead of failing
mysteriously in CI.
