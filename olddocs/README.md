# olddocs — reference documentation for `vercel-labs/skills`

Reference material for reimplementing the Node.js CLI [`vercel-labs/skills`](https://github.com/vercel-labs/skills)
as a standalone Go binary. It describes the **old solution**; it is not a decision record and is
not part of `docs/`.

- **Solution documented:** `vercel-labs/skills` v1.9.0, MIT licensed.
- **Sources:** the public repository at commit `57217cf86ba38148a57af9e90d14a4c049558330`
  (2026-11-17), its `README.md`, `docs/*.md`, `src/`, `tests/`, `e2e/`, `scripts/` and build
  configuration. Nothing was executed: no upstream binary was installed or run, so every claim is
  either read from code/tests/docs or explicitly marked as inferred.
- **Date documented:** 2026-11-19.
- **Not copied:** no upstream source code, credentials, customer data or third-party assets. Code
  snippets appear only where a byte sequence or JSON shape is the clearest way to state a contract.

## Convention used in every file

Each statement is marked **(observed)** when confirmed in source, tests or upstream docs, and
**(assumed)** when inferred. Assumptions are never presented as fact. Every file ends with its own
open questions and an evidence index of `file:line` pointers into the upstream clone.

## Index

| File | Covers |
|---|---|
| `behavior/01-add-and-install.md` | `skills add`: grammar, flags, prompt flow, canonical vs agent directories, symlink/copy, overwrite, rename, exit codes |
| `behavior/02-sources-and-fetching.md` | Source parsing and precedence, refs and commits, well-known providers, direct downloads and archives, git clone, authentication ladder, limits |
| `behavior/03-lockfile-and-hashing.md` | Project and global lock schemas (v1/v2/v3), `computedHash` algorithm with vectors, v1 migration, write/atomicity semantics |
| `behavior/04-cli-commands.md` | Dispatch and argument grammar, `list`, `remove`, `update`, `check`, `use`, `find`, `init`, output conventions, exit codes |
| `behavior/05-agents-and-paths.md` | The 79-agent registry, project/global path resolution per OS, detection heuristics, canonical `.agents/skills` linking, agent-specific quirks |
| `behavior/06-sync-and-experimental.md` | `experimental_sync`, the `package.json` `skills` field, conflict and cleanup rules, `experimental_install`, the public programmatic API |
| `behavior/07-skill-format-and-discovery.md` | `SKILL.md` frontmatter contract, validation, sanitization, discovery depths and ordering, plugin manifests, `skills.sh.json`, internal skills, relocation |
| `technical/01-architecture.md` | Stack, module map, dependencies, build and packaging, test strategy, telemetry, CI, concurrency and error conventions |
| `technical/02-porting-risks.md` | What a Go port must get exactly right, Node-specific surfaces, cross-compilation and platform pitfalls, risk table |

## Headline findings that shape the reimplementation

1. **Two lock files, two hash contracts.** `skills-lock.json` (project, v2) uses a 64-hex content
   hash; the global `.skill-lock.json` (v3) uses a 40-hex Git tree SHA for GitHub sources. They are
   not interchangeable and comparing across shapes always looks outdated.
2. **The content hash is a published contract.** Sorted UTF-8 relative paths, CRLF→LF for text
   files, `path NUL length NUL content` framing, one SHA-256. Existing lock files must keep
   validating.
3. **Discovery is order-dependent and unsorted.** `readdir` order decides duplicate winners, and
   `--full-depth` does not deepen catalog walks. Determinism is a deliberate choice to make.
4. **Interactive behavior is a large part of the product** — scope, skill, agent and method
   prompts, plus a custom grouped multiselect with a detail pane.
5. **Documented behavior and code disagree in at least a dozen places** (`remove --agent '*'`,
   `list` vs `check` on hash-less entries, `use --help`, global update failure exit codes,
   `init` path traversal, OpenClaw legacy paths, hidden universal agents). Each needs an explicit
   keep-or-fix decision.
6. **Node-only surfaces:** the npm library API, `node_modules`/workspace sync, the `package.json`
   `skills` field and telemetry are not reachable from a standalone binary without emulation.

## Open questions carried into requirements

Per-file lists are authoritative. Consolidated themes:

- Compatibility posture: bug-for-bug, or documented-behavior-first with the known defects fixed.
- Byte-parity of lock files that Node and Go might both write.
- Fate of the v1 lock migration and of the legacy `localeCompare` sort.
- Whether `experimental_sync` and the `skills` field are dropped outright or kept as a clear
  error stub.
- Telemetry and the security-audit block: drop, or keep wire-compatible.
- Interactive UX parity versus a simpler, modern prompt library.
- Windows symlink policy (junctions plus copy fallback, or require Developer Mode).
- `skills use` prompt delivery (stdout pipe and argv) as a downstream contract.
- Release engineering: signing/notarization ownership and whether macOS Intel stays a target.
