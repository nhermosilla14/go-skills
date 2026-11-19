# Eve subagents

- Status: Accepted
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

Projects that use Eve subagents can install a skill for the root agent or for specific subagents,
and keep that placement across list, update and remove.

## Acceptance criteria

- When an Eve project has `agent/subagents/`, an interactive install offers `Root agent` plus one
  entry per subagent (sorted), multi-select, at least one required.
- `--subagent root|name` selects targets non-interactively; `root` and `.` mean `agent/skills`;
  unknown names are accepted as given, matching upstream.
- Each selected subagent receives the flat `<name>.md` copy under
  `agent/subagents/<subagent>/skills/`, with the same reduced frontmatter as the root Eve install.
- The project lock records the placement in `subagents` (`""` for root), so `update` re-applies it
  and `remove` deletes exactly those files without touching other subagents.
- `list` attributes the skill to Eve and shows subagent placement.
- Global scope rejects Eve and its subagents with a clear error.
- `--subagent` without an Eve source is a usage error.

## Spec changes

- Adds: `install` / Eve subagent targets
- Adds: `lockfile` / `subagents` field
- Adds: `management` / subagent-aware list, update and remove

## Out of scope

- Subagents for agents other than Eve.

## Open questions

- None blocking.
