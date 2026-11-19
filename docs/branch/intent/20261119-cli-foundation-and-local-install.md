# CLI foundation and local installation

Work order in `docs/branch/intent/20261119-cli-foundation-and-local-install.md`. Folded into
`docs/spec/` and deleted when the branch is finalized.

- Status: In progress
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

A user can install skills from a local directory into the project's canonical skills location and
into at least one coding agent, non-interactively, and the result is recorded in a
`skills-lock.json` that upstream tools accept.

## Acceptance criteria

- `skills add ./fixtures/skills -a claude-code -y` installs every discovered skill into
  `.agents/skills/<name>/` (complete directory, symlinks dereferenced) and links it into
  `.claude/skills/<name>`, using the same layout as upstream v1.9.0.
- `skills add` without `-y` in a non-TTY exits with a clear error instead of hanging.
- `skills --help`, `skills <command> --help`, `skills --version` work; unknown commands and unknown
  flags exit non-zero with a message naming the offending argument.
- Discovery finds `SKILL.md` at the root, in `skills/<name>/`, and in `skills/<category>/<name>/`;
  a file without `name` and `description` is skipped with a warning, never fatal.
- The written `skills-lock.json` matches the v2 schema and the `computedHash` of a fixture equals
  the published vectors; `skills-lock.json` from upstream is read without error.
- `--copy` installs independent copies, `--skill` selects by name, `--list` prints without
  installing, and `--all` is equivalent to `--skill '*' --agent '*' -y`.
- `--rename` installs one selected skill under a new folder name, lock key and frontmatter `name`,
  and records the original in `skillName`.
- Exit codes follow the documented table: 0 success, 1 failure, 2 usage error or unsupported stub.

## Spec changes

- Adds: `cli` / command surface, help and exit codes
- Adds: `sources` / local-directory sources
- Adds: `discovery` / skill discovery from a source tree
- Adds: `install` / canonical install and agent linking
- Adds: `lockfile` / project lock v2 and content hash

## Out of scope

- Remote sources, authentication and archives (intent `remote-git-sources`).
- The complete 79-agent registry and detection (intent `agent-registry-and-install-modes`).
- The interactive TUI and the full `--json` contract (intent `interactive-install-flow`).
- Global scope.

## Open questions

- None blocking. Platform specifics (junctions, reserved names) follow the parity ADRs.
