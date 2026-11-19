# Use a skill without installing it, and create a skill

- Status: Accepted
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

A user can hand one skill's instructions to an agent without touching the project, and can start a
new skill from the documented template.

## Acceptance criteria

- `skills use <source>@<skill>` resolves the source like `add`, selects exactly one skill (positional
  `@skill`, `--skill`, or the only discovered skill), writes it to a temporary directory and prints
  only the generated prompt on stdout.
- The prompt has the documented three/four-section structure and ends with a single newline; nothing
  else is written to stdout.
- `--agent <id>` starts a supported agent interactively with the prompt passed as one argument,
  inherits stdio, and propagates the child's exit code. Unsupported agents and unknown ids fail with
  a clear error before any fetch.
- `--full-depth` is honoured; more than one selected skill is an error; no skill found is an error
  with exit 1.
- `skills init [name]` creates a `SKILL.md` template with `name` and `description`, in the current
  directory or a new subdirectory, refuses to overwrite an existing file, rejects names that escape
  the working directory, and prints the created path.
- Both commands are in help and the spec; `use` stays out of `--json`.

## Spec changes

- Adds: `cli` / `use` prompt generation and agent launch
- Adds: `cli` / `init` template
- Adds: `sources` / single-skill selection for `use`

## Out of scope

- Installing the skill `use` fetched.
- `init` inside an existing non-empty directory.

## Open questions

- None blocking. The temporary directory is kept because the printed prompt points the agent at
  its supporting files; this matches upstream and is documented in the spec.
