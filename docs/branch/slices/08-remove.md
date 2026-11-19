# Remove

- Status: Done

- Intents: 20261119-skill-management-and-scopes
- ADRs: 20261119-upstream-defect-corrections, 20261119-install-plan-and-filesystem-model

## Scope

`remove`/`rm` by name, exact source, `--all`, `--skill '*'`, with `--agent` (including `'*'`),
confirmation and `-y`. Ownership rules: delete the canonical folder and lock entry only when no
other agent still links the skill; never delete untracked or unmanaged directories; skip and report
what the lock does not own; non-zero exit when nothing matched or a removal failed.

## End-to-end test

`TestE2ERemoveOwnership` builds a two-agent install, removes from one agent, asserts the canonical
folder and lock entry survive, removes from the other, asserts full cleanup; then asserts that a
hand-written folder is never deleted by `--all`, and that a missing name exits non-zero.

## Out of scope

- Global scope (slice 10).
