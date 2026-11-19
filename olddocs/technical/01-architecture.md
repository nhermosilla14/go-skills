# 01 — Architecture of `skills` (vercel-labs/skills v1.9.0)

Source read: `/tmp/opencode/vercel-skills` (clone of `vercel-labs/skills`, MIT).
Every claim below is tagged **observed** (read directly in the repo) or **assumed**
(inferred from naming, comments, or absence of evidence). No code was executed.

---

## 1. What the tool is

A single-package TypeScript CLI plus a small programmatic library that installs, updates,
lists, and removes "agent skills" — directories containing a `SKILL.md` with YAML
frontmatter — into the skills directories of ~85 coding-agent products (**observed**, `src/types.ts`,
`package.json` keywords).

It supports four families of source:

| Source kind | How it is obtained | Evidence |
| --- | --- | --- |
| GitHub / GitLab / generic git / Azure Repos | `git clone --depth 1` (with SHA-fetch and `gh`/SSH fallbacks) | **observed** `src/git.ts`, `src/source-parser.ts` |
| Local path | read in place, no copy of the source tree | **observed** `src/source-parser.ts` (`type: 'local'`) |
| Well-known HTTP discovery | `/.well-known/agent-skills/index.json` or `/.well-known/skills/index.json` | **observed** `src/providers/wellknown.ts` |
| Direct download | raw `SKILL.md` or zip/tar.gz archive, limits enforced | **observed** `src/download-source.ts`, `src/archive.ts` |

There is also a "blob fast path" that avoids cloning for allow-listed GitHub owners by
reading a precomputed snapshot from skills.sh or the GitHub Trees API (**observed**
`src/blob.ts`), and a Notion integration that shells out to the external `ntn` CLI
(**observed** `src/notion-test.ts`).

---

## 2. Stack and runtime requirements

| Item | Value | Evidence |
| --- | --- | --- |
| Language | TypeScript, ESM only (`"type": "module"`) | **observed** `package.json`, `tsconfig.json` |
| Runtime | Node.js **>= 22.20.0** | **observed** `engines`, README |
| Package manager | `pnpm@12.18.0`, single-package workspace (`pnpm-workspace.yaml` present but no sub-packages) | **observed** |
| TS target | `ESNext`, `moduleResolution: bundler`, `allowImportingTsExtensions`, `strict`, `noUncheckedIndexedAccess` | **observed** `tsconfig.json` |
| Runtime deps | `nostics`, `tar`, `yaml` only | **observed** `package.json` `dependencies` |
| Bundled deps (declared dev-only) | `@clack/prompts`, `picocolors`, `simple-git`, `xdg-basedir`, `@vercel/detect-agent` | **observed** `devDependencies` + `build.config.mjs` + `e2e/package.e2e.ts` assertion that they are *not* installed at runtime |
| Formatter | Prettier, single-quote-ish defaults in `.prettierrc` | **observed** |
| Git hooks | husky `pre-commit` → lint-staged → prettier | **observed** `.husky/`, `package.json` |

