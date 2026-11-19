# Agent registry and installation modes

Work order in `docs/branch/intent/20261119-agent-registry-and-install-modes.md`. Folded into
`docs/spec/` and deleted when the branch is finalized.

- Status: Proposed
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Depends on: `20261119-cli-foundation-and-local-install`

## Goal

Skills can be installed for every agent upstream supports, on every target OS, in the location that
agent reads, using symlinks where possible and copies where necessary.

## Acceptance criteria

- The registry contains all 79 upstream agent ids with their display names, project and global
  directories, project-only markers and universal-directory membership; `skills add -a <id>`
  accepts every id and rejects unknown ones.
- Project scope resolves to `./<agent dir>/skills/` or the shared `./.agents/skills/`; global scope
  resolves to the home-based path for the OS, honouring `XDG_CONFIG_HOME`/`XDG_DATA_HOME` where
  upstream does, and `%APPDATA%`-style fallbacks on Windows.
- Detection is `stat`-based like upstream; `-y` with nothing detected targets all agents, a single
  detected agent is auto-selected, and running inside a supported coding agent targets that agent.
- Symlink mode creates one canonical copy and relative links on POSIX; copy mode is forced by
  `--copy`, by a single target directory, and by Eve. Windows uses junctions with absolute targets
  and falls back to copying when link creation is not permitted.
- Missing agent project directories follow upstream's skip/create rules, including Claude Code's
  on-demand creation and the OpenClaw source/destination overlap case.
- Eve installs a flat `<name>.md` with the retained frontmatter subset.
- Path collisions (two ids sharing a directory) install once, not twice.
- Golden tests assert the resolved path for every agent id on Linux, macOS and Windows (path
  resolution is pure and can be tested with a fake home and env).

## Spec changes

- Adds: `agents` / registry and path resolution
- Adds: `agents` / detection rules
- Adds: `install` / symlink, copy and junction modes
- Adds: `install` / agent-specific installation quirks

## Out of scope

- Global lock file contents (intent `skill-management-and-scopes`).
- Interactive agent multiselect (intent `interactive-install-flow`).

## Open questions

- Whether the eight "hidden universal" agents stay hidden from the prompt, as upstream does.
  Proposed: yes, for parity.
