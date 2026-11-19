# Frozen lock file

- Status: Done

- Intents: 20261119-restore-from-lock
- ADRs: 20261119-restore-and-frozen-lockfile

## Scope

`--frozen-lockfile` and `--no-frozen-lockfile` on restore, defaulting to frozen when any documented
CI variable is set. Frozen mode validates before touching disk: missing or empty lock, entry without
a hash, content or version mismatch, or an entry the source no longer provides all fail with exit 1
and change nothing. A v1 entry with an empty hash fails with a specific message; normal mode
re-fetches it and records the new hash.

## End-to-end test

`TestE2ERestoreFrozen`: a clean lock restores under `CI=1`; a mutated installed file fails frozen and
leaves the lock untouched; an empty-hash v1 fixture fails with the documented message; normal mode
repairs and writes the hash; `--no-frozen-lockfile` in CI allows the repair.

## Out of scope

- Frozen semantics for sync (no sync in this build).
