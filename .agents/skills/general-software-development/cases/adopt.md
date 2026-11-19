# Case: adopting an old project with a different workflow

The repo has real code or history but not this structure. Reconstruct the scaffolding and records
without inventing history. Each phase ends with a user checkpoint: do not move on without the
user's confirmation. No existing content is lost along the way. Work on a branch.

## 1. Survey (read-only)
Read the code, manifests, README, existing docs, git history, CI config and any old decision records
or notes. Parallelize it: delegate one explorer per module or area, then merge their findings.
Produce a short summary of what the project is, how it is built and deployed, and what already
exists in terms of docs. Write nothing yet.

## 2. Interview
Ask the user one concise set of questions about purpose, users, constraints, known past decisions
and what is deliberately out of scope. Record the answers; do not assume.

## 3. Infer
Draft, without saving as final:
- `vision.md` and `architecture.md` from the survey and the interview.
- A list of **candidate ADRs**: decisions visible in the code (language, framework, persistence,
  deployment, interfaces, security).
- A list of **candidate spec areas and entries**: what the system does today, as behaviours with
  acceptance criteria that can be verified, each with the existing tests that prove it, if any.

## 4. Review
Show the drafts to the user. They correct, merge, drop or add. Mark anything uncertain as an open
question instead of choosing for them.

## 5. Write
Create the structure and files as in `cases/greenfield.md` (docs tree, `README.md`, `AGENTS.md`,
merge gate), merging with what exists.
- **Spec.** Write the reviewed entries in `docs/spec/`. An entry with tests lists them. An entry the
  user confirms works today but has no tests gets
  `Unverified: confirmed by <name> on YYYY-MM-DD`; it gets tests when that behaviour is next
  touched. Leave out behaviour the user cannot confirm, and list it as an open question.
- **ADRs.** Write them directly in `docs/adr/` with
  `Origin: Reconstructed from existing code on YYYY-MM-DD` and `Deciders` set to the developer doing
  the adoption. Set `Accepted` and `Approved by` only for those the user explicitly accepts; drop
  the rest and mention them in the summary.

## 6. Migrate
Move existing decision records into `docs/adr/`, keeping their content and file names, so every
adopted repo has the same layout. Move other existing docs into the new structure, or link them
from `docs/README.md` if moving would break references. Keep every old file's content and update
links.

## 7. Finish
Vendor the skill (`reference/vendoring.md`) and commit. List work that came up during the adoption
in the summary. From now on, every change goes through the tiers and the loop in `SKILL.md`.
