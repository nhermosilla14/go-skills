---
name: general-software-development
description: |
  Use this skill for any software development work in a repo: starting a new project, adding a
  feature, fixing a bug, refactoring, or changing code or configuration in any way.
  Triggers include "start a new project", "create a new repo", "scaffold", "bootstrap", "add a
  feature", "implement", "fix a bug", "refactor", or any task that edits source code in a repo.
  Also use it when the user asks to follow "the general workflow", "the standard workflow", "our
  workflow" or similar phrasing for software development work.
  Do not use it for non-development workflows (CI/CD pipelines, approval processes, git-only
  operations, documents or presentations) or for conceptual questions that edit no files.
metadata:
  version: 4.0.1
---

# General Software Development

Goal: every software change is driven by written requirements, decisions and docs. `AGENTS.md` sends
any agent opening the repo to the vendored copy of this skill, which holds the rules. Work follows
the loop Requirements → Design → Slices → Code → Test; how a repo gets there depends on its starting
point, so case files and reference files are loaded only when needed.

## Tiers
Size the task first. State the tier you propose and why, and wait for the user's confirmation.

- **Trivial.** Nothing a user or caller could observe changes (typos, comments, formatting, doc
  wording, patch-level dependency bumps, mechanical refactors that need no design decision), or a
  fix that restores behaviour the spec already describes.
- **Full loop.** Everything else, including any refactor that needs a design decision.

A trivial change needs no records, case file or loop: it is done inline, following the repo's git
workflow and the hygiene rules below. A fix to specified behaviour adds a regression test and its
name to the spec entry's `Tests`. If the repo does not follow this workflow yet, offer three options
and let the user choose: adopt now, fix only, or fix and adopt afterwards. Create no scaffolding
unless they choose adoption.

## Delegation
For full-loop work the agent talking to the user is an **orchestrator**: it plans, delegates,
reviews and reports. Before delegating, read `reference/delegation.md` (briefs, parallel work,
fallback when there are no subagents).

## Step 0 — Inspect and route
1. Run `git rev-parse --show-toplevel`. A git repo is mandatory. If there is none, stop and offer
   two options: you run `git init`, or the user creates the repo and tells you. Never run `git init`
   without confirmation, and do nothing else until the repo exists.
2. If the repo already has a vendored copy of this skill, compare versions and follow the repo copy
   (`reference/vendoring.md`).
3. Find the git workflow and the **integration branch** (the branch feature branches merge into:
   `main`, `develop`, ...) in `AGENTS.md` Conventions, a workflow skill or the user's instruction.
   If it is unclear, ask, and record the answer in `AGENTS.md` Conventions.
4. Never overwrite existing content: merge into it. Leave `CLAUDE.md` alone unless the user asks to
   link it to `AGENTS.md`.
5. Classify by evidence and read the matching case file, and only that one:

   | Case | Evidence | File |
   |---|---|---|
   | Reimplement | The user asks to rebuild, rewrite or replace an existing solution as a new implementation (the old one may be elsewhere, or in this repo), or `olddocs/` exists without `docs/spec/` yet. Check this first: it depends on the request, not on the repo state | `cases/reimplement.md` |
   | Greenfield | No meaningful code (empty, initial commit or bare scaffold) and no `docs/` structure | `cases/greenfield.md` |
   | Maintain | `docs/spec/` and `docs/adr/` exist and `AGENTS.md` references this skill (any version) | `cases/maintain.md` |
   | Adopt | Real code or history without that structure, including partial or foreign structures (plain `docs/`, another ADR layout) | `cases/adopt.md` |

   If the evidence is mixed or unclear, ask the user which case applies. Do not guess.
6. Follow the case file, then run the loop for the user's task. Every case ends by vendoring the
   skill (`reference/vendoring.md`); every task ends with the wrap-up below.

## Records

| Record | Where | Lifetime | Statuses |
|---|---|---|---|
| Spec | `docs/spec/<area>.md` | Living: current behaviour | none |
| ADR | `docs/adr/` | Permanent | Proposed, Accepted, Deprecated, Superseded |
| Intent | `docs/branch/intent/` | Work order, deleted at finalization | Proposed, Accepted, In progress |
| Slice | `docs/branch/slices/` | Work plan, deleted at finalization | In progress, Done |

Templates are in this skill's `templates/` folder; do not copy them into `docs/`.

