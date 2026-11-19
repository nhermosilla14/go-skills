# Remaining parity gaps

- Intents: 20261119-parity-gaps
- ADRs: 20261119-subagents-and-host-detection, 20261119-upstream-defect-corrections

## Scope

Both-scope `update` with per-phase headers and a `Both` prompt answer; host-agent detection from
environment variables with auto non-interactive targeting; the remove guard for unmanaged non-dot
agent directories; consistent use of OpenClaw's legacy global directories in add/list/remove/update;
`add --list --skill` listing all skills. Each deviation is fixed or recorded in the corrections ADR.

## End-to-end test

`TestE2EUpdateBothScopes` runs global and project phases in one command; `TestE2EHostAgentDetection`
runs with a host marker and no `-y`; `TestE2ERemoveUnmanagedGuard` proves a foreign non-dot
directory is refused; `TestE2EGlobalLegacyDirs` covers the OpenClaw legacy home.

## Out of scope

- `--metadata` and the npm library API.
