# Sync stub and help polish

- Status: Accepted
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

The excluded Node package integration fails clearly instead of looking like a typo, and the help
surface matches what the tool can actually do.

## Acceptance criteria

- `skills experimental_sync` and its `sync` compatibility alias exit 2 with a message that names the
  feature and explains it is not supported by this implementation (npm/`node_modules` integration),
  plus a pointer to `skills add` for git or local sources.
- The commands are absent from `--help` (or listed under an "Unsupported" note), so nobody discovers
  a dead command by accident.
- No other command changes behaviour; unknown command handling for everything else stays as it is.
- `skills init --help` prints a short command-specific help block (template, target path, overwrite
  rule, traversal rejection), consistent with `use`.
- The help text, README command table and spec agree on every command and flag.

## Spec changes

- Adds: `cli` / unsupported command stubs
- Adds: `cli` / init help

## Out of scope

- Implementing sync (excluded by decision).
- Changing the exit code for unknown commands.

## Open questions

- None blocking.
