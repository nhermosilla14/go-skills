# Delegation details

For full-loop work the agent talking to the user is an **orchestrator**: it plans, splits work,
delegates, reviews and reports. Trivial-tier changes are done inline.

- **Delegate the loop's work** (code, tests, docs, records, non-trivial exploration or debugging).
  Inline only small things: a one-line edit, a quick read to decide whom to delegate to.
- **Prefer specialized subagents** (developer, architect, QA, test reviewer, explorer); use a
  general-purpose one only when none fits.
- **Brief fully.** Subagents start with no context and do not run this workflow: they follow the
  brief. Every brief includes: the goal; the relevant intent, ADR and slice files; the stack and
  `AGENTS.md` conventions; the files they own and must not touch; where branch records live and how
  they are named; the record rules that apply (see "Records" in `SKILL.md`); the expected output;
  and the statement that they work from this brief and must not start the workflow themselves.
- **Verify, don't trust.** Review each result against the intent's acceptance criteria, run or
  delegate the slice's end-to-end test, and send fixes back to the same subagent session.
- **Shared state is the orchestrator's.** Only it writes `docs/README.md`, `docs/architecture.md`
  and `docs/spec/` (subagents report what to change), finalizes the branch, commits, and handles
  summaries, questions to the user and status changes.
- **No subagents available.** Run the same loop and checkpoints inline, and tell the user that
  delegation was not possible.

## Parallelizing

Parallelize only tasks that pass the conflict check; when in doubt, go sequential.

- **Conflict check.** Two tasks conflict if both change the same thing: a file; a schema, data
  format or API contract; a dependency, build or runtime config; a test fixture; a shared doc.
  Tasks that only use an agreed, stable contract do not conflict. A task that changes a schema,
  migration, driver or fixture conflicts with everything built on it.
- **One slice at a time.** Within a branch, slices run in order. Parallel work is allowed only
  inside one slice, on changes that do not conflict, or for read-only tasks (exploration, review).
  Subagents share the branch's working tree: no sub-branches and no extra worktrees.
- **Cross-cutting changes go first.** A change that other work builds on (database engine,
  framework, auth method, data model) is its own intent. In the same branch it is built before the
  intents that depend on it, which list it in `Depends on`. Across branches it is merged before the
  dependent branch starts (`reference/branches.md`).

## Opting out

The user may ask for more or less delegation ("don't use subagents", "do it yourself"). Apply it
only to the scope they stated.
