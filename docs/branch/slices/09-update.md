# Update

- Status: Done

- Intents: 20261119-skill-management-and-scopes
- ADRs: 20261119-install-plan-and-filesystem-model, 20261119-source-acquisition-clone-first

## Scope

`update` for all or named skills: scope selection (`-g`, `-p`, `-y`, auto-detect), re-resolution of
the recorded source/ref/commit, changed-content detection, install-mode and rename preservation,
relocation handling for moved skills, per-skill failure reporting, non-zero exit when any source or
skill failed.

## End-to-end test

`TestE2EUpdateRoundTrip` installs from a local git fixture, advances the fixture's commit, updates the
skill, asserts new content and lock commit; then tests a renamed skill, a moved skill, a copied
install and an unreachable source (exit non-zero).

## Out of scope

- Global scope (slice 10).
- Update of download/well-known sources (later branch).
