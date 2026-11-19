# Case: reimplementing an existing solution

The goal is a new implementation of something that already exists (a legacy system, a closed
product, a prototype), not a continuation of its codebase. First document the old solution in
`olddocs/`, then run the greenfield workflow using `olddocs/` as the input for requirement discovery.
If the user wants to keep evolving the same codebase, use `cases/adopt.md` instead.

## 1. Locate the old solution
Ask the user where it lives and what is available: source code, binaries, running instance, API
specs, manuals, database schemas, test data, people to ask. If the old solution is inside this repo,
leave it where it is; if it is not, never copy anything that the user is not allowed to copy, and
keep secrets, credentials and customer data out of the repo and out of `olddocs/`.

## 2. Document it in `olddocs/`
Create `olddocs/` at the repo root, with an `olddocs/README.md` that indexes it and states the
sources used and the date. Cover two things, in separate files or folders:

- **Behavior** (what it does, as seen from outside): features and user flows, inputs and outputs,
  APIs and interfaces, data formats, business rules, error cases, permissions, integrations,
  non-functional characteristics that matter (performance, availability, compliance).
- **Technical analysis** (how it is built): stack and versions, architecture and components, data
  model, dependencies, deployment and operations, known defects and pain points, and anything
  undocumented or surprising.

Rules:
- Describe, do not copy: document behavior and structure in prose, tables and diagrams, not the old
  source code. Short snippets are allowed only when they are the clearest way to state a format or rule.
- Mark every statement as **observed** (confirmed from code, a running system or a document) or
  **assumed** (inferred), and list open questions. Never present a guess as fact.
- Parallelize: delegate one subagent per area or module (a documenter or explorer type if available),
  then merge and cross-check their output.
- `olddocs/` is reference material, not a decision record. It is not frozen and is not part of the
  `docs/` records; fix it when later discoveries show it was wrong.

Checkpoint: show the user the `olddocs/` index and open questions, and get confirmation (or the
answers) before going on.

## 3. Discover requirements
With the user, go through `olddocs/` behavior by behavior and decide for each one whether the new
solution must **keep** it, **change** it, or **drop** it, and what is **new**. Ask the user, do not
decide for them. Capture the answers; they feed the next step.

## 4. Greenfield workflow
Follow `cases/greenfield.md` from the start (`docs/`, README, `AGENTS.md`), with these differences:
- `docs/vision.md` is derived from `olddocs/` and the step 3 answers: goals, what is kept, changed,
  dropped and new, and the constraints (e.g. data migration, coexistence with the old system).
  List the kept, changed and new behaviours individually enough to cut intents from them.
- Intents are written from those behaviours as branches take them, each with verifiable acceptance
  criteria and naming the `olddocs/` files it comes from.
- Dropped behaviours are recorded as non-goals in `docs/vision.md`, so the decision stays visible;
  once their area exists in `docs/spec/`, they go in its "Not supported" section.
- `docs/spec/` starts empty: it describes the new solution, and fills up as work is delivered.
- Decisions about the new architecture are ADRs as usual. Do not carry over old technical choices
  just because they exist; decide and record them. Where `olddocs/` shows a choice that must be
  kept (compatibility, data formats), say so in the ADR's context.
- Add `olddocs/` to the README's project structure and to `docs/README.md`, and keep the "Reference
  material" section of `AGENTS.md` (from `templates/AGENTS.md`), filling in which solution is being
  reimplemented and why.

## 5. Finish
Vendor the skill (`reference/vendoring.md`) and commit, then run the loop in `SKILL.md` for the
first behaviours the user picks. Consider an early slice that proves compatibility with the old solution (same
inputs, same outputs).
