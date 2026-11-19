# Upstream parity policy

- Status: Proposed
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.

## Context

`olddocs/` documents `vercel-labs/skills` v1.9.0, and the goal is feature parity. But upstream's code
and its own documentation disagree in more than a dozen places, some documented behavior cannot be
reproduced safely (path traversal in `init`, silent no-op flags, exit codes that hide failures), and
two surfaces are impossible or unwanted in a binary (the npm library API and telemetry).

"Parity" therefore needs a rule that says what has to match, what may differ, and where the boundary
between compatibility and correctness lies.

## Decision

Three tiers:

1. **Data and formats are contracts.** Anything another tool or an existing installation may read or
   depend on matches upstream: `skills-lock.json` (v2) and the global lock (v3) schemas and field
   semantics, the content-hash algorithm and its framing, the `SKILL.md` frontmatter contract, the
   install layout (canonical directory, per-agent directories, relative links), agent ids and their
   project/global paths, source grammar, and the `skills use` prompt format. Changes here need an
   ADR that explicitly accepts the break.
2. **Documented behavior wins over code defects.** Where upstream's docs and code disagree, or where
   code behaviour is plainly unintended (silent flag ignoring, invalid JSON on an error path,
   non-deterministic selection, exit code 0 after a failure), the Go tool implements the documented
   or correct behaviour and records the deviation in
   `docs/adr/20261119-upstream-defect-corrections.md`. Reproducing an upstream bug requires saying
   so in that ADR; it is not the default.
3. **Unreachable surfaces are dropped with an explanation.** The npm library API, `node_modules`
   sync, the `package.json` `skills` field and all telemetry are out of scope. Dropped commands keep
   their names and fail with a clear message and a dedicated exit code, never as "unknown command".

Human-facing text (help, prompts, warnings, progress) follows upstream's information, not its exact
wording: messages may be clearer, but must not remove information or change what a flag does.

## Alternatives considered

- Strict bug-for-bug parity — rejected: it would ship known data-loss risks (notably `remove`'s
  protection logic and unsafe `init` paths), unusable exit codes for CI, and invalid JSON.
- Free modernization — rejected: existing lock files, agent directories and team scripts must keep
  working, and "it's better now" is not a testable target.

## Consequences

Every deliberate deviation is written down and testable. Compatibility tests use upstream-produced
lock files and the published hash vectors as fixtures. Reviewers can reject a change that alters a
tier-1 contract without an ADR, and can require a deviation entry for a tier-2 fix.
