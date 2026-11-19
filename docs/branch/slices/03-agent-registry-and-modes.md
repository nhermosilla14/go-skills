# Agent registry, detection and install modes

- Intents: 20261119-agent-registry-and-install-modes
- ADRs: 20261119-package-architecture-and-dependencies, 20261119-install-plan-and-filesystem-model

## Scope

All 79 agent ids with project/global paths, project-only flags and universal membership; `stat`-based
detection; `*` expansion; copy-mode derivation (single directory, all-Eve, `--copy`); relative links
on POSIX, junctions with copy fallback on Windows; missing-project-dir skip and Claude Code's
on-demand creation; OpenClaw overlap; Eve flat-file install with its frontmatter subset; shared-path
deduplication.

First slice that also covers global paths, though global scope and its lock arrive in slice 10.

## End-to-end test

`TestE2EAgentMatrix` runs one install per agent id against a temporary home and project with a fake
agent-directory layout, asserting the resolved destination, the link type, and the copy fallback
where links are refused. Golden tests cover path resolution for every id on Linux, macOS and Windows.

## Out of scope

- Interactive agent selection, global lock writes.
