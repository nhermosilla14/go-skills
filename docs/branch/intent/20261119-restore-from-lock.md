# Restore project skills from the lock file

- Status: In progress
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

A clean checkout plus a committed `skills-lock.json` is enough to rebuild the project's skills
exactly, with no prompts and no drift, in a terminal or in CI.

## Acceptance criteria

- `skills install` and `skills i` without a source restore from `skills-lock.json`;
  `experimental_install` is an alias with the same behaviour and is listed in help.
- Each locked entry is fetched from its recorded source at the recorded `commit` (a git pin), not
  at the current head of its ref, so restoring after upstream moves yields the same bytes.
- Installed entries whose folder content already matches `computedHash` are left alone and only get
  missing agent links; entries whose content differs, or whose folder is missing, are restored.
- `--force` reinstalls everything; `--dry-run` reports the plan and writes nothing.
- Agent fallback order is `-a`/`--agent` (`'*'` allowed), then `lastSelectedAgents` from the global
  lock, then detected agents; with none of these only `.agents/skills` is filled. `installMode:
  copy` entries are copied into each agent directory instead of linked.
- `--frozen-lockfile` is the default when any of `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`,
  `BUILDKITE`, `CIRCLECI`, `TRAVIS`, `TF_BUILD`, `JENKINS_URL` is set; `--no-frozen-lockfile`
  overrides. Frozen mode fails with exit 1 and changes nothing when the lock is missing or empty,
  when a stored hash or package version differs, or when an entry has no hash; the lock file is
  never rewritten in that mode.
- A version 1 lock with unmigrated entries (empty hash) fails frozen mode with a clear message and
  is re-fetched in normal mode, recording the new hash.
- One failing source fails the run (exit 1) but does not prevent other sources from restoring.
- Restore never prompts and never installs `node_modules` entries (out of scope in this build).

## Spec changes

- Adds: `management` / restore from the project lock
- Adds: `lockfile` / frozen-lockfile checks
- Changes: `cli` / `install` alias behaviour

## Out of scope

- Global restore, `--frozen-lockfile` for sync (no sync in this build).
- Re-fetching download or well-known entries (later branch).

## Open questions

- None blocking.
