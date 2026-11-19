# Case: existing project that already follows the workflow

The repo has `docs/spec/`, `docs/adr/` and an `AGENTS.md` that references this skill. Do only what
is missing or outdated.

## 1. Sync the skill
Run the comparison from `reference/vendoring.md`. Follow the repo's vendored copy; upgrade it only
if the user agrees.

## 2. Quick check
On every entry, check only that the structure is in place and report what is missing:
- `docs/spec/README.md`, `docs/adr/`, `docs/branch/.gitkeep` and `scripts/check-branch-docs.sh`
  exist, and `AGENTS.md` names the git workflow and the integration branch.
- On the integration branch, `docs/branch/` holds nothing but `.gitkeep`.

## 3. Continue
Size the user's task and run it as `SKILL.md` describes (tiers, loop, finalization, wrap-up).

## Scoped check (at finalization)
Check what the branch touched, and fix it before merging:
- Every spec entry added or changed has its criteria and names tests that exist and pass.
- Every ADR added is `Accepted` with `Approved by`, its `Spec area` names an existing area file if
  set, and statuses match the folders the files are in.
- `docs/README.md` and `docs/spec/README.md` list the new files; `docs/architecture.md` and
  `README.md` describe what the code now does, and documented commands work.
- `docs/branch/` holds nothing but `.gitkeep`, and the merge gate script passes.

## Full audit (only when the user asks)
Run the scoped check over the whole repo, and also:
- List the `Unverified` spec entries. They are reported, not failures.
- Report code behaviour the spec does not describe, and spec entries the code no longer honours.
- Check that `AGENTS.md` contains every rule of `templates/AGENTS.md` (add what is missing, keep
  project specifics in Conventions) and that the merge gate policy is enabled (ask the user).
- If `olddocs/` exists: it is indexed in `docs/README.md` and the README's project structure, its
  `README.md` still lists its sources, and `AGENTS.md` has the "Reference material" section. Treat
  it as reference, not as a record.

Fix drift in living documents directly. For merged ADRs, only status changes and replacement are
allowed.
