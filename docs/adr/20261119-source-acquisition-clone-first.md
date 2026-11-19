# Source acquisition: clone-first, no blob fast path

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `sources`

## Context

Upstream acquires a Git source in two ways: a GitHub API "blob" snapshot that reads the tree and
files without cloning, and a real `git clone`. The two paths disagree on depth arithmetic, `SKILL.md`
casing, duplicate-name handling, root-skill payload and lock behaviour
(`olddocs/behavior/07-skill-format-and-discovery.md` §6). The blob path also needs a second
authentication ladder (anonymous API → token → `gh api`) and is disabled for explicit refs and
non-allowlisted owners.

## Decision

**One acquisition path: clone.** For git sources, run `git` into a temporary directory:

- Resolve the ref: a 40-hex `@…` suffix is a commit; anything else is a branch/tag. Shallow-clone
  the ref, then read `rev-parse HEAD` for the commit recorded in the lock.
- For a pinned commit, fetch that commit directly (server permitting), as upstream's restore path
  does.
- Select the subpath inside the clone, rejecting escapes and absolute paths.
- Remove the temporary tree on every exit path.

The GitHub Trees API is used **only** to compute the global lock's `skillFolderHash` for GitHub
sources, and degrades to the content hash with a warning when it is unavailable.

**Not implemented:** the blob snapshot fast path, its owner allowlist, its snapshot-completeness
check and its separate API authentication ladder.

## Alternatives considered

- Reimplement both paths for parity — rejected: doubles the surface with two divergent rule sets, and
  the observable result (installed files) is the same for well-formed repositories.
- Clone with sparse checkout — rejected for now: adds Git-version sensitivity for a bandwidth gain
  that matters only on very large repositories; revisit if it becomes a complaint.
- Use a Git library instead of the binary — rejected in the single-binary ADR; the authentication
  ladder is the reason.

## Consequences

- Users need `git` for git sources. Upstream could install from public GitHub without Git through the
  API; that capability is lost and is listed as a known limitation in the spec.
- Explicit refs, commit pins, private repositories and non-GitHub hosts all take the same code path,
  so behaviour is uniform and testable offline with local repositories.
- Bandwidth increases for large repositories; the lock and installed layout are unchanged.
- If a future intent wants git-free installs, the blob path returns as a new decision with its own
  tests, not as an implicit second implementation.
