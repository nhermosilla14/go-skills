# Global scope and lock migration

- Status: Done

- Intents: 20261119-skill-management-and-scopes
- ADRs: 20261119-lockfile-compatibility-and-write-policy, 20261119-package-architecture-and-dependencies

## Scope

`-g` on every command, the global lock (`~`-based `.skill-lock.json`, v3, `skillFolderHash`,
`lastSelectedAgents`, `dismissed`), atomic writes and corruption reporting, GitHub Trees API folder
hash with content-hash fallback, and v1 → v2 project-lock migration including the empty-hash path
and its `--frozen-lockfile` consequences.

## End-to-end test

`TestE2EGlobalScopeRoundTrip` installs, lists, updates and removes a global skill with a temporary
`HOME`/`XDG_*`; `TestE2EProjectLockV1Migration` feeds the v1 fixture and asserts migrated hashes,
the empty-hash entry, and the frozen-lockfile error until re-fetched.

## Out of scope

- Windows registry-style home resolution beyond the documented env vars.
