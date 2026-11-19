# Vision

## Purpose

`skills` is a command-line tool for installing, inspecting, updating and removing **agent
skills** — directories containing a `SKILL.md` file that teach coding agents how to perform a
task — across the coding agents a developer uses, in a project or globally, with a lock file that
makes installs reproducible.

This project is a Go reimplementation of [`vercel-labs/skills`](https://github.com/vercel-labs/skills)
v1.9.0. The upstream tool requires Node.js 22.20 or later and ships as an npm package. This one is a
single static binary with no runtime, aimed at the same data and the same workflows, on platforms
the upstream tool serves poorly or not at all (32-bit Linux, armhf Raspberry Pi, Windows x86).

## Users

- Developers who use one or more of the ~80 supported coding agents and want one tool to place
  skills where each agent reads them.
- Teams that commit a project lock file and restore it in CI or on another machine.
- People on Windows, Linux (including Raspberry Pi) and macOS who do not want a Node.js toolchain
  installed to manage skills.

## Goals

1. **No runtime.** One binary per platform, no Node.js, no package manager, no daemon.
2. **Behavioral parity** for the CLI surface described in `olddocs/behavior/`, with the deliberate
   deviations listed below.
3. **Data compatibility.** Lock files written here must validate with the upstream CLI and vice
   versa; the project content hash is the published contract and must match its test vectors.
4. **Eight platforms.** Windows x86/x86_64, Linux x86/x86_64/aarch64/armhf, macOS Intel/Apple
   Silicon, each from the same source in CI.
5. **Interactive parity.** The same decisions, in the same order, with a keyboard-driven
   multi-select that shows skill names, descriptions and sources.
6. **No surprises.** No telemetry, no hidden network calls, no silent deletion of data the tool
   does not own, and exit codes scripts can trust.

## Non-goals

- **The npm programmatic API.** `import { installSkills } from 'skills'` cannot exist in a Go
  binary. The operations behind it are still reachable through the CLI.
- **Node package integration.** `experimental_sync` (including `node_modules` and workspace
  scanning) and the `package.json` `skills` field are out of scope. The command names remain as
  explicit "not supported" stubs so scripts fail with an explanation instead of "unknown command".
- **Telemetry and security-audit requests.** No anonymous events, no advisory lookups, no outbound
  request except the ones a command explicitly needs (source fetch, search, update check).
- **New product features.** The goal is parity, not a larger tool: no registry, no GUI, no daemon,
  no new commands.
- **Reformatting or migrating user skills.** The tool installs, links and removes; it does not
  rewrite skill content except the documented `--rename` case.

## Success criteria

| ID | Criterion |
|---|---|
| S1 | `skills add <source> -a <agent> -y` installs the same files, in the same layout, as upstream v1.9.0 for the same source and agent. |
| S2 | A `skills-lock.json` written by this tool is accepted unchanged by upstream `skills list`/`check`, and one written by upstream is accepted here, including the v1 migration path. |
| S3 | The published content-hash vectors (empty skill, single file, two files) reproduce exactly. |
| S4 | All eight target binaries build in CI from one commit and pass a smoke test (`--version`, `--help`, install from a local source). |
| S5 | `add` → `list` → `update` → `check` → `remove` round-trips on project and global scope without leaving orphaned files or lock entries. |
| S6 | `skills use <source>@<skill>` prints exactly the generated prompt on stdout, and `--agent` launches the agent with it. |
| S7 | No command contacts the network unless it is fetching a source, searching, or checking for updates. |

## Constraints

- **Language:** Go 1.27, standard library first, minimal third-party dependencies (recorded in ADRs).
- **License:** MIT, matching upstream. This is a clean reimplementation from documented behavior;
  upstream source is not copied, and `olddocs/` is prose, not code.
- **Git access:** delegated to the installed `git` (and optionally `gh`) so the user's existing
  credentials, credential helpers and SSH agents keep working. No credential storage.
- **Filesystem:** must work on case-insensitive and case-sensitive filesystems, with and without
  symlink privileges (Windows falls back to copies).
- **Tests:** offline by default; network and git interactions are faked behind interfaces.

## Behavior inventory

The detailed behavior lives in `olddocs/behavior/`. This inventory is what requirements are cut
from; the spec grows as slices deliver.

### Kept as-is

| Behavior | Reference |
|---|---|
| `add` with its flag set, prompt order, scope choice, install methods and overwrite rules | `olddocs/behavior/01-add-and-install.md` |
| Source grammar and precedence: GitHub shorthand, refs, URLs, Azure DevOps, GitLab, SSH, local paths, archives, direct downloads, well-known hosts | `olddocs/behavior/02-sources-and-fetching.md` |
| Project lock (`skills-lock.json` v2) schema, content hash, v1 migration, atomic writes | `olddocs/behavior/03-lockfile-and-hashing.md` |
| `list`, `remove`, `update`, `check`, `use`, `find`, `init` and their documented options and output formats | `olddocs/behavior/04-cli-commands.md` |
| The agent registry (79 ids), project/global paths, `stat`-based detection, canonical `.agents/skills` linking, Eve special cases | `olddocs/behavior/05-agents-and-paths.md` |
| Skill contract, frontmatter rules, discovery depths and container list, plugin manifests, `skills.sh.json`, internal skills, relocation and `--rename` | `olddocs/behavior/07-skill-format-and-discovery.md` |
| `install` (no source) as restore-from-lock, and the `skills use` prompt format | `olddocs/behavior/06-sync-and-experimental.md`, `04-cli-commands.md` |
| Global scope and its separate lock file | `olddocs/behavior/03-lockfile-and-hashing.md` |

### Changed on purpose

Each item is an upstream defect or ambiguity being corrected; an ADR records the set.

| Area | Change |
|---|---|
| `remove` | `--agent '*'` works as documented; a request that matches nothing exits non-zero; failed removals exit non-zero. |
| `list` / `check` | A lock entry without a hash is reported the same way by both commands. |
| `update` | A source that fails to check counts as a failure for the exit code. |
| `init` | Names that escape the working directory (`..`, separators, absolute paths) are rejected. |
| Argument parsing | Unknown options are rejected with a clear error instead of silently ignored; extra positional sources are rejected. |
| `--json` | Output is always valid JSON, including error paths. |
| Ordering | Directory listings, duplicate-skill resolution and `list` output are deterministic (sorted) instead of filesystem order. |
| Exit codes | Any failed skill×agent install, removal or update yields a non-zero exit. |
| Global lock | Written atomically like the project lock; a corrupt lock is reported, not silently replaced. |
| Windows | Reserved device names and case-insensitive path containment are handled safely. |
| `use --help` | Prints `use`'s own help. |
| Blob fast path | May be dropped in favour of clone-only if installed results are identical (decided in design). |

### Dropped

| Behavior | Disposition |
|---|---|
| npm library API (`src/api/*`) | Not possible in a binary; operations stay available through the CLI. |
| `experimental_sync`, `sync` alias, `package.json` `skills` field, `node_modules` discovery | Command stubs with an explanation and exit code 2; everything else is out of scope. |
| Telemetry, security-audit requests, `--metadata` flag | Removed entirely; `--json` has no `security` block. |

### New

| Item | Why |
|---|---|
| Deterministic output and a documented exit-code table | Scripted use on CI is a primary use case. |
| `skills --version` reporting version, commit and build date | Reproducible bug reports for binaries without a package manager. |
| Clear "not supported" stubs for the dropped experimental commands | Fail loudly instead of pretending to work. |
