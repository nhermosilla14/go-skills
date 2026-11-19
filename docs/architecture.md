# Architecture

Status: slice 01 delivered. The CLI shell, local `add`, skill discovery, the content hash, the
project lock and the two-agent install path exist. The diagram and the package list below include
the planned shape of later slices and are updated as they land. For the forces behind the project
see `docs/vision.md`; for decisions see `docs/adr/` and `docs/branch/adr/`.

## Shape

```
             ┌──────────────────────────────────────────┐
  argv ─────▶│ cmd/skills — flags, dispatch, exit codes   │
             └───────────────┬──────────────────────────┘
                             │ use cases
             ┌───────────────▼──────────────────────────┐
             │ internal/app — add, list, remove, update, │
             │ check, use, find, init, restore           │
             └───┬───────────┬────────────┬─────────────┘
                 │           │            │
        ┌────────▼──┐ ┌──────▼─────┐ ┌────▼──────────┐
        │ source    │ │ registry   │ │ install plan  │
        │ resolve + │ │ agents +   │ │ filesystem +  │
        │ fetch     │ │ detection  │ │ links/copies  │
        └────┬──────┘ └──────┬─────┘ └────┬──────────┘
             │               │            │
        ┌────▼──────┐   ┌────▼─────┐  ┌───▼──────────┐
        │ git/gh    │   │ paths    │  │ lock + hash  │
        │ http/tar  │   │ per OS   │  │ skills-lock  │
        └───────────┘   └──────────┘  └──────────────┘
```

## Packages

| Path | Responsibility | State |
|---|---|---|
| `cmd/skills` | Entry point: dispatch, user-facing errors, exit codes, build metadata. | delivered |
| `internal/cli` | Argument parser, help, JSON document, human rendering; `internal/cli/tui` holds the Bubble Tea flow. | delivered |
| `internal/app` | Use cases (`Add`, `List`, `Check`), prompt questions and the `Prompter` seam; no command syntax. | partial |
| `internal/skill` | `SKILL.md` parsing and rewrite, discovery (containers, fallback, plugin manifests, `skills.sh.json`), sanitization, name matching. | delivered |
| `internal/agents` | The 79-agent registry, injected-environment path resolution, `stat` detection, universal-target expansion. | delivered |
| `internal/install` | Plan and apply: canonical copy, relative links or Windows junctions with copy fallback, copies, skips, Eve flat files, installed-name rewrite. | partial |
| `internal/lockfile` | Project lock v2, atomic write, unknown-field preservation. Global lock is planned. | partial |
| `internal/hash` | Content hash contract and vectors. Git tree hash is planned. | partial |
| `internal/fsutil` | Atomic writes, realpath containment, guarded removal. | delivered |
| `internal/source` | Source grammar, git acquisition and remote ref lookup, authentication ladder, temporary workspaces. | delivered |
| `e2e/` | Built-binary tests with their fixtures. | partial |

Build and release: GoReleaser cross-compiles the eight targets from one commit with version
metadata injected through `-ldflags`; CI runs tests on Linux, macOS and Windows, lints, and
cross-compiles every target. The Taskfile wraps the same commands.

The clock seam is `app.Env.Now`, the process seam is `source.Runner` (git/`gh` execution), and the
prompt seam is `app.Prompter` (real TUI, scripted answers for tests, no-prompt refusal). The
filesystem is real (temporary directories) rather than virtual. `cmd/skills` wires the production
implementations and owns the process streams. The global lock does not exist yet.

Interfaces are defined when the first slice needs them, not up front. Network, git and clock
access sit behind small interfaces so tests run offline.

## Boundaries

- Nothing below `internal/app` knows about command syntax; nothing in `cmd/skills` touches the
  filesystem or network directly.
- Data formats (lock files, hash framing, install layout) are contracts and are specified in
  `docs/spec/`; changes need an ADR.
- Platform differences are isolated in `internal/agents` (paths and detection, with an injected
  environment so every OS is testable anywhere), `internal/install/link_{posix,windows}.go` (links
  and junctions) and `internal/fsutil` (atomic replace, containment).

## Diagrams

None yet.
