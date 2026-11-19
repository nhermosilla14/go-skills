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
| `internal/cli` | Argument parser, help, output rendering. TUI and `--json` are planned. | partial |
| `internal/app` | Use cases (`Add`); source resolution; no command syntax. | partial |
| `internal/skill` | `SKILL.md` parsing, discovery, sanitization. Rewriting and rename are planned. | partial |
| `internal/agents` | Agent registry and path rules (two entries; the other 77 are planned). | partial |
| `internal/install` | Plan and apply: canonical copy, relative links, copies, fallbacks. | partial |
| `internal/lockfile` | Project lock v2, atomic write, unknown-field preservation. Global lock is planned. | partial |
| `internal/hash` | Content hash contract and vectors. Git tree hash is planned. | partial |
| `internal/fsutil` | Atomic writes, realpath containment, guarded removal. | delivered |
| `internal/source` | Source grammar, fetches, authentication. | planned |
| `e2e/` | Built-binary tests with their fixtures. | partial |

The clock seam is `app.Env.Now`; no package calls `time.Now` directly except `cmd/skills`, which
wires the real clock. `internal/source`, TUI packages and the global lock do not exist yet.

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
