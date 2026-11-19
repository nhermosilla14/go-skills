# Interactive installation flow

Work order in `docs/branch/intent/20261119-interactive-install-flow.md`. Folded into `docs/spec/`
and deleted when the branch is finalized.

- Status: Accepted
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Depends on: `20261119-cli-foundation-and-local-install`, `20261119-agent-registry-and-install-modes`

## Goal

A user running `skills add <source>` in a terminal picks scope, skills, agents and install method
with the keyboard, sees what each skill does before choosing, and gets the same decisions upstream
offers — while scripts get a stable machine-readable mode.

## Acceptance criteria

- The prompt sequence matches the documented matrix: scope (unless `-g`), skill selection (unless
  `--skill`/`--all` or a single skill), agent selection (unless `-a`/`--all` or a single detection),
  install method (unless `--copy` or copy is forced), and a final confirmation (unless `-y`).
- The skill selector is a grouped multi-select with the skill name, description and source, a
  detail pane for the focused skill, space to toggle, arrow/vim keys to move, `a` to toggle all and
  enter to confirm; groups follow the source's categories and plugin groupings.
- Cancel (`ctrl+c`, `esc`, EOF) exits without changes; the exit code is 0 on a TTY and 1 on a
  non-TTY, as upstream.
- `--json` emits exactly one JSON document on stdout for success and every failure mode, all human
  output goes to stderr, and the schema carries skill, agent, status, path and error per entry.
- Non-interactive combinations behave as documented: `-y`, `--all`, `--list`, `--skill '*'`,
  `--agent '*'`, invalid combinations rejected with their documented messages.
- `--rename`, `--subagent` and `--full-depth` are accepted with upstream's restrictions
  (`--rename` exactly one skill, `--subagent` only for Eve).
- The TUI degrades safely: no ANSI escapes when output is not a terminal, `NO_COLOR` respected,
  and a terminal narrower than the layout still renders without panics.

## Spec changes

- Adds: `cli` / interactive prompt flow
- Adds: `cli` / `--json` output contract
- Adds: `cli` / selection flags and their precedence

## Out of scope

- `find`'s interactive search (later intent).
- Any prompt redesign: the decision flow stays upstream's.

## Open questions

- Exact keystrokes beyond the documented set are ours to choose; the intent fixes only that space
  toggles and arrows move.