Node-version-sensitive APIs actually used (matters for a Go port's behaviour mapping):
`node:util` `parseArgs`, `node:path` `posix.matchesGlob`, `fs/promises` `glob`,
`module.enableCompileCache`, import attributes (`with { type: 'json' }`),
`AbortSignal.timeout`, `readline.emitKeypressEvents`, `node:util.stripVTControlCharacters`
(**observed** by import/usage grep).

---

## 3. Module layout

Paths are relative to the repo root. "Key exported operations" lists only the names that
shape behaviour (not every symbol).

### 3.1 Entry points and public surface

| File | Responsibility | Key exported operations |
| --- | --- | --- |
| `bin/cli.mjs` | npm bin shim. Optionally enables Node's compile cache (unless `NODE_DISABLE_COMPILE_CACHE`), then dynamically imports `../dist/cli.mjs`. | none (side-effecting script) |
| `src/cli.ts` | CLI entry: reads `package.json` version at runtime, routes subcommands, prints banner/help, owns `main()` and the final `flushTelemetry()`. | none exported; `getVersion()` internal |
| `src/index.ts` | Library entry. Named re-exports only (explicitly forbids `export *`); guarded by a `tsnapi` API snapshot. | `getSupportedAgents`, `detectAgents`, `resolveSource`, `installSkills`, `listSkills`, `removeSkills`, `diagnostics`, `isDiagnostic`, `version` + public types |
| `src/api/*.ts` | Programmatic API layer: never prompts, prints, or exits; throws `nostics` diagnostics for whole-call failures and returns per-skill failures. | `api/types.ts` (hand-written public types), `config.ts#resolveConfig`, `agents.ts`, `resolve.ts`, `install.ts`, `list.ts`, `remove.ts`, `version.ts` |

### 3.2 Command implementations (CLI)

| File | Responsibility | Key exported operations |
| --- | --- | --- |
| `src/add.ts` (≈3.7k LOC, largest) | `add` command: source resolution, skill selection, agent selection, install-mode choice, lock writes, telemetry, JSON output, security-advisory rendering. Also hosts shared helpers reused by `update`, `install`, `sync`, and the API. | `runAdd`, `parseAddOptions`, `initTelemetry`, `buildInstallTargets`, `nonInteractiveTargetAgents`, `defaultInstallMode`, `installToTargets`, `fetchSources`, `installFetched`, `installFromSource`, `recordProjectLock`, `recordGlobalLock`, `trackSourceInstall`, `renameSkill`, `applyRename` |
| `src/remove.ts` | `remove`/`rm`: resolve names to canonical install dirs, refuse to delete unmanaged or category directories, prune agent links, update locks. | `removeCommand`, `parseRemoveOptions`, `resolveSkillsToRemove` (internal helpers exported for tests) |
| `src/list.ts` | `list`/`ls`: enumerate installed skills per scope/agent, annotate with lock source and "modified" state, optional `--json`. | `runList` |
| `src/update.ts` | `update`/`upgrade` and `check`: compare GitHub tree SHAs / content hashes against locks, then reinstall in process. | `runUpdate`, `parseUpdateOptions`, `resolveUpdateScope`, `updateProjectSkills`, `updateGlobalSkills`, `checkWellKnownForUpdates`, `printSkippedSkills` |
| `src/install.ts` | `experimental_install` / `install` / `i` with no source: restore `skills-lock.json`, with `--dry-run`, `--force`, `--frozen-lockfile`. | `runInstallFromLock`, `resolveRestoreAgents` |
| `src/sync.ts` | `experimental_sync` / `sync`: crawl `node_modules` + `package.json` `skills` field, prune stale skills, write locks. | `runSync`, `parseSyncOptions` |
| `src/find.ts` | `find`/`search`: query `skills.sh` `/api/search`, interactive picker, then delegate to `runAdd`. | `runFind`, `parseFindOptions`, `searchSkills`, `searchSkillsAPI` |
| `src/use.ts` | `use`: materialize one skill into a temp dir and print a prompt to stdout, or spawn one of three supported agents. | `runUse`, `parseUseOptions`, `buildUsePrompt`, `materializeUseSkill`, `launchAgentInteractively` |

### 3.3 Core libraries

| File | Responsibility | Key exported operations |
| --- | --- | --- |
| `src/agents.ts` | Static registry of ~85 agents: project `skillsDir`, global `skillsDir`, detection predicate, universal/global-read flags; plus Eve subagents and discovery dir lists. | `agents` (record), `detectInstalledAgents`, `getUniversalAgents`, `getVisibleUniversalAgents`, `getNonUniversalAgents`, `isUniversalAgent`, `readsCanonicalGlobally`, `getEveSubagents`, `AGENT_PROJECT_SKILL_DIRS`, `EVE_SUBAGENTS_DIR` |
| `src/installer.ts` | Placement engine: canonical `.agents/skills/<name>` copy plus per-agent symlink (POSIX relative link / Windows junction), or copy; overwrite protection, category-dir protection, symlink dereferencing with containment checks; readers for installed skills. | `installSkillForAgent`, `installRemoteSkillForAgent`, `installWellKnownSkillForAgent`, `installBlobSkillForAgent`, `listInstalledSkills`, `copyDirectory`, `getCanonicalPath`, `getInstallPath`, `getCanonicalSkillsDir`, `isSkillInstalled`, `sanitizeName` (re-export), `pathsOverlap` |
| `src/skills.ts` | Discovery + `SKILL.md` parsing (`name`/`description` required strings), internal-skill gating, depth-limited search, filter by requested names. | `discoverSkills`, `parseSkillMd`, `hasSkillMd`, `filterSkills`, `getSkillDisplayName`, `isInternalSkill`, `shouldInstallInternalSkills` |
| `src/frontmatter.ts` | Minimal YAML frontmatter parser (no `---js`), and byte-preserving `name:` rewrite used by `--rename`. | `parseFrontmatter`, `setFrontmatterName` |
| `src/skill-hash.ts` | The v2 lock content hash over the files the installer would write. | `hashSkillFiles`, `computeSkillFolderHash`, `collectSkillFolderFiles`, `isExcluded`, `isExcludedPath` |
| `src/skills-sh-hash.ts` | Separate `skills-sh-tree-v2` hash used for pack pinning parity with skills.sh. | `collectSkillsShFiles`, `computeSkillsShTreeHash` |
| `src/local-lock.ts` | Project lock `skills-lock.json` (v2): read/validate/migrate v1, atomic write, cross-process mutex, portable local sources, frozen-lock dry comparison. | `readLocalLock`, `writeLocalLock`, `addSkillToLocalLock`, `removeSkillFromLocalLock`, `getLockedSkillState`, `computeLegacySkillFolderHash`, `getLocalLockPath`, `getInstalledSkillDir` |
| `src/skill-lock.ts` | Global lock `~/.agents/.skill-lock.json` or `$XDG_STATE_HOME/skills/.skill-lock.json` (v3), plus dismissed prompts and last-selected agents; GitHub token lookup. | `readSkillLock`, `writeSkillLock`, `getSkillLockPath`, `getGitHubToken`, `fetchSkillFolderHash`, `addSkillToLock`, `removeSkillFromLock`, `getLastSelectedAgents`, `saveSelectedAgents` |
| `src/source-parser.ts` | Parse every accepted source syntax (shorthand, `github:`/`gitlab:`, tree URLs, Azure Repos `_git`, SSH, fragments for ref/skill, aliases), subpath sanitisation, privacy probe. | `parseSource`, `getOwnerRepo`, `parseOwnerRepo`, `sanitizeSubpath`, `isRepoPrivate`, `fetchGitHubRepoInfo`, `lookupGitHubRepo`, `withCanonicalGitHubRepo` |
| `src/git.ts` | All git interaction: clone with hardened env, SHA fallback, `gh repo clone` and SSH retry on auth failure, submodule init, `rev-parse` tree/commit, temp-dir cleanup guard. | `cloneRepo`, `getGitTreeHash`, `getHeadCommit`, `cleanupTempDir`, `parseGitHubRepoUrl`, `isCommitSha`, `isMissingRefError` |
| `src/blob.ts` | GitHub Trees/Contents fast path and skills.sh snapshot downloads; lazy auth (anonymous → token → `gh api`); rate-limit memo; blob fallback reasons. | `tryBlobInstall`, `fetchRepoTree`, `fetchTreeCommit`, `getSkillFolderHashFromTree`, `toSkillSlug`, `BLOB_ALLOWED_REPOS` |
| `src/download-source.ts` | Download-then-extract pipeline with byte/entry limits and archive-type sniffing. | `downloadSource` |
| `src/archive.ts` | Hand-written ZIP reader (local headers, central directory, ZIP64, CP437/UTF-8 names, stored/deflate), rejecting unsafe paths, links, encryption, multi-disk. | `readZipArchive`, `ArchiveLimits` |
| `src/providers/wellknown.ts` | RFC 8615 discovery provider supporting v0.2 (single artifact + `sha256:` digest) and legacy v0.1 (`files[]`). | `wellKnownProvider`, `Well KnownProvider` class, `computeWellKnownSkillDigest`, `getWellKnownIndexPath` |
| `src/providers/{registry,types,index}.ts` | Provider interface, registration (duplicates rejected), re-exports. | `registry`, `registerProvider`, `findProvider`, `getProviders`, `HostProvider` types |
| `src/plugin-manifest.ts` | Claude-plugin `marketplace.json` / `plugin.json` discovery (local paths only, containment-checked). | `getPluginSkillPaths`, `getPluginGroupings` |
| `src/skills-field.ts` | Parser for the `package.json` `"skills"` field grammar (`npm:<pkg>`, git sources, `skills[]`, `ref`). | `parseSkillsField` |
| `src/skills-sh-config.ts` | `skills.sh.json` `paths` field reader. | `parseSkillsShPaths`, `getSkillsShSearchDirs` |
| `src/notion-test.ts` | Notion Agent Plugins via `ntn api` subprocess + pack download/extract. | `prepareNotionPackSource`, `prepareNotionSkillSource`, `downloadNotionUrl`, `isNotionSource`, `isNotionPackUrl`, `parseNotionSkillUrl` |
| `src/update-source.ts` | Build the re-fetch descriptor for a lock entry (git vs download vs notion). | `buildLocalUpdateSource` |
| `src/skill-relocation.ts` | Resolve where an installed skill actually lives (canonical dir, agent links, relocated names) for remove/update. | `resolveSkillLocations` |
| `src/concurrency.ts` | Bounded parallel map for source fetches. | `mapConcurrent`, `fetchConcurrency` |
| `src/color.ts` | Single source of truth for ANSI enablement and SGR constants; no raw escapes elsewhere (enforced by `color.test.ts`). | `pc`, `colorEnabled`, `RESET/BOLD/DIM/TEXT/CYAN/YELLOW`, `GRAYS`, `CLEAR_LINE` |
| `src/sanitize.ts` | Terminal-escape stripping (CSI/OSC/DCS/C1/control) and `sanitizeName` (kebab-case, path-traversal-proof, 255-char cap). | `stripTerminalEscapes`, `sanitizeMetadata`, `sanitizeName` |
| `src/diagnostics.ts` | The complete `nostics` error/warning catalog (`SKILLS_*`), three merged catalogs, ANSI formatter. | `diagnostics`, `gitCloneDiagnostics`, `rejectedArchiveDiagnostics`, `formatError`, `isDiagnostic`, `DiagnosticCode` |
| `src/warnings.ts` | Warning routing via `AsyncLocalStorage` so the API can capture what the CLI prints. | `warn`, `withWarnings` |
| `src/telemetry.ts` | Telemetry + security-audit HTTP clients, gating, flush. | `track`, `fetchAuditData`, `flushTelemetry`, `setVersion`, `setDetectedAgent` |
| `src/detect-agent.ts` | Wraps `@vercel/detect-agent`, adds stricter Cursor signal check, maps to agent ids, feeds telemetry. | `detectAgent`, `isRunningInAgent`, `getAgentName`, `getAgentType` |
| `src/ci.ts` | CI detection across 8 env vars. | `isCI` |
| `src/github-host.ts` | `GH_HOST` validation for GitHub Enterprise. | `getGitHubHost`, `isGitHubHost` |
| `src/cli-args.ts` | Subcommand option parsing on top of `node:util.parseArgs` (grouped short flags, `--flag=value`, list options, unknown-option reporting). | `parseCliArgs`, `throwFirstError` |
| `src/prompts/search-multiselect.ts` | Custom fzf-style multiselect over `readline`: search, groups, locked section, detail pane, width-aware redraw. | `searchMultiselect`, `approxStringWidth`, `visualRowsForLine`, `countVisualRowsForLines`, `cancelSymbol` |

### 3.4 Module dependency graph (module level, **observed** from imports)

```
bin/cli.mjs ─► dist/cli.mjs (bundled from src/cli.ts)

src/cli.ts ─┬─► add.ts ─────┬─► source-parser.ts ─► github-host.ts
            │                ├─► git.ts
            │                ├─► installer.ts ─► agents.ts ─► xdg-basedir
            │                ├─► skills.ts ─┬─► frontmatter.ts
            │                │              ├─► plugin-manifest.ts
            │                │              └─► skills-sh-config.ts
            │                ├─► blob.ts / providers/wellknown.ts / archive.ts
            │                ├─► download-source.ts
            │                ├─► local-lock.ts ─► skill-hash.ts
            │                ├─► skill-lock.ts
            │                ├─► skills-field.ts / skills-sh-hash.ts
            │                ├─► notion-test.ts (spawns `ntn`)
            │                ├─► telemetry.ts
            │                ├─► concurrency.ts
            │                └─► prompts/search-multiselect.ts
            ├─► find.ts ─────────► add.ts (delegates install)
            ├─► use.ts ──────────► blob.ts / git.ts / installer.ts / wellknown.ts
            ├─► remove.ts ───────► installer.ts / local-lock.ts / skill-lock.ts
            ├─► list.ts ─────────► installer.ts / local-lock.ts / skill-lock.ts
            ├─► update.ts ───────► add.ts / update-source.ts / skill-lock.ts
            ├─► install.ts ──────► add.ts / local-lock.ts / sync.ts
            ├─► sync.ts ─────────► add.ts / skills-field.ts / local-lock.ts
            └─► diagnostics.ts ◄──── every module (leaf-ish, depends on color.ts)

src/index.ts ─► api/{agents,resolve,install,list,remove}.ts ─► the same core modules
```

Layering rule **observed**: `diagnostics.ts`, `color.ts`, `sanitize.ts`, `constants.ts`,
`types.ts` are leaves. `add.ts` is the hub — `update`, `install`, `sync`, and the API all
import from it, so it cannot be split without touching those call sites.

### 3.5 Where the CLI entry sits relative to the library entry

Both entries are bundled in **one** obuild "bundle" entry so they share chunks
(**observed** `build.config.mjs` comment: separate bundles would overwrite each other's
tree-shaken copies in `dist/_chunks`).

```
src/cli.ts   ──► dist/cli.mjs    ──► bin/cli.mjs ──► `skills` / `add-skill` bin
src/index.ts ──► dist/index.mjs  ──► package exports "." (+ dist/index.d.mts types)
```

The library entry deliberately imports **no** CLI module and produces no output
(**observed** `tests/dist.test.ts` asserts importing `dist/index.mjs` prints nothing and
that the export key set is exactly nine names). `version` is duplicated on purpose:
`src/cli.ts` reads `package.json` at runtime (`0.0.0` fallback), while
`src/api/version.ts` imports it at build time (**observed**).

---

## 4. Third-party dependencies and exactly what each is used for

### Runtime (`dependencies`)

| Package | Use | Evidence |
| --- | --- | --- |
| `tar` ^7.5.22 | Extract `.tar`, `.tar.gz`, `.tgz` downloads with a `filter` callback that validates every entry path, counts bytes/entries, and ignores links. Not used for ZIP. | **observed** `src/download-source.ts#extractTar` |
| `yaml` ^2.9.1 | `parse` for frontmatter; `stringify` for `init` template and for rewriting `name:` on `--rename`. | **observed** `frontmatter.ts`, `cli.ts#runInit` |
| `nostics` ^1.4.0 | Typed diagnostic catalog: `defineDiagnostics`, `Diagnostic` class, `InferDiagnosticCodes`, `ansiFormatter`. Every user-facing error/warning is one of these. | **observed** `src/diagnostics.ts` |

### Bundled at build time but declared dev-only

| Package | Use | Evidence |
| --- | --- | --- |
| `@clack/prompts` | All interactive UI: `confirm`, `select`, `spinner`, `log`, `note`, `cancel`, `isCancel`. | **observed** `add.ts`, `remove.ts`, `sync.ts`, `install.ts`, `notion-test.ts` |
| `picocolors` | `createColors(enabled)` underlying `pc`; also `pc.green/red/dim` inside prompts. | **observed** `color.ts`, `prompts/search-multiselect.ts` |
| `simple-git` | Clone, `env()`, `cwd()`, `init/addRemote/fetch/checkout`, `submoduleUpdate`, with per-instance timeout and hardened `unsafe.*` allow-list. | **observed** `src/git.ts` |
| `xdg-basedir` | `xdgConfig` for agents whose global dirs follow XDG (Amp, Zed, …). Comment notes it was chosen over `env-paths` to match agent behaviour. | **observed** `src/agents.ts` |
| `@vercel/detect-agent` | `determineAgent()` to detect "we are running inside an agent" (suppresses banner/prompts) and to tag telemetry. | **observed** `src/detect-agent.ts` |

Transitive bundled packages named in the license script: `@clack/core`, `sisteransi`,
`is-unicode-supported` (**observed** `scripts/generate-licenses.ts`). Note
`@vercel/detect-agent` is bundled but **absent** from that `BUNDLED_PACKAGES` list — a
license-notice gap (**observed**).

### Dev-only tooling that shapes behaviour

| Package | Effect on shipped behaviour | Evidence |
| --- | --- | --- |
| `obuild` | Bundler/orchestration; single bundle entry; emits `dist/*.mjs` + `.d.mts`. | **observed** `build.config.mjs` |
| `tsnapi` | `ApiSnapshot` rolldown plugin: build fails when the public API of entry `.` changes; snapshot files committed. | **observed** `build.config.mjs`, `__snapshots__/tsnapi/` |
| `vitest` ^5 | Unit + integration + e2e runner. | **observed** `vitest.config.ts`, `e2e/vitest.config.ts` |
| `license-checker` | Generates `ThirdPartyNoticeText.txt` during `build`. | **observed** `scripts/generate-licenses.ts` |
| `typescript` ^6 | `tsc --noEmit` for `src` and `e2e`. | **observed** `type-check` script |
| `husky` + `lint-staged` | Pre-commit prettier on `src`, `scripts`, `tests`, `e2e`. | **observed** `.husky/pre-commit`, `package.json` |

---

## 5. Build and packaging

**Observed** `package.json`, `build.config.mjs`, `scripts/generate-licenses.ts`,
`e2e/package.e2e.ts`, `bin/cli.mjs`.

- `pnpm build` = `node scripts/generate-licenses.ts && obuild`.
- `obuild` runs a single `bundle` entry with inputs `./src/cli.ts` and `./src/index.ts`,
  using rolldown internally; output is `dist/cli.mjs`, `dist/index.mjs`, `dist/index.d.mts`
  and shared chunks (the config comment names `dist/_chunks`).
- Public-API guard: `ApiSnapshot({ entryFilter: entryName === '.' })` — only the library
  entry is snapshot; `pnpm api:update` (`UPDATE_SNAPSHOT=1`) is the sanctioned refresh;
  `pnpm api:update:breaking` also sets `TSNAPI_ALLOW_BREAKING=1`.
- `bin` exposes **two** names for the same shim: `skills` and `add-skill` → `./bin/cli.mjs`
  (legacy compatibility).
- The shim enables `module.enableCompileCache()` (Node 22.8+) unless
  `NODE_DISABLE_COMPILE_CACHE`, swallowing errors, then imports the bundle. This is a
  startup-latency optimisation only.
- Published `files`: `dist`, `bin`, `docs`, `README.md`, `ThirdPartyNoticeText.txt`.
  `e2e/package.e2e.ts` asserts that `src/`, `tests/`, `e2e/`, `scripts/`, `.github/`,
  `.automations/`, `.vc/` and all `.ts` files (except `.d.mts`) are **absent** from the
  tarball.
- The same test asserts runtime `node_modules` contains exactly the three `dependencies`
  and none of the bundled libraries.
- `prepublishOnly` = build. `publish:snapshot` bumps a prerelease and publishes under the
  `snapshot` dist-tag.
- `exports`: `"."` → `dist/index.mjs` with `dist/index.d.mts` types; `"./bin/cli.mjs"`;
  `"./package.json"`.
- Version reporting: `skills --version` prints the runtime-read `package.json` version
  (fallback `0.0.0`); the library exports the build-time imported `version`.
- License generation: `license-checker --json` over the installed tree, filtered to the
  bundled allow-list, with license text pulled from each package's `LICENSE*` file and a
  hard-coded MIT/ISC fallback. Skips (writes a placeholder notice) when `cwd` has no
  `package.json` — a workaround for `npx` runs from non-project directories.

---

## 6. Test strategy

Three layers, run by three different mechanisms (**observed**).

### 6.1 Unit / integration — vitest, colocated and in `tests/`

- Default `vitest` include picks up both `src/*.test.ts` (~20 files, colocated with
  implementation) and `tests/*.test.ts` (~70 files). `vitest.config.ts` only raises
  timeouts on Windows (30 s tests / 60 s hooks vs 5 s / 10 s) because spawns and
  filesystem work are slower there.
- No coverage provider or thresholds are configured → **no enforced coverage**
  (**observed**: `vitest.config.ts` has no `coverage` key).
- The `src/*.test.ts` files are mostly **CLI-subprocess tests**: `src/test-utils.ts#runCli`
  spawns `node src/cli.ts …` with an isolated temp `HOME`/`USERPROFILE`, sanitised agent
  env vars, sanitised CI env vars, `DISABLE_TELEMETRY=1`, and ANSI stripped from output.
- `tests/*.test.ts` are finer-grained: they import modules directly and mock at the
  boundary — `vi.mock('simple-git')` + `vi.mock('child_process')` in `src/git.test.ts`,
  `vi.mock('../src/git.ts')` in `tests/update.test.ts`, `vi.stubGlobal('fetch', …)` for
  HTTP (`find`, `blob`, `wellknown-update`), `vi.stubEnv` for env-dependent logic.
- Fixtures: `tests/fixtures/zip.ts` synthesises ZIP buffers (stored/deflate, encrypted flag,
  Unicode path extra field, Unix mode, ZIP64 extra) so the hand-written reader is tested
  without binary blobs. `src/test-utils.ts` builds temp homes with `XDG_*`,
  `APPDATA`/`LOCALAPPDATA`, `CODEX_HOME`, `CLAUDE_CONFIG_DIR`, etc.
- Snapshots are limited: inline snapshots in `src/init.test.ts` and `src/cli.test.ts`,
  plus the two committed `__snapshots__/tsnapi/` files guarding the library surface.

Representative coverage areas (file names, **observed**): symlink install/copy, nested
symlink escape, archive safety, subpath traversal, blob fallbacks and auth, well-known
integrity, lock hashing/races/migration, xdg and cross-platform paths, agent-specific path
tests (antigravity, grok, eve, kimchi, minimax-code, pi, posit-assistant, reasonix,
sarvam-code, zcode, fx), search prompt rendering/visual rows, telemetry endpoints,
private-repo security, `dist` build.

### 6.2 Production e2e — `e2e/`, separate config

- `pnpm test:e2e` → `vitest run --config e2e/vitest.config.ts`; include `**/*.e2e.ts` so it
  is excluded from `pnpm test`; `testTimeout` 180 s, `hookTimeout` 300 s, `retry: 1` in CI,
  `maxWorkers: 4` to stay under GitHub anonymous rate limits (**observed**).
- `e2e/setup.ts` (globalSetup): builds, `pnpm pack`, then `npm install` the tarball into a
  throwaway prefix with a fresh `HOME`, fresh npm cache, and no inherited npm config. Then
  exposes `skillsBin`, `installDir`, `tarball`, `npmCache` to tests. `SKILLS_E2E_PACKAGE`
  lets the same suite test a published version instead.
- `e2e/env.ts` is an **allow-list** of inherited env vars (PATH, proxies, CA bundles, git
  CA info, `GITHUB_TOKEN`/`GH_TOKEN`), with `DISABLE_TELEMETRY=1`, `NO_COLOR=1`,
  `GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`. This is how agent markers, XDG
  overrides, and CI markers are prevented from changing defaults.
- `e2e/sandbox.ts` gives each test its own `home/` and `project/` and runs the **installed
  bin** (never repo sources), stripping ANSI for substring assertions.
- Suites hit **live** sources: GitHub (`anthropics/skills`, `mattpocock/skills`,
  `vercel-labs/agent-skills`), skills.sh blobs, `https://vercel.com` well-known, npm
  registry. `notion.e2e.ts` stubs `ntn` on `PATH` and serves a local archive.
- Sandboxing: no network faking at all in e2e — the mitigation is per-test temp
  HOME/project plus a bounded worker count and one CI retry (**observed**; the "fake" is
  the local HTTP server used by blob/notion suites and the `ntn` stub).

### 6.3 Gaps visible from the outside (**observed** unless noted)

- No coverage measurement or gate.
- Windows is exercised in CI for unit tests (`windows-latest`) but **not** in e2e
  (ubuntu + macos only) — Windows symlink/junction behaviour is only covered by unit tests
  and, presumably, skipped paths (**assumed** for skipped branches; the matrix is
  observed).
- No 32-bit, aarch64, or armhf target anywhere in CI — a Go port targeting those has no
  behavioural oracle from this suite.
- Telemetry is always disabled in tests; the real endpoints are only asserted by URL
  construction (`tests/telemetry-endpoints.test.ts`) (**observed**).
- No tests for `skills use | claude` launching a real agent; `launchAgentInteractively` is
  tested with an injected spawn (**observed** signature `spawnImpl`).
- Release workflow has no automated post-publish smoke test against the published tarball
  (e2e runs pre-merge/daily) (**observed**).

---

## 7. Telemetry and security-audit requests

**Observed** `src/telemetry.ts`, `src/add.ts`, `docs/troubleshooting.md`.

### 7.1 Endpoints

| Purpose | Endpoint | Method |
| --- | --- | --- |
| Usage telemetry | `https://www.skills.sh/tele/t` | GET with query string |
| Partner security audit | `https://www.skills.sh/tele/audit` | GET with query string |

Search uses a separate host default `https://skills.sh` + `/api/search` (`SKILLS_API_URL`
overrides), and blob/snapshot downloads use `https://skills.sh` (`SKILLS_DOWNLOAD_URL`).

### 7.2 What is sent, and when

Every event sends common params first: `v` (CLI version, when initialised), `ci=1` (when
`isCI()`), `agent` (detected agent name, e.g. `claude`, when known).

| Event | Trigger | Fields |
| --- | --- | --- |
| `install` | after a successful install path (several call sites) | `source`, `skills`, `agents`, optional `global=1`, `skillFiles` (JSON `{skillName: repoRelativePath}`), `installUrl`, caller `metadata` (verbatim JSON from `--metadata`), `sourceType` |
| `remove` | after removal | `source` (optional), `skills`, `agents`, `global`, `sourceType` |
| `update` | end of `runUpdate` | `scope`, `skillCount`, `successCount`, `failCount` |
| `find` | after a search | `query`, `resultCount`, `interactive=1` |
| `experimental_sync` | end of sync | `skillCount`, `successCount`, `agents` |

No timestamps, no install paths, no usernames, no machine id, no hashed identifiers are
sent **by this client** (the server may hash server-side — **assumed**). There is no
hashing or anonymisation step in the CLI itself (**observed**: none present in
`telemetry.ts`).

Privacy gating before install telemetry: identifiers are attached only for repositories
GitHub positively confirms are public; when privacy cannot be determined the identifiers
are omitted (**observed** comments and `isSourcePrivate` usage; README and
`docs/troubleshooting.md` state the same policy).

### 7.3 Gating

- Hard off when `DISABLE_TELEMETRY` **or** `DO_NOT_TRACK` is set (any non-empty value).
- `fetchAuditData` also returns `null` immediately when disabled, and when the skill list is
  empty.
- The library treats `SkillsConfig.telemetry` as intent, but the env vars always win
  (**observed** doc comment in `api/types.ts`).

### 7.4 Failure handling

- `track()`: fire-and-forget `fetch` with an `AbortController`; errors swallowed; promises
  are queued so `flushTelemetry()` can await them.
- `flushTelemetry(timeoutMs = 5000)` races all pending requests against a timer, clears the
  timer, then aborts every controller so no socket outlives the flush. It is called in
  `main().finally(...)`; the CLI deliberately exits by draining the event loop rather than
  `process.exit()` (see the Windows libuv comment in `cli.ts`).
- `fetchAuditData`: 3 s `AbortSignal`-style timeout via `AbortController`, `!response.ok`
  → `null`, parse errors → `null`. Audits never block or fail an install.
- Audit response shape consumed: `Record<skillName, Record<partnerId, {risk, alerts?,
  score?, analyzedAt}>>`, with `ath`, `socket`, `snyk` partners rendered
  (`buildJsonSecurity`, `buildSecurityLines`) (**observed**).

---

## 8. CI, release mechanics, platforms, OS-specific paths

### 8.1 Workflows (**observed** `.github/workflows/`)

| Workflow | Trigger | Matrix / steps |
| --- | --- | --- |
| `ci.yml` | PR + push to `main` (ignores `**/*.md`) | matrix `ubuntu-latest`, `windows-latest` × Node `22.20.0`, `24`, `26`; each: `pnpm install --frozen-lockfile`, `pnpm build`, `pnpm test`; prettier check only on ubuntu+24; separate `typecheck` job on ubuntu/24 (`tsc --noEmit` for `src` and `e2e`) |
| `e2e.yml` | PR + push + daily cron `17 6 * * *` + manual | ubuntu 22.20, ubuntu 24, macos 24; 20 min timeout; `GITHUB_TOKEN` provided; `concurrency` cancel-in-progress per ref; manual input `package` (npm spec) |
| `publish.yml` | `workflow_dispatch` with `patch`/`minor` | install, build, import pinned GPG key, reset `ThirdPartyNoticeText.txt`, `npm version`, push + push tags, `npm publish --provenance --access public`, then create a GitHub Release whose notes are generated from merged PRs via `gh pr list` + `jq` |
| `agents.yml` | PR/push touching `src/agents.ts` | validate agents (duplicate display names etc.); on push to `main`, run `scripts/sync-agents.ts` and auto-commit README/docs/commands/creating-skills/package keywords |

### 8.2 Release mechanics

Version lives in `package.json` only; bumps are done by the workflow (`npm version`).
Provenance is published (`--provenance`), tags are signed, and there is no changesets-driven
versioning even though `.changeset/` exists (two pending patch entries are present; the
release path does not read them — **observed**: `publish.yml` never runs `changeset version`).

### 8.3 Supported platforms

- Runtime claims Node ≥ 22.20 on any platform Node supports; explicit CI coverage is Linux
  (x64) and Windows (x64) for tests, plus macOS for e2e (**observed**).
- No `os`/`cpu` restriction in `package.json`; no prebuilt binaries — pure JS
  (**observed**).
- macOS app-bundle detection exists as a detection signal only (`/Applications/ZCode.app`,
  `/Applications/MiniMax Code.app`) (**observed** `agents.ts`).

### 8.4 OS-specific code paths (**observed**)

1. `installer.ts#createSymlink`: on `win32`, use a **junction** with an absolute target;
   elsewhere a **relative** symlink computed from the realpath of the link's parent
   directory. `realpath` checks short-circuit when target and link are already the same
   physical path (to avoid deleting the canonical dir).
2. `local-lock.ts#renameWithWindowsRetry`: 5 attempts with 15 ms × attempt backoff on
   `EPERM`/`EBUSY` (Windows-only); 1 attempt elsewhere.
3. `local-lock.ts#getPortableLocalSource`: falls back to an absolute forward-slashed path
   when the relative path would cross Windows drives.
4. `git.ts`: always sets `core.longpaths=true` (MAX_PATH), plus LFS filter disabling and
   `LC_ALL=C`; comment cites downstream Windows reports.
5. `agents.ts`: `reasonix` global dir uses `%APPDATA%\reasonix` on win32 vs `~/.reasonix`;
   `zed` detection also probes `%APPDATA%\Zed`.
6. `blob.ts`: `execFile('gh', …)` with `windowsHide: true`.
7. `cli.ts`: no `process.exit()` on the main path — Windows libuv
   `UV_HANDLE_CLOSING` assertion risk.
8. Tests: `shell: isWindows` for spawns; `USERPROFILE` always set alongside `HOME`;
   vitest timeouts raised on Windows.

---

## 9. Concurrency model

- **Bounded source fan-out**: `mapConcurrent(items, fn, limit = fetchConcurrency())`,
  default 4, override `SKILLS_CLONE_CONCURRENCY` (positive integer only; invalid values fall
  back to 4). Used for cloning/downloading multiple sources and for batched per-skill
  operations (**observed** `concurrency.ts`, call sites in `add.ts`/`update.ts`).
- **Unbounded intra-directory parallelism**: `copyDirectory`, `collectFiles`
  (`skill-hash.ts`), `computeLegacySkillFolderHash`, and `detectInstalledAgents` all use
  `Promise.all` over directory entries or agents without a cap (**observed**). On a large
  skill tree this can open many file handles at once — a plausible EMFILE source
  (**assumed**, no backpressure observed).
- **Cross-process lock serialisation**: `withLocalLock` creates `<lock>.lock` with
  `open(..., 'wx')`, polls every 10–25 ms, breaks a stale lock (mtime older than 5 s) by
  renaming it to a unique `.stale` name and re-linking if it turned out to be live, gives up
  after 10 s with `SKILLS_LOCAL_LOCK_BUSY` (**observed**). In-process serialisation is the
  same mutex file, so it is also process-safe by construction.
- **Telemetry**: one in-flight `fetch` per event, no pooling; flushed once at exit with a
  5 s ceiling.
- **Prompts** are strictly sequential; the CLI is single-threaded apart from the above
  fan-out. No worker threads, no child processes except `git`, `gh`, `ntn`, and the agent
  launch in `use`.
- e2e caps vitest workers at 4 to protect GitHub rate limits (**observed**).

---

## 10. Error-handling conventions

- **One catalog**: every user-facing error and warning is a `nostics` diagnostic with a
  permanent `SKILLS_<WHAT_WENT_WRONG>` code declared in `src/diagnostics.ts`. Three
  catalogs (`baseDiagnostics`, `gitCloneDiagnostics`, `rejectedArchiveDiagnostics`) are
  merged into `diagnostics`, whose `is()` matches any (**observed**).
- Codes carry `why` (user message) and optional `fix` (separate guidance line), plus
  optional structured `data` used by warning consumers. Codes may be parameterised;
  changing parameters is a public-API change because the catalog is exported.
- Throwing a diagnostic object is normal control flow (`throw diagnostics.SKILLS_X({...})`).
  Matching is always `diagnostics.SKILLS_X.is(error)` or `catalog.is(error)`; hand-comparing
  `.code` is forbidden by `AGENTS.md` (**observed**).
- Printing: `formatError(error)` renders a diagnostic with `nostics`' ANSI formatter; plain
  `Error`s render `error.message`; anything else `String(error)`.
- Warnings escalate through `warn()` → `AsyncLocalStorage` handler → `console.warn` in the
  CLI, `onWarning` in the API (dropped silently when no handler). Used for skipped
  `SKILL.md`s and unsafe symlinks.
- **Exit codes**: `0` success; `1` for usage errors, unknown command, failed installs,
  failed lock writes, update failures/outdated skills in `check`, frozen-lock violations.
  `process.exitCode = 1` is preferred so the event loop can drain; `process.exit(code)` is
  used only where control flow must not continue (JSON mode, prompt cancellation, failed
  agent launch propagation). Prompt cancellation from a non-TTY exits 1 with
  `SKILLS_PROMPT_REQUIRES_TTY`; from a TTY it exits 0 (**observed**
  `exitInstallationCancelled`).
- **Lock write failures** after a successful install are reported
  (`SKILLS_LOCK_WRITE_FAILED`) and set exit code 1, but the files stay installed.

---

## 11. Logging and output conventions

- `console.log` for stdout, `console.error` for errors, `console.warn` for warnings; no
  logging framework.
- **ANSI is centralised** in `src/color.ts`: `NO_COLOR` or `--no-color` disable;
  `FORCE_COLOR`, `--color`, or `CI` enable; otherwise stdout must be a TTY with
  `TERM != dumb`. picocolors' "always on for win32" shortcut is deliberately removed (would
  leak escapes into pipes on Windows). Cursor/line control (`CLEAR_LINE`) is TTY-only
  regardless of colour. `color.test.ts` forbids raw `\x1b[` literals elsewhere.
