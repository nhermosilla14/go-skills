# Specification index

One file per functional area. Entries describe **delivered** behavior.

| Area | File | Delivered |
|---|---|---|
| cli | `cli.md` | Command surface, parsing, interactive flow, streams, JSON, unsupported stubs, exit codes |
| sources | `sources.md` | Source grammar, git clone and API acquisition, downloads, archives, well-known providers, Notion, authentication |
| discovery | `discovery.md` | Skill contract, containers, manifests, duplicates, rename |
| install | `install.md` | Plan/apply, link and copy modes, agent targets, root payload, global placement |
| agents | `agents.md` | Registry, path resolution, detection, selection |
| lockfile | `lockfile.md` | Project and global locks, source-specific entries, content hash, v1 migration |
| management | `management.md` | `list`, `check`, `remove`, `update`, restore, both scopes |
| release | `release.md` | Targets, metadata, artifacts, CI |

Conventions for each area file:

- Entries describe delivered behavior in the present tense.
- Each entry ends with `Tests:` naming the test functions that prove it, or `Unverified:` while no
  test covers adopted behavior.
- A "Not supported" section records deliberate non-goals that belong to the area.
