# Branch records

Full-loop work happens on a feature branch that starts from the integration branch, never from
another unmerged feature branch. The branch carries its working records in `docs/branch/`, which
must be empty again before it merges. That is what lets the hosting platform tell an unfinished
branch from a finished one.

## During the branch
- **Intents** are written in `docs/branch/intent/` and **new ADRs** in `docs/branch/adr/`, named and
  approved as described under "Records" in `SKILL.md`.
- **Slices** are the work plan, in `docs/branch/slices/` as `NN-kebab-case-title.md` (build order).
- **`docs/spec/` is not edited** during the branch, apart from the finalization below.
- **Merged ADRs** change only through their status and moves to `superseded/` or `deprecated/`. To
  change one, write a new ADR in `docs/branch/adr/` with `Replaces: <id>`.
- **Dependent intents** may share a branch and are built in order. If the work depends on another
  branch, that branch merges first; then update from the integration branch and continue.

## Finalize before merging
Only the orchestrator finalizes. First update the branch from the integration branch (rebase or
merge, as the repo's workflow says). Then, in this order:
1. **Fold each intent into the spec.** Add, change or remove the entries it names, with their
   acceptance criteria and the names of the tests that prove them. Move lasting out-of-scope
   decisions to the area's "Not supported" section. Update `docs/spec/README.md`, then delete the
   intent file.
2. **Move the ADRs** to `docs/adr/`. Each must be `Accepted`; delete any that was abandoned. Apply
   the status change and move of every ADR they replace.
3. **Delete** `docs/branch/slices/` and any other leftover, keeping `docs/branch/.gitkeep`.
4. **Look for conflicts with what merged meanwhile:** compare `docs/architecture.md`, the ADRs and
   the spec entries the branch touches with the integration branch. If an ADR contradicts another,
   or components, boundaries, data models or interfaces conflict, do not resolve it silently and do
   not let the newer one win by default. Explain the conflict, let the user choose which decision
   stands, supersede the other, reconcile code, architecture and spec, and rerun the affected tests.
5. **Check.** Run the scoped check from `cases/maintain.md` and the merge gate script, then commit.

If the integration branch moves again before the merge, update from it and repeat steps 4 and 5.
Nothing is renamed or renumbered.

## Merge gate
`scripts/check-branch-docs.sh` (template: `templates/check-branch-docs.sh`) fails when:
- `docs/branch/` holds anything but `.gitkeep`;
- a spec entry has neither `Tests:` nor `Unverified:`;
- an ADR in `docs/adr/` is still `Proposed`.

It checks form only. Whether the tests pass is the job of the repo's own test pipeline, which should
be a separate required check.

Run it in CI on every pull request into the integration branch and make it required: a required
status check in a branch protection rule or ruleset on GitHub, a build validation branch policy on
Azure DevOps. The skill can add the script and the pipeline file but cannot switch the policy on:
ask the user to enable it and to confirm it is on.

## Several branches in parallel
Apply the conflict check from `reference/delegation.md` across branches too. If two branches would
change the same contract (an ID format, a reference between entities, a schema, the migration
tool), first deliver and merge a branch that settles it, then start the others from the result.
Branches cannot see each other's `docs/branch/`.
