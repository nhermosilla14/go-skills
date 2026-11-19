# Records and vertical slices

- Status: Proposed
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.

## Context

This repo starts from a documented reference implementation (`olddocs/`) and has to grow into a
cross-platform tool with a large parity surface. Without written requirements and decisions, the
"match upstream" rule becomes unverifiable and the work becomes one long unshippable branch.

## Decision

Adopt the `general-software-development` workflow, vendored at
`.agents/skills/general-software-development/`:

- Requirements are intents in `docs/branch/intent/`; design is ADRs; the plan is vertical slices in
  `docs/branch/slices/`; each slice is implemented and tested before the next starts.
- `docs/spec/` describes delivered behavior and is updated when a branch is finalized.
- Working records are deleted before merge; the merge gate script enforces empty branch records,
  spec entries with `Tests:` or `Unverified:`, and no `Proposed` ADR in `docs/adr/`.

## Alternatives considered

- Ad-hoc development from `olddocs/` — rejected: no traceability from behavior to tests, and the
  parity decisions would live only in commit messages.
- A single up-front full specification — rejected: the tool is too large to specify before code,
  and slices would not be independently testable.

## Consequences

Every change has a written "why" and a test named in the spec. The cost is bookkeeping on every
branch and a merge gate that must be enabled on `main`.
