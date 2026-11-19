# Remaining parity gaps

- Status: Accepted
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

Close the behavioural gaps found while delivering the first branch, so the tool matches upstream in
the places a user can notice without needing new source types.

## Acceptance criteria

- `update` supports both scopes: names with no `-g`/`-p` run the global phase then the project phase
  and print a header per phase; no names on a TTY offers `Project`, `Global` and `Both`; `Both`
  executes both phases and reports failures per phase. Exit 1 if anything failed.
- Running inside a supported host agent is detected from its environment variables; the install then
  becomes non-interactive and targets that agent, with `--json` allowed without `-y` in that case.
- `remove` refuses, with a clear message, to delete a non-dot agent directory it does not own when
  the request is ambiguous, matching upstream's unmanaged-directory guard.
- Global legacy directories (OpenClaw `~/.clawdbot`, `~/.moltbot`) are used consistently by add,
  list, remove and update, not only by path resolution.
- `add --list --skill <name>` lists all discovered skills, as upstream does.
- `add` accepts `--full-depth` retries for `@skill` selectors on remote sources (already
  implemented) and reports skipped category conflicts without failing the run unless nothing
  installed.
- Every deviation is either corrected or recorded in the defect-corrections ADR; the spec index and
  help text are updated.

## Spec changes

- Changes: `management` / update scope selection
- Adds: `cli` / host-agent detection
- Changes: `management` / remove guard
- Changes: `agents` / global legacy directories

## Out of scope

- The `--metadata` telemetry flag (no telemetry by decision).
- The npm library API.

## Open questions

- Exact host-agent environment variables follow the upstream `@vercel/detect-agent` mapping; the
  design ADR lists the ones implemented.