- **Untrusted text is sanitised** before printing: `stripTerminalEscapes` (CSI, OSC, DCS/PM/
  APC, C1, control chars except `\t`/`\n`) and `sanitizeMetadata` (also collapses newlines)
  for names/descriptions/sources/paths.
- The **logo** is a 6-line box-drawing banner printed only when stdin is not an agent
  session; help text aligns example comments by measuring string length (no width-aware
  padding) (**observed**).
- **`--json` contract**: `add --json` redirects `process.stdout.write` to `stderr` for the
  whole run, then restores it and prints exactly one JSON array; a `process.once('exit')`
  safety net guarantees one parseable array even on unexpected exits; errors print
  `[]` with exit 1 when nothing was attempted (**observed**). `list --json` emits a JSON
  array; `find` has no JSON mode (**observed**).
- Progress: `@clack/prompts` spinners and notes; a `silentSpinner` stand-in keeps
  non-interactive paths quiet (**observed** `add.ts`).

---

## 12. Configuration, precedence, and files

### 12.1 Precedence (library, **observed** `src/api/config.ts`)

```
call options  >  options.config (SkillsConfig)  >  defaults
defaults: scope=project, mode=auto, lockfile=true, fullDepth=false,
          includeInternal=false, duplicates=first, symlinks=follow,
          telemetry=true, agents={}
```
`cwd` defaults to `process.cwd()`. The doc comment reserves a slot between
`options.config` and defaults for a future on-disk `skills.config`; the library never reads
such a file **today**.

