# Restore from the lock and frozen-lockfile

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `management`, `lockfile`, `cli`

## Context

`skills-lock.json` is written and compared but nothing rebuilds a project from it. Upstream restore
is the reason the lock exists: a clean checkout plus the lock reproduces the installed skills at the
exact commits they were installed from. CI also needs a mode that refuses to change the lock instead
of quietly accepting drift.

## Decision

**Restore is lock-driven and pinned.** `skills install`/`i` without a source (and
`experimental_install`) read `skills-lock.json` and, for each entry:

- fetch its source at the recorded `commit` (not the head of its ref), using the existing source
  runner and per-source temporary checkout;
- compare the installed folder with `computedHash`; skip installation and only repair missing agent
  links when it matches; otherwise replace the content (`--force` always replaces);
- install with the recorded `installMode` into `.agents/skills` and the selected agents, using
  `lastSelectedAgents` from the global lock when `-a` is absent, then detected agents, then the
  canonical directory only;
- group entries by source and pinned commit so one checkout serves all of them; a failing source
  fails the run but not the others.

**Frozen mode** (`--frozen-lockfile`, default when any of `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`,
`BUILDKITE`, `CIRCLECI`, `TRAVIS`, `TF_BUILD`, `JENKINS_URL` is set, `--no-frozen-lockfile`
overrides) validates before installing: a missing or empty lock, an entry without a hash, a folder
whose content or package version differs, or an entry that the source no longer provides is an
error. Nothing is written and the exit code is 1. Entries from a migrated v1 lock with an empty hash
fail with a specific message; normal mode re-fetches them and records the new hash.

**No prompts, no partial lock.** Restore never asks; the lock is written only in non-frozen mode and
only when a hash actually changed, with the project lock's atomic writer. Download, well-known and
`node_modules` entries are reported as unsupported rather than guessed at.

## Alternatives considered

- Restore from the current ref head — rejected: it defeats reproducibility and CI drift detection.
- Frozen as an opt-in only — rejected: upstream defaults to frozen in CI, and that is what makes the
  lock meaningful there.
- Reusing `update` for restore — rejected: update chases new content, restore reproduces the pinned
  content; conflating them makes both surprising.

## Consequences

A committed lock becomes a reproducible installation recipe. Restoring an old lock can require
fetching old commits, which some hosts refuse; that failure is reported per source. The five
commands now share one source-grouping engine, which is also what `update` uses.
