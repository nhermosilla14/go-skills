# No telemetry

- Status: Proposed
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Spec area: `cli`, `network`

## Context

Upstream sends anonymous usage events and security-advisory requests, and surfaces an audit block
in `--json`. This tool is distributed as a binary outside npm, is used on private repositories and CI
machines, and its stated goal is no hidden network activity. Keeping the wire format would also
couple the implementation to an endpoint it does not control.

## Decision

No telemetry, no usage events, no security-audit requests, and no `DISABLE_TELEMETRY`/
`DO_NOT_TRACK` handling beyond ignoring them. The `--json` output has no `security` block, and the
`add --metadata` flag (which only fed telemetry) is removed.

Outbound network access is limited to what a command needs:

| Command | Allowed requests |
|---|---|
| `add`, `use`, `install`, `update`, `check` | Fetching the source it was given (git, HTTPS, well-known, GitHub API), plus update checks for tracked skills |
| `find` | The skills.sh search API |
| Everything else | None |

GitHub API calls that merely avoid a clone are an optimization, not telemetry; they are documented
in the spec because they reveal the repository being installed.

## Alternatives considered

- Keep wire-compatible telemetry — rejected: against the project's stated goal, and it makes the
  binary depend on a third-party endpoint for an optional feature.
- Local-only audit command — rejected for now: useful, but it is a new command (a non-goal) and
  would need its own requirement.

## Consequences

Nothing to disclose in a privacy notice; no environment variables to document; offline use is fully
supported. Users who want the upstream audit keep using the upstream CLI. If an audit feature is
wanted later, it arrives as a new intent and ADR.