### 12.2 Precedence (CLI)

1. Flags on the command line (see `skills --help`: `-g/-a/-s/-l/-y/--copy/--rename/
   --metadata/--subagent/--all/--full-depth/--json`, plus `experimental_install` and
   `experimental_sync` flags).
2. Environment variables (table below).
3. Files: project `skills-lock.json` (project scope), global
   `XDG_STATE_HOME/skills/.skill-lock.json` or `~/.agents/.skill-lock.json` (global scope,
   also holds `lastSelectedAgents` and `dismissed` prompts), repo `skills.sh.json`,
   `package.json` `skills` field (sync only), plugin manifests.
4. Built-in defaults (e.g. scope project, `-g` flips to global; install mode `symlink` in
   `add`, but auto-downgraded to `copy` when every target shares one skills dir).

Special rule **observed**: `--frozen-lockfile` defaults to `isCI()` for
`experimental_install` and `experimental_sync`; `--no-frozen-lockfile` overrides.

### 12.3 Environment variables (all **observed**)

| Variable | Effect |
| --- | --- |
| `DISABLE_TELEMETRY`, `DO_NOT_TRACK` | disable telemetry + audit |
| `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `CIRCLECI`, `TRAVIS`, `BUILDKITE`, `JENKINS_URL`, `TEAMCITY_VERSION` | CI detection → frozen lockfile default, telemetry `ci=1`, colour enabled |
| `NO_COLOR`, `FORCE_COLOR` | colour off / on |
| `DEBUG` | not used; `notion-test.ts` uses a `[notion]` `console.error` line unconditionally for `ntn` invocations (**observed**) |
| `INSTALL_INTERNAL_SKILLS` | `1`/`true` reveals `metadata.internal` skills |
| `GITHUB_TOKEN`, `GH_TOKEN` | explicit GitHub API token (never read from `gh` storage) |
| `GH_HOST` | GitHub Enterprise hostname (validated; falls back to `github.com`) |
| `GIT_SSH_COMMAND` | inherited, defaulted to `ssh -o BatchMode=yes` on the SSH retry |
| `SKILLS_CLONE_CONCURRENCY` | source fetch fan-out (default 4) |
| `SKILLS_CLONE_TIMEOUT_MS` | clone timeout (default 300 000) |
| `SKILLS_DOWNLOAD_MAX_BYTES` | download cap (default 10 MiB) |
| `SKILLS_EXTRACT_MAX_BYTES` | extraction cap (default 25 MiB; well-known/Notion use their own larger caps) |
| `SKILLS_EXTRACT_MAX_FILES` | extraction entry cap (default 1000) |
| `SKILLS_API_URL` | search base (default `https://skills.sh`) |
| `SKILLS_DOWNLOAD_URL` | blob/snapshot base (default `https://skills.sh`) |
| `XDG_CONFIG_HOME`, `XDG_STATE_HOME` | agent global dirs and global lock location |
| `CODEX_HOME`, `CLAUDE_CONFIG_DIR`, `VIBE_HOME`, `HERMES_HOME`, `AUTOHAND_HOME`, `GROK_HOME`, `SARVAM_HOME`, `REASONIX_HOME` | per-agent home overrides |
| `APPDATA`, `FLATPAK_XDG_CONFIG_HOME` | Windows Zed / Flatpak Zed detection |
| `NODE_DISABLE_COMPILE_CACHE` | skip the bin shim's compile cache |
| `SKILLS_E2E_PACKAGE`, `SKILLS_E2E_KEEP` | e2e only |
| `UPDATE_SNAPSHOT`, `TSNAPI_ALLOW_BREAKING` | build-time only |

