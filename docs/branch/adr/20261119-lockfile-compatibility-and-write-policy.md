# Lock file compatibility and write policy

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `lockfile`

## Context

Two lock files exist: the project `skills-lock.json` (v2, content hash) and the global
`.skill-lock.json` (v3, Git tree hash or content hash). Users commit the project lock, teams mix the
Node and Go CLIs during migration, and `--frozen-lockfile` makes any mismatch fatal. A lock written
here must be readable upstream and vice versa; a hash computed here must equal a hash computed there.
`olddocs/behavior/03-lockfile-and-hashing.md` has the schemas and the exact hash algorithm.

## Decision

**Semantic compatibility is the contract; byte identity is a best effort.**

- **Read:** accept v1 and v2 project locks and v1/v2/v3 global locks, tolerating unknown fields.
  Preserve unknown top-level keys and unknown per-entry keys on rewrite instead of dropping them
  (upstream drops them for the project lock, preserves them for the global one; keeping them is
  strictly safer for forward compatibility and is recorded in the corrections ADR).
- **Write:** the same document shape, `version: 2`/`3`, two-space indentation, trailing newline,
  keys sorted by UTF-16 code units — the JavaScript default ordering, implemented explicitly so
  non-BMP skill names sort identically to upstream. Field order inside an entry follows upstream's
  insertion order.
- **Hash:** reproduce the published algorithm exactly: collect files relative to the skill folder,
  skip `metadata.json` at any depth and the `.git`/`__pycache__`/`__pypackages__` directories,
  resolve symlinks inside the folder and drop escaping/broken/cyclic ones, normalise CRLF to LF only
  for files without a NUL byte, sort by unsigned UTF-8 path bytes, then feed
  `path NUL len(content) NUL content` to SHA-256 for each file in order. The three published vectors
  are tests. The global `skillFolderHash` is the 40-hex Git tree SHA for GitHub sources obtained from
  the Trees API, and the same content hash for local/clone/download sources; well-known entries use
  an empty string, as upstream does.
- **v1 migration:** read the legacy format, recompute the legacy hash (locale-independent ordering,
  no framing, skipping `.git`/`node_modules`), and only when it matches the folder on disk write the
  v2 hash. Otherwise store an empty hash, which forces a re-fetch and makes `--frozen-lockfile` fail
  with a specific message. Names with non-ASCII characters may not reproduce the legacy locale
  ordering; those take the empty-hash path and are re-fetched, which is safe.
- **Writes are atomic and serialised:** write to a temporary file in the same directory, `fsync`,
  then rename over the target, retrying on Windows sharing violations; guard concurrent writers with
  a `<lock>.lock` file (short acquire timeout, stale-lock takeover). The global lock uses the same
  writer; a corrupt lock is reported and left untouched until the user acts.
- **Ordering and pruning:** keys are always written sorted; entries not owned by the operation are
  preserved; removals delete only the keys they selected.

## Alternatives considered

- Byte-identical JSON (field order, spacing) as a hard requirement — rejected: it constrains
  internal modelling for no user benefit, and the semantic contract covers what upstream validates.
- Keep upstream's non-atomic global write — rejected: silent data loss on interruption.
- Drop v1 support — rejected: existing users would lose their install history and CI locks.

## Consequences

Mixed-tool teams can switch CLIs in either direction. A v1 lock with non-ASCII names may require a
re-install once. The hash package becomes a small, heavily tested island with no dependencies on the
rest of the tool, which is where reviewers should look first for regressions.
