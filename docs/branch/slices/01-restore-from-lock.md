# Restore from the lock

- Status: Done
- Intents: 20261119-restore-from-lock
- ADRs: 20261119-restore-and-frozen-lockfile

## Scope

`skills install` and `skills i` without a source, plus the `experimental_install` alias, rebuild the
project from `skills-lock.json`: group entries by source and recorded commit, fetch each group once
at the pinned commit, skip entries whose installed content matches `computedHash`, repair missing
agent links, reinstall the rest with the recorded install mode, and report per skill. Agent targets
come from `-a`, then the global lock's `lastSelectedAgents`, then detection, then the canonical
directory alone. `--force` reinstalls everything; `--dry-run` prints the plan and writes nothing.
No prompts. A failing source fails the run but not the other sources.

## End-to-end test

`TestE2ERestoreFromLock`: install from a local git fixture, delete `.agents`, `.claude` and the
build output, run `install`, and assert identical content, hashes, links and exit code. A second
case moves the fixture tag and asserts the restored content is still the locked commit; a third
covers `--force`, `--dry-run` and a copy-mode entry.

## Out of scope

- `--frozen-lockfile` (slice 02).
- Download, well-known and `node_modules` entries.
- Global restore.
