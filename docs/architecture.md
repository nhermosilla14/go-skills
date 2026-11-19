# Architecture

Status: scaffolding only. There is no implementation yet; the shape below is the starting plan and
is updated by every slice. For the forces behind the project see `docs/vision.md`; for decisions see
`docs/adr/`.

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

## Planned packages

| Path | Responsibility |
|---|---|
| `cmd/skills` | Entry point: argument parsing, command dispatch, user-facing errors, exit codes. |
| `internal/cli` | Flag model, help text, TUI prompts and non-interactive equivalents, output rendering (`--json`). |
| `internal/app` | Use cases orchestrating the rest; no filesystem or network code of its own. |
| `internal/source` | Source grammar, refs, providers, downloads, archives, authentication ladder, temp trees. |
| `internal/skill` | `SKILL.md` parsing and rewriting, discovery, sanitization, skill selection, rename. |
| `internal/agents` | Agent registry, per-OS path resolution, detection, canonical directory and install-mode rules. |
| `internal/install` | Install planning and execution: files, links, copies, overwrite rules, per-agent quirks. |
| `internal/lockfile` | Project and global lock schemas, read/write with atomicity, status comparison. |
| `internal/hash` | The two content-hash contracts and their test vectors. |
| `internal/fsutil` | Atomic writes, locking, realpath containment, platform-specific link/junction helpers. |

Interfaces are defined when the first slice needs them, not up front. Network, git and clock
access sit behind small interfaces so tests run offline.

## Boundaries

- Nothing below `internal/app` knows about command syntax; nothing in `cmd/skills` touches the
  filesystem or network directly.
- Data formats (lock files, hash framing, install layout) are contracts and are specified in
  `docs/spec/`; changes need an ADR.
- Platform differences are isolated in `internal/agents` (paths) and `internal/fsutil` (links,
  atomic replace, locking).

## Diagrams

None yet.
