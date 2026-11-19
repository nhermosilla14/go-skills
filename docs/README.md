# Documentation

| Document | Contents |
|---|---|
| `vision.md` | Goals, non-goals, users, success criteria, constraints, behavior inventory |
| `architecture.md` | Current architecture: packages, responsibilities, boundaries |
| `spec/` | What the system does today, per functional area, with the tests that prove it |
| `adr/` | Accepted architecture decisions, frozen once merged |
| `branch/` | Working records of the branch in progress: intents, ADRs, slices. Empty when merged |
| `../olddocs/` | Reference: the upstream `vercel-labs/skills` v1.9.0 behavior and technical analysis |

## Reading order for a new contributor

1. `vision.md` for what the project is and refuses to be.
2. `architecture.md` for how the code is arranged.
3. `spec/` for what is delivered today.
4. `adr/` for why, when the spec does not answer it.
5. `../olddocs/` only when implementing parity with upstream.

## Conventions

- Spec areas are `docs/spec/<area>.md`, one per functional area, indexed in `docs/spec/README.md`.
- Every spec entry names the tests that prove it (`Tests:`) or is marked `Unverified:`.
- ADRs are `YYYYMMDD-kebab-case-title.md`; an accepted ADR is never edited, only superseded or
  deprecated.
- Working records live under `docs/branch/` and are deleted before merge; the merge gate script
  enforces both this and the spec/ADR form.