- **Spec.** One file per functional area, indexed in `docs/spec/README.md`. Each entry is a
  behaviour, its acceptance criteria and the names of the tests that prove them (`Tests:`), or an
  `Unverified:` mark for adopted behaviour that has no tests yet. The spec changes only at
  finalization, so on the integration branch it always describes delivered behaviour. Deliberate
  non-goals go in the area's "Not supported" section; project-level ones in `docs/vision.md`.
- **Intent.** A goal, its acceptance criteria and the spec entries it adds, changes or removes. A
  branch delivers its intents whole: split an intent that is too big. A dropped intent is deleted.
  Work that no branch has started yet is not an intent: it is the gap between `docs/vision.md` and
  the spec.
- **Names.** Intents and ADRs are `YYYYMMDD-kebab-case-title.md`, dated at creation and never
  renamed. Refer to them by that ID (the file name without `.md`), not by path. ADRs with older
  names keep them.
- **Approval.** Ask the user for explicit confirmation, then set `Accepted` and fill `Approved by`.
  Nothing is approved implicitly, and coding approves nothing.
- **Editable until merged.** Records in `docs/branch/` may be edited. After approval, a change to
  acceptance criteria, interfaces, data formats, dependencies, security or scope needs approval
  again; smaller corrections are applied and reported in the summary.
- **Frozen once merged.** The content of an ADR on the integration branch is never edited. Allowed:
  its status and moves into `superseded/` or `deprecated/`. To change one, write a new ADR with
  `Replaces: <id>`, set the old one to `Superseded by <id>` and move it to `docs/adr/superseded/`.
  An ADR with no replacement becomes `Deprecated` and goes to `docs/adr/deprecated/`. Those two
  folders are history: do not read them by default.
- **People.** `Owner` (`Deciders` in ADRs) is the developer who generated the record, `Approved by`
  the one who accepted it. Take the name from git `user.name` or ask. Never record emails, invent
  names or list agents.
- **Vertical slice.** A thin end-to-end piece (a bit of foundation, the services involved, just
  enough UI) testable on its own. Never build all foundation, then all services, then all UI.

## The loop

For full-loop work. Each numbered checkpoint is a stop: do not go on without the user's approval.

1. **Branch.** Create a feature branch from the up-to-date integration branch
   (`reference/branches.md`, mandatory).
2. **Requirements.** Write intents. If the goal is unclear, ask the user and record the answer. Use
   specialized skills to run the questioning well (for example `product-discovery`,
   `requirements-analysis` or an interview skill) instead of improvising.
   **Checkpoint 1:** the user approves the intents.
3. **Design.** Record each decision as an ADR with its `Spec area` set. If the work needs no
   decision, say so and skip the checkpoint.
   **Checkpoint 2:** the user approves the ADRs.
4. **Slices.** Split the work into vertical slices in build order. Together they must cover every
   acceptance criterion. One slice is a valid plan. Start with a walking skeleton only for a new
   system or a new execution path.
   **Checkpoint 3:** the user approves the plan.
5. **Code and test, one slice at a time:**
   1. *Code.* Set the slice and its intents `In progress`, implement from its ADRs, update
      `docs/architecture.md`.
   2. *Test.* Run the slice's end-to-end test, which goes through the system's public interface
      (UI, API, CLI or library entry point) and is kept as a real test in the repo. Write a new one
      unless an existing test already proves the slice; a refactor slice reuses the existing suite.
      Earlier tests must still pass. Fix code failures. If the design is wrong, return to Design:
      edit the branch's ADR or replace a merged one, and update later slices.
   3. *Close.* When everything passes, set the slice `Done` and commit. Only then start the next.
6. **Finalize** when every intent of the branch is delivered (`reference/branches.md`).

## Hygiene and wrap-up (every task)
- Ensure `.gitignore` covers secrets (`.env`, `*.pem`, `*.key`, credentials) and stack build
  artifacts. **Warn the user** about any secret-looking file found.
- Git: create the branch and commit your work without asking, following the workflow in `AGENTS.md`
  Conventions. Push, open a pull request or merge only when the user asks. Use the contributor's
  configured git identity; never hardcode or change author data. If none is configured, ask.
- Summarize: files created or changed, records written, spec and doc updates, corrections made to
  approved records, vendoring result, open `TODO`s.