### 12.4 On-disk layout the tool owns

```
<project>/.agents/skills/<sanitized-name>/…      canonical project install
<project>/<agent.skillsDir>/<sanitized-name>     symlink (junction on Windows) or copy
<project>/skills-lock.json                       project lock, v2, sorted keys
<project>/skills-lock.json.lock                  transient mutex
$HOME/.agents/skills/<sanitized-name>/…          canonical global install (XDG variant for some agents)
$HOME/.agents/.skill-lock.json                   global lock, v3
$XDG_STATE_HOME/skills/.skill-lock.json          alternative global lock
$TMPDIR/skills-*                                 clones, downloads, use materialisations
```

---

## Open questions

1. Does `obuild` externalise only `dependencies` (making every devDependency bundled)? The
   e2e assertions imply yes, but the bundler's config does not state it.
2. `dist/_chunks` is named only in a comment; the exact multi-entry chunk naming and whether
   `dist/index.mjs` re-exports from a shared chunk is unverified.
3. Is `@vercel/detect-agent` intentionally omitted from `BUNDLED_PACKAGES`, or is the
   license notice simply stale?
4. Is the `.changeset/` directory vestigial, or is a changesets release path planned
   alongside the manual `publish.yml` bump?
5. `tests/` and `src/` tests overlap in intent; is there a documented policy for which layer
   a new test belongs to (AGENTS.md lists only a subset of `tests/`)?
6. Which agents' `globalSkillsDir` values are verified against the vendor's real behaviour,
   and which are best-effort? `scripts/sync-agents.ts` updates docs but does not validate
   paths.
7. Is the `skills use` agent launch intended to grow beyond `claude`, `codex`,
   `sarvam-code`?
8. Does the server side of `tele/audit` ever return partner keys other than `ath`, `socket`,
   `snyk`, and is the client tolerant of unknown keys?
