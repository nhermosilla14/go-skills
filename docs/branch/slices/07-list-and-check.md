# List and check

- Status: Done

- Intents: 20261119-skill-management-and-scopes
- ADRs: 20261119-upstream-defect-corrections, 20261119-lockfile-compatibility-and-write-policy

## Scope

`list` for the project scope: installed skills per agent, lock comparison, `tracked`/`modified`/
`untracked`/`missing`, human grouping and stable ordering, `--json`. `check`: update availability plus
local changes, exit 1 when either exists, no writes.

## End-to-end test

`TestE2EManagementRoundTrip` starts from a slice-01 install, mutates one skill, deletes one, adds an
untracked folder, then asserts `list` human and JSON output and `check`'s exit code.

## Out of scope

- Global scope, remove and update (later slices).
- Update checks against a real remote (local git fixtures only).
