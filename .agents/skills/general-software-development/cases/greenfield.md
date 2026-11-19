# Case: greenfield project

The repo has no meaningful code and no `docs/` structure. Follow the workflow from the very start.

## 1. Understand the project
- Detect the stack from manifest files (`go.mod`, `pyproject.toml`, `package.json`, `Cargo.toml`,
  `*.csproj`, `pubspec.yaml`, ...) if any exist, so prerequisites and commands are accurate.
- If purpose, scope or constraints are unknown, ask one concise set of questions before writing
  `docs/vision.md`. Do not invent goals.

## 2. Where the scaffolding goes
Put the scaffolding on its own branch, like any other work. Only a repo with no commits at all gets
it as its first commit, directly on the integration branch.

## 3. `docs/`
Create the structure below, filling `vision.md` and `architecture.md`.

```
docs/
├── README.md          # index of all docs, kept current
├── vision.md          # goals, non-goals, users, success criteria, constraints
├── architecture.md    # CURRENT architecture only; short; diagrams welcome (ASCII or Mermaid)
├── spec/     README.md   # index of areas; one file per area (templates/spec.md) as they appear
├── adr/      YYYYMMDD-record-architecture-decisions.md   + deprecated/ superseded/
└── branch/   .gitkeep    # working records of the branch in progress; see reference/branches.md
```

The first ADR records the decision to use the spec, ADRs, branch records and vertical slices. Also
write ADRs for decisions already made at bootstrap (language, framework, packaging, ...). These
scaffolding ADRs are written directly in `docs/adr/` and need no `Spec area`. Ask the user to
approve them before finishing this case.

Install `templates/check-branch-docs.sh` as `scripts/check-branch-docs.sh` and set up the merge gate
as described in `reference/branches.md`.

## 4. `README.md`
Create or complete these sections (keep existing content; document only commands that exist, mark
planned ones `TODO`):

1. Name and one-paragraph purpose, linking `docs/vision.md`.
2. Prerequisites: tools and exact versions.
3. Getting started: clone, install, configure (`.env.example`, never real secrets), run.
4. Usage: CLI flags, API endpoints or UI entry points, with examples.
5. Project structure: annotated tree, including `.agents/skills/`.
6. Development guidelines: build/test/lint/format commands, conventions, the loop order, and where
   the templates for intents, ADRs, slices and spec areas are (the vendored skill's `templates/`).
7. Documentation: link to `docs/README.md`.
8. License, if known.

## 5. `AGENTS.md`
Create it from `templates/AGENTS.md`, or merge into an existing one without duplicating: add any
missing rule and renumber. Fill the bracketed Conventions, including the git workflow and the
integration branch. Never put a project description or command list there; point to `README.md`.

Tooling (formatters, linters, test runners, build tools, CI helpers) and workflow (git workflow,
branching, commit conventions, review and release process) are recorded under `## Conventions`, not
in ADRs. Edit them in place when they change. Never record emails there.

## 6. Finish
Vendor the skill (`reference/vendoring.md`) and commit. The loop for the first piece of work starts
on a new branch once the scaffolding is merged; if the user prefers, continue on the scaffolding
branch instead.
