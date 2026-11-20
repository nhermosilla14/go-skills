# Architecture

Status: all planned slices delivered. The CLI, local and git installs, the GitHub API fast path,
direct downloads and archives, well-known providers, Notion through `ntn`, discovery, the content
hash, both locks, update/check/remove/restore/use/find/init and the eight-target release pipeline
exist. The diagram and the package list below describe the delivered shape. For the forces behind the
project see `docs/vision.md`; for decisions see `docs/adr/`.

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
        │ API · http│   │ per OS   │  │ skills-lock  │
        │ tar · ntn │   │          │  │              │
        └───────────┘   └──────────┘  └──────────────┘
```

## Packages

| Path | Responsibility | State |
|---|---|---|
| `cmd/skills` | Entry point: dispatch, user-facing errors, exit codes, build metadata. | delivered |
| `internal/cli` | Argument parser, help, JSON document, human rendering; `internal/cli/tui` holds the Bubble Tea flow. | delivered |
| `internal/app` | Use cases (`Add`, `Restore`, `List`, `Check`, `Remove`, `Update`, `Use`, `Init`, `Find` support), prompts and the `Prompter` seam; no command syntax. | delivered |
| `internal/skill` | `SKILL.md` parsing and rewrite, discovery (containers, fallback, plugin manifests, `skills.sh.json`), sanitization, name matching. | delivered |
| `internal/agents` | The 79-agent registry, injected-environment path resolution, `stat` detection, universal-target expansion. | delivered |
| `internal/install` | Plan and apply: canonical copy, relative links or Windows junctions with copy fallback, copies, skips, Eve flat files, installed-name rewrite. | delivered |
| `internal/lockfile` | Project lock v2 and global lock v3, source-specific entries, v1 migration, atomic writes, unknown-field preservation. | delivered |
| `internal/hash` | Content hash contract and vectors, Git tree hash for the global lock. | delivered |
| `internal/fsutil` | Atomic writes, realpath containment, guarded removal. | delivered |
| `internal/source` | Source grammar; git acquisition and remote ref lookup, GitHub API fast path, downloads and archives, well-known providers, Notion through `ntn`, temporary workspaces. | delivered |
| `internal/search` | skills.sh search client, defensive parsing, partial-match fan-out. | delivered |
| `e2e/` | Built-binary tests with their fixtures. | delivered |

Build and release: GoReleaser cross-compiles the eight targets from one commit with version
metadata injected through `-ldflags`; CI runs tests on Linux, macOS and Windows, lints, and
cross-compiles every target. The Taskfile wraps the same commands.

The clock seam is `app.Env.Now`, the process seam is `source.Runner` (git and `gh` execution;
`ntn` has its own command seam), and the prompt seam is `app.Prompter` (real TUI, scripted answers
for tests, no-prompt refusal), and the host-agent seam is `agents.DetectHost` over the injected
environment. The filesystem is real (temporary directories) rather than virtual. `cmd/skills` wires
the production implementations and owns the process streams.

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
