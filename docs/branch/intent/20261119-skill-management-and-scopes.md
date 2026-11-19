# Skill management and scopes

Work order in `docs/branch/intent/20261119-skill-management-and-scopes.md`. Folded into `docs/spec/`
and deleted when the branch is finalized.

- Status: Accepted
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Depends on: `20261119-cli-foundation-and-local-install`, `20261119-agent-registry-and-install-modes`

## Goal

Once skills are installed, a user can see what is installed, detect drift and available updates,
update them, and remove them safely — for the project and for the whole machine.

## Acceptance criteria

- `skills list` shows installed skills per agent for the selected scope, compares project skills
  with the lock, and reports `tracked`, `modified`, `untracked` and `missing`; `--json` returns the
  same information as one JSON document; output order is deterministic.
- `skills list -g` reads the global lock and global agent directories, and a hash-less lock entry is
  reported the same way by `list` and `check`.
- `skills remove` removes by name, by exact source, with `--all`, with `--skill '*'`, and with
  `--agent '*'`; it removes the canonical folder and lock entry only when no other agent still links
  the skill, never deletes directories the lock does not track, and exits non-zero when nothing
  matched or a removal failed.
- `skills update` updates all or named skills, in the selected scope, preserves install mode and
  rename, re-fetches download sources, reports per-skill failures, and exits non-zero if any skill
  or source failed to check or update.
- `skills check` reports available updates and local changes without writing anything, and exits 1
  when either exists.
- Global scope has its own lock file (v3 schema), written atomically, and a corrupt global lock is
  reported instead of being silently replaced.
- A version 1 project lock is upgraded on read: entries whose files still match the legacy hash get
  a v2 hash, others get an empty hash and are re-fetched by `install`, and `--frozen-lockfile`
  fails until they are.
- A lock entry with no hash never causes data loss and is clearly reported as unverifiable.

## Spec changes

- Adds: `management` / `list` and status comparison
- Adds: `management` / `remove` selection and ownership rules
- Adds: `management` / `update` and `check`
- Adds: `lockfile` / global lock and scope resolution
- Adds: `lockfile` / version 1 migration

## Out of scope

- Restoring from the lock without a source (later intent).
- `skills use`, `find` and `init` (later intent).

## Open questions

- Whether `update` should also restore agent links that were manually deleted. Upstream repairs
  links when it already has the content; propose: yes for tracked skills.
