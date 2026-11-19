# 06 — Package/`node_modules` integration, `experimental_sync`, `experimental_install`, and the public API

Behavioral reference for `vercel-labs/skills` v1.9.0 (MIT), reconstructed from source, tests and docs.
Purpose: enough precision to reimplement the behavior in Go without re-reading TypeScript.

Evidence convention: **(observed)** = read directly in source/tests (file:line at the end);
**(assumed)** = inferred from library semantics or an unexercised code path, not verified by running.

Companion doc: `07-skill-format-and-discovery.md` covers `SKILL.md` parsing, `sanitizeName`, source
discovery (`discoverSkills`), `skills.sh.json`, and plugin manifests in full. This doc covers only the
package-side integration and the two experimental commands.

---

## 1. Command surface

| Command | Aliases | Handler | Notes |
|---|---|---|---|
| `skills experimental_sync` | `sync` | `runSync(restArgs, parseSyncOptions(restArgs).options)` | `sync` is a compatibility alias for skills@1.7.3 users. Help/banner advertise only the long name. |
| `skills experimental_install` | — | `runInstallFromLock(restArgs)` | Restores `skills-lock.json`. |
| `skills install` | `i` | `runInstallFromLock` **only when no source argument** | With a source, `install`/`i` behave exactly like `add`. |
| `skills add` / `a` | — | `runAdd` | Never restores. |

Evidence: `src/cli.ts:368-418`, `:156-166`, docs `commands.md:389-396`.
Tests assert help shows `experimental_sync ... (alias: sync)` and does **not** advertise a bare `sync`
line (`tests/sync.test.ts:1305-1318`). **(observed)**

Both commands are documented as previews whose names/options/behavior may change in any release
(`README.md:66-84`). **(observed)**

### 1.1 Exit-code conventions shared by both

- Success is `exitCode 0`; the process is never hard-exited on an ordinary failure.
- Failures set `process.exitCode = 1` and usually **continue** with the remaining work (partial success
  is the norm).
- Two exceptions hard-exit: an invalid `--agent` value in sync calls `process.exit(1)` immediately
  (`src/sync.ts:766-776`), and a cancelled agent prompt calls `process.exit(0)` (`:803-806`).
- An invalid `skills-lock.json` is fatal for both commands and for `update`/`add` project installs; the
  CLI's top-level handler prints `Error: [SKILLS_INVALID_LOCAL_LOCK] …` on **stderr** and sets exit 1.
  `list` only warns and continues. (`src/cli.ts:448-459`, `tests/invalid-local-lock.test.ts:83-102`)
  **(observed)**

---

## 2. `experimental_sync` — flags, defaults, pipeline

### 2.1 Exact flag set

Parsed by `parseSyncOptions` (`src/sync.ts:1078-1107`) with the generic parser in `src/cli-args.ts`.

| Flag | Type | Becomes |
|---|---|---|
| `-y, --yes` | boolean | `yes = true` |
| `--copy` | boolean | `copy = true` |
| `--dry-run` | boolean | `dryRun = true` |
| `--no-cleanup` | boolean | `cleanup = false` (there is **no** `--cleanup`) |
| `--no-remote` | boolean | `remote = false` (there is **no** `--remote`) |
| `--frozen-lockfile` | boolean | `frozenLockfile = true` |
| `--no-frozen-lockfile` | boolean | `frozenLockfile = false` |
| `-r, --recursive` | boolean | `recursive = true` |
| `-a, --agent <names…>` | list | `agent[]` |
| `--include <patterns…>` | list | `include[]` |
| `--exclude <patterns…>` | list | `exclude[]` |

Parser semantics that matter **(observed)**, `src/cli-args.ts:5-14`, `:76-101`:
- A `list` option in space form swallows every following positional until the next option
  (`--agent claude-code cursor` → two values). The `=` form takes exactly one (`--agent=a source`
  leaves `source` positional).
- `--bool=value` is an error (`SKILLS_OPTION_TAKES_NO_VALUE`); a list option with no value is an error
  (`SKILLS_OPTION_REQUIRES_VALUE`). Both are thrown; the CLI prints them and sets exit 1.
- Unknown options and stray positionals are collected and ignored — so `experimental_install --force`
  and `sync --whatever` do not fail.

Defaults when unset: `yes=false`, `copy=false`, `dryRun=false`, `cleanup` treated as enabled,
`remote` treated as enabled, `recursive=false`, `agent/include/exclude` undefined, and
`frozenLockfile` falls back to `isCI()`.

### 2.2 CI detection

`isCI()` is true when any of these environment variables is a **non-empty string**:
`CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `CIRCLECI`, `TRAVIS`, `BUILDKITE`, `JENKINS_URL`,
`TEAMCITY_VERSION`. (`src/ci.ts:1-12`) **(observed)**

`frozen = options.frozenLockfile ?? isCI()` (`src/sync.ts:691`). Any non-empty value counts, so
`CI=0` and `CI=false` both enable frozen mode; only unset/empty means off. **(observed)**

### 2.3 Pipeline, in exact order

`runSync` (`src/sync.ts:637-1076`), with `cwd = process.cwd()`:

1. **Agent auto-detection.** `detectAgent()` (from `@vercel/detect-agent`, with a stricter Cursor
   check). If running inside an agent: force `yes = true`, and if no `--agent` was given, map the host
   agent name to an agent id and append **all universal agents** to it. (`:640-654`,
   `src/detect-agent.ts:12-92`) **(observed)**
2. **Discovery** (`:671-680`). `discoverNodeModuleSkills(cwd, recursive)`. Any `discovery.errors`
   (only ever produced by the *project's own* `skills` field) aborts the whole run: message
   `Fix the "skills" field in package.json and run sync again.`, exit 1, nothing installed, nothing
   pruned.
3. **Filtering** (`:681-688`): `discoveredSkills` filtered by `--include/--exclude`; remote requests
   filtered by `filterRemoteRequests`. Dependency-level warnings are printed here.
4. **Read the project lock** (`:690`). An invalid lock throws (fatal, see §1.1). `frozen` computed here.
5. **Cleanup of stale sync-owned skills** (`:692-717`) unless `--no-cleanup` — see §8.
6. **Remote planning** (`:719-739`): requests already installed are dropped; "cached" remote skills
   (present in the lock and on disk but missing agent links) are collected.
7. **Early exit** if there is nothing at all to install (`:741-748`, dim `Nothing to sync.`).
8. **Print discovered skills** (`:751-756`), name + `from <package>` + indented description.
9. **Agent selection** (`:758-809`) — see §9.1. `--dry-run` does **not** skip this step, so a dry run
   without `-y` can still prompt.
10. **Conflict resolution** (`:811-834`) — see §6, §7.
11. **Frozen lock validation** for packaged skills (`:835-845`) — see §11.
12. **Nothing-to-do exit** (`:846-850`, yellow `Nothing to sync.`).
13. **Summary note** (`:852-875`), then `--dry-run` returns here (`:877-880`).
14. **Confirmation** (`:882-889`) unless `--yes`; cancel → exit 0.
15. **Install** packaged skills and cached remote links (`:891-943`).
16. **Write lock entries** for packaged skills that had at least one successful agent (`:945-970`).
17. **Install remote `skills`-field sources** (`:972-1024`).
18. **Report** (`:1026-1062`), telemetry (`:1064-1070`), outro.

`runSync` takes an `args` parameter that it never reads (`:637`): option parsing happens in the CLI
wrapper. **(observed)**

---

## 3. Package discovery

### 3.1 Which `package.json` fields are read

From the project (and, with `-r`, each workspace package) (`src/sync.ts:116-126`, `:234-239`):

| Field | Use |
|---|---|
| `name` | Only for workspace packages without a name fallback (display/`via`). Not used for the root project (which is `.`). |
| `version` | Recorded as `version` in the lock entry for skills the package ships. |
| `workspaces` | `string[]` or `{ packages: string[] }`; used only with `-r`. |
| `dependencies`, `devDependencies`, `optionalDependencies`, `peerDependencies` | **Only the key names** are read; version specifiers are ignored entirely. All four are scanned, in that order. |
| `skills` | The skills-pack field, see §4. |

Missing/unreadable `package.json` at `cwd` → discovery returns empty lists immediately; sync then
treats the project as having no packaged skills (and **prunes** sync-owned lock entries, see §8).
(`:290-291`, test `tests/sync.test.ts:187-192`) **(observed)**

`package.json` is read as UTF-8 JSON; any parse failure is swallowed and treated as "no package.json".
(`:128-134`) **(observed)**

### 3.2 Which packages get scanned

Only **declared** packages are looked at:

1. the project itself (always, `recursive` or not);
2. with `-r`, each workspace package declared by `package.json#workspaces` and/or
   `pnpm-workspace.yaml#packages`;
3. packages reached through `npm:` entries in any scanned package's `skills` field (transitively,
   cycle-safe).

A package that merely sits in `node_modules` and is not declared is never scanned. A transitive
package becomes visible only when a direct dependency's `skills` field names it (`npm:`), or when the
declaring direct dependency is itself a workspace member scanned with `-r`.
(`src/sync.ts:241-249` comment, `:313-330`, test `:142-151`) **(observed)**

Workspace resolution (`findWorkspacePackages`, `:250-275`):

- Patterns = `package.json#workspaces` (array form, or `.packages`) **plus**
  `pnpm-workspace.yaml#packages` string entries (merged, not overridden).
- `!pattern` entries are removed from the inclusion set and used as exclusions matched against the
  workspace directory relative to `cwd`.
- Each included pattern is turned into `<pattern>/package.json` and globbed with `cwd` as base,
  excluding any path whose basename is `node_modules`.
- Only directories other than `.` are kept; the result is deduplicated and sorted.
- Workspace display name = that package's `name`, else the `/`-joined relative path.

### 3.3 `node_modules` resolution

`findInstalledPackage(from, name)` (`:173-183`) walks upward from `from`:

```
for dir = from, dirname(from), … root:
    candidate = <dir>/node_modules/<name>
    if exists(<candidate>/package.json) → candidate
```

Consequences **(observed)**:
- Hoisted/monorepo layouts work: a workspace package's dependency may resolve in the repo root's
  `node_modules`.
- pnpm's isolated layout works: a package's own nested `node_modules` (or the `.pnpm` store reached
  through the symlinked package directory) is found first, because the walk starts at the declaring
  package's directory. Tests: `tests/sync.test.ts:163-177` (symlinked package), `:739-755`, `:1221-1246`.
- The doc comment says "realpath, then walk up" (`:173-176`), but the implementation does **not**
  realpath `from` — only `existsSync` follows the final symlink. For pnpm this is equivalent in
  practice because the candidate's `package.json` is stat-ed through the symlink. **(observed code,
  assumed equivalence for exotic layouts)**
- The resolved path is *not* realpath'd before use, so a skill's `path` may contain a symlinked prefix.
  Deduplication (`add`, `:295-302`) and hashing later use `realpath`, so identity is by real target.
- Scoped names (`@acme/tools`) are handled naturally by path joining.
- No `exports`/`files` awareness, no `require.resolve`, no version check against the specifier.
- `peerDependencies` are scanned like normal deps, so a peer that is not installed is simply skipped.

### 3.4 How a package's skills are found

`discoverPackageSkills(pkgDir, packageName, depth)` (`:136-171`):

1. Read `<pkgDir>/package.json`; absent → `[]` (treated as "not installed").
2. **Root skill**: if `<pkgDir>/SKILL.md` parses, return **only** that one, with
   `skillPath = "SKILL.md"`. The `skills/` and `dist/skills/` directories are *not* consulted.
   Short-circuit behaviour confirmed by `tests/sync.test.ts:86-93` and `:214-222`. **(observed)**
3. Otherwise scan, in this order, `skills/` then `dist/skills/`; for each **direct child directory**
   with a `SKILL.md`, parse it. `skillPath` is `"<dir>/<child>/SKILL.md"`.
   Both directories are always scanned (no early exit after `skills/`). Tests `:95-105`. **(observed)**
4. Nothing else: no `examples/`, no `.agents/skills/`, no arbitrary-depth walk, no `skills.sh.json`,
   no Claude plugin manifests, no symlink-following beyond what `readdir` + `stat` on the child does.
   Test `:153-161`; absence of imports in `src/sync.ts:1-44`. **(observed)**

Parsing rules are those of `parseSkillMd` with **no options**: `metadata.internal: true` skills are
skipped unless `INSTALL_INTERNAL_SKILLS=1`/`true` is set globally. See doc 07 §2.4, §9. Skills with
invalid/missing frontmatter are skipped with a warning (never fatal). **(observed)**

`packageVersion` comes from that package's own `package.json#version`, and is omitted from the lock if
the package has no version. (`:149`, `:164`) **(observed)**

### 3.5 Depth model (conflict relevance)

`depth` is a small integer used only for conflict precedence:

| Situation | depth |
|---|---|
| Project's own declared `dependencies`/etc. shipped skills | `0` |
| Project's own `skills` field → `npm:<pkg>` shipped skills | `0` |
| Workspace package's declared deps | `1` |
| Workspace package's `skills` field → `npm:<pkg>` | `1` |
| A package reached through `npm:` from a declarer at depth *d*, its own shipped skills | *d* (not *d+1*) |
| A package reached through `npm:` from a declarer at depth *d*, its own `skills` field targets | *d+1* |

The last two rows are the literal implementation: `discoverPackageSkills(target, pkg, declarer.depth)`
but `declarers.push({ …, depth: declarer.depth + 1 })` (`:370`, `:381`). So a skill shipped by an
`npm:` target ranks at the same level as the declarer's own direct dependencies, while a *further*
`skills` hop ranks one deeper. **(observed)**

A `skills` field entry that is a **git** source has no depth at all; it is handled in a separate
remote phase where any packaged skill beats it (§9.3).

---

## 4. The `skills` array in `package.json`

Grammar (mirrors https://github.com/antfu/skills-npm SPEC, `src/skills-field.ts:6-34`):

```
"skills": [
  "npm:<package>",
  "npm:<package>",                      // may be repeated with different entries
  { "source": "npm:<package>", "skills": ["<skill>", …] },
  "owner/repo[@skill][#ref]",
  { "source": "owner/repo", "ref": "v1", "skills": ["<skill>", …] },
  "https://gitlab.com/group/repo/-/tree/main/skills"
]
```

Validation, per entry, in order (`src/skills-field.ts:50-80`):

1. Entry must be a string, or an object with a string `source` and (optionally) a string-array
   `skills` and a string `ref`. Anything else → error `SKILLS_PACKAGE_SKILLS_INVALID_ENTRY`
   (`"<declarer>: invalid "skills" entry <JSON>"`) and the entry is dropped. Unknown extra keys are
   tolerated. Test `tests/skills-field.test.ts:79-90`. **(observed)**
2. Source starting with `npm:`:
   - `ref` present → error `SKILLS_PACKAGE_SKILLS_REF_NOT_ALLOWED`
     (`"ref" cannot be used with "npm:x"`);
   - else → an npm request `{ package: <rest>, skills }` (empty `skills` = all).
3. Any other source is parsed by the normal source parser; only `github`, `gitlab`, `git` types are
   accepted. A local path or plain URL → `SKILLS_PACKAGE_SKILLS_NOT_GIT_SOURCE`.
   Test `:47-56`. **(observed)**
4. If the source string itself already carries a ref/`@skill` and the object also has `ref` →
   `SKILLS_PACKAGE_SKILLS_DUPLICATE_REF`.
5. A `@skill` suffix on the source is folded into the requested `skills` array, **appended after** the
   object's own `skills` (`:70`). Test `:21-45`. **(observed)**

`npm:` names are matched against installed packages by `findInstalledPackage`, not against the
`dependencies` map — so the package does not need to be declared anywhere, only installed.
Test `tests/sync.test.ts:729-737`. **(observed)**

### 4.1 How project and dependency declarations combine

- The project's own field and every scanned dependency's field are read **independently** and their
  results are merged into one pool (`discoverNodeModuleSkills`, `:331-383`).
- A dependency's `skills` field is only read if that package is itself a declarer, i.e. a direct
  dependency of the project (or of a workspace package with `-r`), or an `npm:` target.
- Identical remote requests (same `url`, `ref`, `subpath`, and requested-skill list) are merged into
  one request that records every declaring package; `via` becomes the first surviving declarer.
  (`:349-358`, `:187-213`) **(observed)**
- Requests to the same URL with *different* `skills` subsets stay separate requests; both are fetched
  and the first one to claim a name wins (§9.3).
- For `npm:` targets, the requested skill names are matched against **either** the skill's folder
  basename **or** its sanitized name (`:371-380`). So `skills: ["My Skill"]` and
  `skills: ["my-skill"]` both select the same folder.
- Every skill obtained through an `npm:` request carries `via = <the declaring package's name>`
  (`.` for the project).

### 4.2 Error vs warning split

| Declarer | Problem | Result |
|---|---|---|
| Project (`.`) | `skills` not an array | `SKILLS_PACKAGE_SKILLS_NOT_ARRAY`, **aborts sync** |
| Project | invalid entry / not a git source / duplicate ref / ref on npm / unresolved `npm:` | error → **aborts sync** with exit 1 and nothing installed |
| Dependency / workspace | same problems | warning printed, run continues |
| Dependency | `skills` present but not an array | **silently ignored** (no warning) |

Evidence `src/sync.ts:337-348`, `:359-369`; tests `:805-845`. **(observed)**

Warnings are formatted as `[CODE] message` on stderr (`formatError`), while sync's own progress goes
to stdout. `tests/invalid-local-lock.test.ts:99` shows the warning prefix form. **(observed)**

---

## 5. `--include` / `--exclude` pattern grammar

Grammar (`src/sync.ts:62-89`):

```
<packageGlob>                 → every skill of matching packages
<packageGlob>#<skillGlob>     → matching skills of matching packages
```

- Split on the **first** `#`; package names cannot contain `#`, so the split is unambiguous.
- Both halves are matched with `path.posix.matchesGlob` (Node's glob matcher). Package names are
  matched as-is; skill names are matched **after `sanitizeName`** (lowercase, runs of illegal
  characters → `-`, no leading/trailing `.`/`-`).
- A bare pattern therefore matches **package** names only; it never matches a skill name.
  Test `tests/sync.test.ts:666-670`. **(observed)**
- `*` does not cross `/`; `**` does. Hence `@acme/*` matches `@acme/tools` but `@acme/**` is the
  broader form used in tests (`:634-677`). **(observed from tests)**
- No `#` → skill pattern is `**` (all skills).

Semantics:
- `--include` non-empty ⇒ only matching skills survive; empty/absent ⇒ everything is included.
- `--exclude` always wins over `--include` (same skill matching both is excluded).
  Test `:672-677`. **(observed)**
- Multiple `--include` values are OR-ed; multiple `--exclude` values are OR-ed.
- Applies identically to packaged and remote skills. For remote entries the *package* half is matched
  against the **declaring package** name (`.` for the project's own field), never the git source name
  (`:187-213`, docs `commands.md:378-380`). Test `:940-958`. **(observed)**
- `--exclude <pkg>#<skill>` filters the skill out of a request but keeps the request; `--exclude <pkg>`
  drops the declarer entirely (and, if no declarer is left, the request). (`:200-212`) **(observed)**

Side effect worth knowing: because the "keep" set for cleanup is built from the **filtered** discovery
result, excluding a skill that sync installed in an earlier run **deletes it** on the next run (unless
`--no-cleanup`). Test `:679-687`. **(observed)**

---

## 6. Ownership, conflict and shadowing rules

### 6.1 "Did sync install this?" — the two predicates

`installedBySync(entry)` is true when the lock entry has `sourceType === "node_modules"` **or** a
`via` field. Everything else (normal `skills add` installs) is "explicit". (`src/sync.ts:392-395`)
**(observed)**

`foreignDestination(dest, canonicalDir, lockEntry, cwd)` decides whether sync may touch a path
(`:397-421`). It returns `null` ("free or ours") when:

- `dest` does not exist; **or**
- `dest` is a symlink whose resolved target is under any `node_modules` segment, or equals the
  canonical dir; **or**
- `dest` is a real directory **and** `installedBySync(lockEntry)`.

Otherwise it returns a human reason: `"<dest> is a symlink to <target>"` or
`"<dest> already exists and was not installed by sync"`. Paths are shortened with `~` for the home
directory and `.` for `cwd` in messages (`:94-103`). **(observed)**

Consequences:
- A hand-written skill directory is never replaced, even if a dependency ships the same name.
- A symlink pointing anywhere else is never replaced.
- A real directory that a previous `sync --copy` created (and that the lock tracks as node_modules) is
  fair game.
- `node_modules` detection is a **path-segment** test (`path.split(sep).includes('node_modules')`),
  so any directory literally named `node_modules` on the symlink target path qualifies
  (`:388-390`). **(observed)**

### 6.2 Precedence, exactly as implemented

`resolveConflicts` (`:519-568`) groups discovered skills by **sanitized name** and then, per name:

1. Compute `depth = min(depth of all candidates)`.
2. **All candidates with a larger depth are skipped** with reason
   `"<winner.packageName> is closer to the project"`. This is the "direct beats transitive" rule, and
   it also makes the root project beat a workspace package (`-r`). Tests `:793-803`, `:1248-1269`.
3. If **more than one** candidate remains at that minimum depth, **none** is installed. Each is skipped
   with reason `"shipped by <pkgA>, <pkgB>; drop this one with --exclude <own-pkg>#<name>"` — the
   package list is comma-joined in iteration order and is identical for every candidate, while the
   `--exclude` suggestion names the candidate's **own** package (so the user sees one suggestion per
   package). Tests `:515-526`, `:1271-1289`. **(observed)**
4. Otherwise the single candidate is checked by `blockedReason`, and if blocked it is skipped with that
   reason; else it is queued for install.

Note the sanitized-name grouping: two packages publishing `"Foo Bar"` and `"foo_bar"` are treated as
the same name and therefore as a same-level conflict. **(observed)** (Also see doc 07 §4.2.)

`blockedReason` (`:488-512`):

1. If a lock entry exists for that name and `installedBySync(entry)` is **false** →
   `"installed with \`skills add\` from <source>"`. This is the add-installed-wins rule; it applies
   even when the entry is a git remote skill *and* the same name is shipped by a package.
   Tests `:466-485`, `:1148-1165`. **(observed)**
2. Otherwise, every destination that would be written (canonical + each target agent's install path)
   is checked with `foreignDestination`; the first non-null reason blocks the skill.

Two asymmetry notes **(observed)**:
- Rule 1 looks the lock entry up by **sanitized** name inside `resolveConflicts`
  (`lockBySanitizedName`, `:533-535`) but the cached-remote path looks it up by the **exact** skill name
  (`:828`). Mixed-case names can therefore behave differently between the two paths.
- `resolveConflicts` only checks destinations for the **selected** target agents, whereas pruning
  checks every known agent (§8).

### 6.3 What gets skipped, and what still happens

Skipped packaged skills are only reported: `Skipped <name> from <pkg>: <reason>` on stdout (warning
level). Nothing is installed for them, no lock entry is written, and — importantly — a packaged skill
that is skipped still **claims its name** against remote sources (§9.3).

Skipped cached remote links are reported as `Skipped <name>: <reason>`; the skill stays installed for
the agents it already had. Test `:1068-1083`. **(observed)**

---

## 7. Install execution

### 7.1 Mode selection

- `mode = options.copy ? "copy" : "link"` (`src/sync.ts:853`).
- Per agent: `agent === "eve" ? "copy" : mode` (`:929`) — Eve rewrites `SKILL.md` frontmatter on
  install, which a link cannot do.
- `--copy` applies to **packaged skills only**. Remote `skills`-field skills are always installed with
  `installFromSource`, which uses symlink mode and is never told about `--copy`
  (`:984-1011`, `src/add.ts:2158-2169`). **(observed)**

### 7.2 What each mode does on disk

`link` mode (`src/installer.ts:411-421`):
1. `.agents/skills/<sanitized-name>` is created as a **symlink to the skill folder inside the
   installed package**. If symlink creation fails, it falls back to a recursive copy.
2. Each non-universal agent directory then gets a symlink to `.agents/skills/<name>`.
3. Universal agents (`skillsDir === ".agents/skills"`) have no separate directory: their "install path"
   *is* the canonical path, so nothing extra is linked.

`copy` mode (`:374-384`):
1. No canonical symlink is made; each target agent's directory receives a real copy.
2. Because a universal agent's directory is `.agents/skills/<name>` itself, there the "copy" *is* the
   canonical copy. Non-universal agents get independent copies.
3. A subsequent package update is **not** seen until the next sync — this is the documented cost of
   `--copy` (`commands.md:313`).

Tests: symlink identity `tests/sync.test.ts:378-391`, `:393-402`, package-update visibility `:404-415`,
copy mode `:417-426`, e2e `e2e/npm.e2e.ts:93-99`. **(observed)**

### 7.3 Agent-directory creation rules

`installSkillForAgent` skips creating an agent-specific project directory unless one of these holds
(`src/installer.ts:83-100`):

- the install is global, or
- the agent is universal, or
- `createMissingAgentRoot` was passed, or
- the agent config sets `createProjectSkillsDirByDefault`.

Only `claude-code` sets `createProjectSkillsDirByDefault: true` (`src/agents.ts:161-172`).
**(observed)**

Therefore, for a packaged skill:
- `-a claude-code` creates `.claude/skills/<name>` even when `.claude/` does not exist.
- `-a codex` with no existing `.codex/` produces a **successful, skipped** result
  (`skipReason: missing-agent-project-directory`); the skill is still in `.agents/skills` and counted
  as installed.
- Cached remote links **do** pass `createMissingAgentRoot: true`, so repairing links creates the
  agent root. (`src/sync.ts:904-910`, `:506-509`) **(observed)**

### 7.4 Result rows and lock writing

Each (skill × agent) attempt produces `{ skill, packageName, agent(display name), success, path,
canonicalPath?, error? }` (`:894-941`). After the loop:

- `successfulSkillNames` = names with at least one successful row.
- For each packaged skill in `toInstall` whose name is in that set **and** the run is not frozen, a
  lock entry is written. Errors are **swallowed** ("Don't fail sync if lock file update fails",
  `:950-970`). **(observed)**

### 7.5 Eve

Eve always receives copies (§7.1). For Eve the canonical base is the Eve agent dir, not
`.agents/skills` (`src/installer.ts:329-334`, doc 07 §3.3). Sync passes no `eveSubagent`, so Eve root
installs land in `agent/skills`.

---

## 8. Cleanup / pruning

`pruneStaleSkills` (`src/sync.ts:432-486`), run when `cleanup !== false`.

### 8.1 Candidate set

For every lock entry (`:446-455`):
- If `installedBySync(entry)` **and** not (`sourceType !== "node_modules"` **and**
  `keepRemote(entry, name)`): the entry's **sanitized** key becomes a candidate, remembering the raw
  lock key.
- Otherwise the sanitized key is added to `explicit` (a shield).

Additionally, every child of `.agents/skills/` that is a symlink resolving into `node_modules` becomes
a candidate, **even with no lock entry** (`linksIntoNodeModules`, `:423-430`, `:457-460`). So an
orphaned sync-style link is reclaimable.

Stale = candidates − (sanitized names of the **filtered** discovered packaged skills) − explicit.
Sorted for stable output.

`keepRemote` (`:695-699`): with `--no-remote`, every remote entry is kept. Otherwise an entry is kept
only if some surviving remote request both `requestsLockedSkill`-matches it and
`remoteAllowsSkill`-matches the current filters.

### 8.2 What is deleted

For each stale name (`:464-484`):

- Destinations = canonical `.agents/skills/<name>` plus `getInstallPath(name, agent)` for **every**
  agent in the registry (not just the selected targets).
- Each destination is removed only if `foreignDestination` says it is free or sync-owned — so a
  hand-written dir or a foreign symlink survives even if the lock entry is dropped.
- Then the lock key is deleted in memory and `removeSkillFromLocalLock(key)` re-reads and rewrites the
  lock under the inter-process mutex (`:478-483`, `src/local-lock.ts:456-470`). This avoids clobbering
  entries another process added after sync's initial read — verified by
  `tests/sync-lock-race.test.ts:19-74`. **(observed)**

### 8.3 Modes and their interactions

| Flags | Effect |
|---|---|
| (default) | stale skills and their lock entries removed; per-name log `Removed <name> (no longer provided by a dependency)` |
| `--dry-run` | same computation, no deletion; log `Would remove <name> (…)` |
| `--no-cleanup` | pruning skipped entirely (no deletion, no log) |
| `--frozen-lockfile` | pruning computed but **not applied**; if anything is stale the run fails (§11) |
| `--no-remote` | remote entries are never candidates; remote requests are not installed |

Tests: `tests/sync.test.ts:545-617`. **(observed)**

Because `--no-cleanup` skips the block entirely, `--frozen-lockfile --no-cleanup` also skips the
"no longer provided" check. **(observed from control flow)**

### 8.4 Preservation guarantees

- Anything with a non-sync lock entry is `explicit` and never pruned, even if its name collides with a
  discovered skill (`:452-455`).
- Anything sync did not create at a destination is left alone (`foreignDestination`).
- Skills added by `skills add` therefore survive both shadowing (§6.2 rule 1) and cleanup.
  Test `:580-597`. **(observed)**

---

## 9. Remote `skills`-field entries

### 9.1 Why they are a separate phase

Git-sourced entries need network work, so they are planned and installed separately from packaged
skills, after packaged installs and lock writes.

Planning (`:719-739`):
- `remoteRequests` = surviving requests **minus** those already fully installed.
  `isRemoteInstalled` requires, for every requested skill, a lock entry with `via` set, matching
  `source`/`ref`, **and** an existing canonical directory (`:620-635`). A request with an empty
  `skills` list counts as installed when at least one matching entry exists.
- `cachedRemoteSkills`: for lock entries that are not shadowed by a packaged name and belong to a
  request that is *not* in `remoteRequests` (i.e. fully installed) and still passes the filters, the
  canonical `SKILL.md` is re-parsed so the skill can be linked into agents that lack it — without any
  fetch. Test `:1030-1050`. **(observed)**
- If the canonical folder is gone, the request is *not* considered installed and is fetched again
  (test `:1101-1110`).

### 9.2 Cached link repair

For each cached skill, missing target agents are computed, then `blockedReason(name, missing,
lock.skills[name])` is applied. Allowed ones are installed with `mode: "symlink"` and
`createMissingAgentRoot: true` (`:822-834`, `:904-921`). The lock is not rewritten for these
(`:1030-1046` test asserts byte-equality of the lock). **(observed)**

### 9.3 Actual remote install and the claim set

`claimed` starts as **all discovered packaged skill names** — including ones that lost conflict
resolution or were blocked (`:973`). Then, per request, a `select` callback (`:992-1010`):

- drops names not allowed by `--include/--exclude` (re-checked per discovered skill);
- re-applies a rename the lock remembers (`renamedFor`), so a skill restored under an alias keeps it;
- skips a name already in `claimed` with reason `another source in this sync provides it`;
- otherwise applies `blockedReason` (add-installed / foreign destination) and logs `Skipped <name> from
  <label>: <reason>`;
- adds the name to `claimed` and returns it.

Consequence **(observed)**: because packaged names are claimed up front, a git skill loses to a packaged
skill even when the packaged skill was itself skipped (e.g. shadowed by `skills add`). The git skill is
then not installed either. Test `:1136-1146` covers the direct case.

Install is `installFromSource(fetched, { skills, agents: targetAgents, via, …(frozen && {frozenLock}) })`
(`:984-1011`), which:
- installs with symlink placement at project scope,
- records one lock entry per installed skill with `via` (`src/add.ts:2212-2220`, `:2239-2271`),
- in frozen mode instead validates hashes and never writes (`src/add.ts:2192-2205`, `:2213-2218`).

All sources are fetched **concurrently** (`fetchSources`, `src/add.ts:2075-2088`), then installed
**sequentially** in request order. A failing source prints
`Failed to install from <label>: <error>`, sets exit 1, and the rest continue (`:1012-1024`).
Test `:1167-1177`. **(observed)**

### 9.4 Removal

Remote entries are pruned by the same mechanism as packaged ones, keyed off whether the request
survives (`keepRemote`). Tests `:960-985` (excluded remote skill removed without refetching),
`:1112-1122` (field entry removed → skill removed), `:1124-1134` (`--no-remote` neither installs nor
removes). The log line still says "no longer provided by a dependency" even for a remote entry — a
message inaccuracy. **(observed)**

---

## 10. The project lock (`skills-lock.json`)

### 10.1 Shape

```
{
  "version": 2,
  "skills": {
    "<lock key>": {
      "source": string,
      "sourceUrl"?: string,
      "ref"?: string,
      "commit"?: string,
      "sourceType": string,
      "skillPath"?: string,
      "skillName"?: string,
      "version"?: string,
      "via"?: string,
      "computedHash": string,
      "subagents"?: string[],
      "wellKnownDigest"?: string,
      "installMode"?: "copy"
    }
  }
}
```

`version` is 2; v1 is read and migrated. Entries are timestamp-free on purpose to keep merges clean.
(`src/local-lock.ts:23-97`) **(observed)**

What sync writes for a packaged skill (`src/sync.ts:950-965`):

| Field | Value |
|---|---|
| key | `skill.name` **raw** (frontmatter name, not sanitized) |
| `source` | the npm package name (the declaring dependency's name key, not the package's own `name`) |
| `sourceType` | `"node_modules"` |
| `skillPath` | `"SKILL.md"`, `"skills/<dir>/SKILL.md"`, or `"dist/skills/<dir>/SKILL.md"` (relative to package root) |
| `version` | package `version`, omitted when absent |
| `via` | only when the skill came through a `skills` field `npm:` entry |
| `computedHash` | `computeSkillFolderHash(skill.path)` over the package folder |

What a remote install writes (`recordProjectLock`, `src/add.ts:1966-1982`, `:2239-2271`):
`source` (normalized owner/repo or URL), optional `sourceUrl` (non-GitHub hosts / generic git), `ref`,
`commit` (when the fetch resolved one), `sourceType`, `skillPath`, `skillName` (renamed), `computedHash`,
and `via` when sync requested it. `installMode: "copy"` is added by restores that keep copy mode.

### 10.2 Writing discipline

- Sorted by key, `JSON.stringify(…, null, 2)` + trailing `"\n"` (`src/local-lock.ts:338-359`).
- Writes are **delta** operations: `addSkillToLocalLock` and `removeSkillFromLocalLock` each acquire an
  exclusive `<lockPath>.lock` file, re-read, mutate one key, then atomic-rename a temp file
  (`:292-320`, `:370-420`, `:456-470`).
- Mutex: `wx` create; waiters poll 10–25 ms with jitter; a lock older than 5000 ms is broken by
  renaming it to a unique name; give up after 10 000 ms with `SKILLS_LOCAL_LOCK_BUSY`. Windows rename
  retries EPERM/EBUSY. **(observed)**
- Reads: absent file → empty lock; unparseable/missing `skills`/unknown version → `SKILLS_INVALID_LOCAL_LOCK`
  (never silently empty); `local` sources are absolutized; v1 is migrated by recomputing hashes when
  the installed copy still matches the v1 hash, else `computedHash: ""` (`:122-196`).
- Sync never bulk-writes the lock; it only adds per-successful-skill and removes per-stale-skill.

### 10.3 Hash

`computedHash` is SHA-256 over the skill folder's files (paths sorted by UTF-8 bytes; each file framed
`path NUL byteLength NUL content`; CRLF→LF for text files; `metadata.json` files and `.git`,
`__pycache__`, `__pypackages__` directories excluded; symlinks hash as their target's content, but
broken/cyclic/escaping links are skipped). (`src/skill-hash.ts:11-113`, README "Lock File Content
Hash") **(observed)**

For packaged skills the folder hashed is the **package's** skill folder (which for pnpm resolves through
realpath), so the recorded hash is the same value the installed link points at. `--frozen-lockfile`
compares against this. For a renamed remote skill the `SKILL.md` name is rewritten before hashing
(`src/add.ts:2431-2449`). **(observed)**

---

## 11. `--frozen-lockfile` in sync

`frozen = options.frozenLockfile ?? isCI()` (`src/sync.ts:691`). `--no-frozen-lockfile` sets it to
false explicitly.

### 11.1 What counts as a mismatch

Two independent checks:

**A. Stale lock entries** (cleanup phase, `:700-717`). Any name computed as stale by §8.1 becomes the
change `<name> is no longer provided by a dependency`. If any exist, the run prints
`frozenLockfileError(changes)` and **returns immediately** with exit 1: no installs, no lock writes, no
deletions.

**B. Packaged skills to install** (`:835-845`). For each skill that survived conflict resolution, a
hash of its package folder and its `version` are compared via `lockChangeReason`
(`src/local-lock.ts:426-443`):

| Condition | Message |
|---|---|
| no lock entry | `<name> is not in skills-lock.json` |
| entry has `computedHash: ""` | `<name> has no content hash in skills-lock.json (migrated from version 1, and the installed skill was missing or did not match)` |
| hash differs | `<name> has changed since skills-lock.json was written` |
| `version` differs (including undefined vs set) | `<name> comes from version <new>, skills-lock.json has <old>` |

Any change → same abort. Tests `tests/sync.test.ts:255-344`; version message asserted at `:289-298`.
**(observed)**

**C. Remote requests.** Each request is installed through `installFromSource` with `frozenLock`, which
re-hashes the fetched skills before writing and compares against the lock; a mismatch aborts **that
source** with `frozenLockfileError` as the error string, prints
`Failed to install from <label>: …`, sets exit 1, and continues with the other sources
(`src/add.ts:2192-2205`, `src/sync.ts:1012-1016`). **(observed)**

### 11.2 Error text and exit

```
Cannot install with --frozen-lockfile because skills-lock.json is not up to date:
  <change>
  <change>
Run again with --no-frozen-lockfile to update it.
```
(`src/local-lock.ts:445-451`) Exit code 1; the lock file is byte-identical afterwards
(tests assert this: `tests/sync.test.ts:271-309`). **(observed)**

### 11.3 Gaps / surprising consequences

- Cached remote links are **not** hash-verified in frozen mode; only the *newly installed* set is.
  A canonical copy that was edited after the first sync is therefore re-linked into new agent
  directories without complaint, and the run still passes. **(observed absence)**
- A packaged skill that is skipped (conflict / add-installed / foreign destination) is **not** checked,
  so frozen can pass while a packaged skill silently does not install. **(observed)**
- `--no-cleanup` disables check A entirely (§8.3). **(observed)**
- `--no-remote` removes check C for remote entries and also stops pruning them. **(observed)**

---

## 12. `--dry-run` output

In sync (`:852-880`):

1. Discovery, filtering, pruning computation, remote planning, agent selection, conflict resolution and
   the frozen validation of packaged skills all still run (and can therefore fail or prompt).
2. Per stale name: `Would remove <name> (no longer provided by a dependency)`.
3. A "Sync Summary" note with one or two lines per planned item:
   - packaged: `<name> ← <package>` then `  <canonical path> → <package skill folder>` (link mode) or
     `  <canonical path> copied from <folder>` (copy mode);
   - remote: `<lock source> ← <declaring package> (remote)`;
   - cached: `<name> ← <lock source> (cached), link to <agent, agent>`.
4. `Dry run — nothing changed.` and return.

Nothing is written: no skill dirs, no lock, no `.agents/skills` (tests `:449-459`, `:1052-1066`,
`e2e/npm.e2e.ts:81-87`). Telemetry is not sent because the function returns before step 18.
**(observed)**

---

## 13. `experimental_install` (restore from lock)

`runInstallFromLock` (`src/install.ts:54-287`).

### 13.1 Flags

It reuses `parseSyncOptions`, so it accepts **every** sync flag, including the undocumented
`--include`, `--exclude`, `--no-cleanup`, `--no-remote`, `--recursive`, `--copy`, and `-y`.
`--force` is additionally detected with a raw `args.includes("--force")` check — it is not in the
parser's spec, so `--force=true` would not count. (`:56-58`, `src/cli-args.ts`) **(observed)**

The documented flag set is only `-a/--agent`, `--dry-run`, `--frozen-lockfile`,
`--no-frozen-lockfile`, `--force` (`docs/commands.md:412-418`). The others leak through because of the
shared parser; `--copy`/`--include`/`--exclude`/`--no-cleanup`/`--no-remote`/`--recursive` reach the
node_modules sync fallback and `--copy` also reaches packaged skills installed by it. **(observed)**

### 13.2 Behavior

1. `frozen = syncOptions.frozenLockfile ?? isCI()`; `force = "--force" in args`.
2. `readLocalLock(cwd, { persistMigration: !frozen && !dryRun })` — a v1 lock is migrated in memory and
   written back only when the run is allowed to write.
3. **Empty / missing lock**:
   - frozen → error `Cannot install with --frozen-lockfile because skills-lock.json is absent or has no
     skills`, exit 1;
   - otherwise → warning `No project skills found in skills-lock.json` plus an informational hint to use
     `npx skills add` without `-g`, exit 0, nothing installed.
   (`:61-76`, tests `tests/install-from-lock.test.ts:362-367`)
4. **Agent resolution** (`resolveRestoreAgents`, `:22-40`): `--agent '*'` → all registered agents;
   otherwise the given ids are validated (unknown → `SKILLS_INVALID_AGENTS`, caught, printed, exit 1,
   nothing installed), else the last selection saved by `skills add`, else
   `detectInstalledAgents(cwd)`. Universal agents are always unioned in. With none of these, only
   `.agents/skills` is filled.
   Tests `:207-298`, especially `:229-268`. **(observed)**
5. **Partition the lock**:
   - `sourceType === "node_modules"` → collected for sync, never handled here;
   - other entries: unless `--force`, `getLockedSkillState(name, entry)` decides
     `tracked` → "already installed" (only agent links are repaired),
     `modified` → restore over it,
     `missing` → restore.
   (`:111-124`, `src/local-lock.ts:235-247`) **(observed)**
6. **Rebuild the source** per entry (`:126-139`):
   - `sourceType === "download"` → `sourceUrl` as-is (no ref/subpath appended);
   - otherwise `buildLocalUpdateSource({...entry, ref: entry.commit ?? entry.ref})`: the lock's
     `sourceUrl` or normalized `source`, then — only when the source can carry an appended subpath —
     the skill's folder derived by stripping `SKILL.md`, then `#<commit-or-ref>` appended in all cases
     (`src/update-source.ts:17-22`, `:49-79`, `:102-109`, `:140-149`).
   - `null` (a legacy generic-git/GitLab entry normalized to a bare `owner/repo` with no `sourceUrl`) →
     `SKILLS_LOCK_MISSING_SOURCE_URL`, entry recorded as failed. Test `tests/install.test.ts:58-74`.
   - A **git commit** in the lock is therefore fetched by SHA; the recorded `ref` is restored afterwards
     so the lock keeps tracking the branch/tag it came from (`:214-218`, and
     `tests/lock-commit-pin.test.ts:60-79` shows an upstream move does not change what is restored).
     **(observed)**
7. **Group** by `(copy|symlink, download|source, installSource, pinned ref)`; each group remembers the
   installed names and, per lowercased **source** name, every install name (renames).
   `installMode === "copy"` is per lock entry, so copied skills stay copies. (`:140-155`)
8. **Report the plan** and, when not `--dry-run`, repair links for already-installed skills:
   for each target agent missing the path, link (or copy, if the lock says copy) from the canonical
   dir, creating missing agent roots. (`:157-190`, `:289-318`)
9. **Fetch everything at once**, then install each group sequentially (`:192-255`). Downloads are
   fetched with `fullDepth: true` because a pack/archive may hold skills at any depth.
   Each found skill is installed under every name the lock has for it (rename support).
10. **Not found**: a locked skill the source no longer provides is reported
    `<names> not found in <source>` and counted as failed. Test `install-from-lock.test.ts:140-153`.
11. **node_modules entries**: if any exist, print `<n> skills from node_modules`, then call
    `runSync(args, { ...syncOptions, frozenLockfile: frozen, yes: true, agent: targetAgents })`
    (`:257-275`). This is why restoring a project with packaged skills works after `npm install`, and
    why restore never installs npm packages itself (`commands.md:398-405`).
    A thrown error is reported as `Failed to sync node_modules skills: …` and those names are marked
    failed.
12. **Summary**: `Restored N skill(s); failed to restore M skill(s) from skills-lock.json: <names>` or
    `Failed to restore M skill(s) …`, exit 1. Sources that failed keep their lock entries unchanged
    (tests `:125-138`).
13. **No prompts at all**: the command never calls a prompt/confirm. `yes` is not consulted for a
    confirmation; it only influences sync's internals. **(observed absence of prompt calls)**

### 13.3 Failure isolation

- A source that cannot be fetched fails only its own entries; other sources still restore.
- A single skill not found fails only that entry.
- Per-agent failures from `installFromSource` set exit 1 and continue.
- Duplicate lock keys for the same source name (aliases) are installed from a single fetch.
- `--frozen-lockfile` never writes: `installFromSource` receives `frozenLock` and validates instead,
  and link repair still happens for already-tracked skills (their hash was already verified).
  Test `:347-360`. **(observed)**

### 13.4 Interaction with sync that is easy to miss

- A restored **remote** skill (one with `via`, created by sync) is re-recorded by `recordProjectLock`
  without `via`. Full-entry replacement in `addSkillToLocalLock` means the `via` field is **lost**
  after a non-frozen restore. Consequently a later sync treats that entry as user-installed (`explicit`)
  and will never prune it. **(observed in code; not covered by a test — see Open questions)**
- `experimental_install` restores remote entries in lock order (alphabetical), one source group at a
  time, not in dependency order.
- `installMode: "copy"` survives a restore (passed to `recordProjectLock`).

---

## 14. Plugin manifests and the package boundary

`src/plugin-manifest.ts` implements Claude Code compatibility for **source discovery**, not for
`node_modules`. Its rules (containment under the base path, `./` prefix requirement, remote-source
skipping, parent-directory expansion, grouping map) are documented in doc 07 §8; this section records
only the boundary facts relevant to the packages work.

- `getPluginSkillPaths(basePath)` and `getPluginGroupings(basePath)` are called exclusively from
  `discoverSkills` (`src/skills.ts:8`, `:210`, `:282`). **(observed)**
- `src/sync.ts` does **not** import or call them, and does not consult `.claude-plugin/*`,
  `skills.sh.json`, or `.claude-plugin` directories inside a package. A package shipping skills *only*
  through a plugin manifest is invisible to `experimental_sync`. **(observed: no imports in
  `src/sync.ts:1-44`; discovery hard-codes `skills` and `dist/skills`)**
- They do apply when a package directory is used as an `add`/`resolveSource` source (local path,
  cloned repo, or blob snapshot), where a manifest can contribute search directories and a display
  `pluginName`. `pluginName` is display/telemetry metadata and, for global installs, is persisted in
  the global `.skill-lock.json` entry (`src/skill-lock.ts:41-42`; `src/add.ts:2823-2843`,
  `src/list.ts:242-243`). It never changes install paths or folder names. **(observed)**
- Manifest contributions are walked at depth 1 and appended after the standard containers, so a
  standard-location copy wins a name collision; a declared path `./a/b/c/skill` contributes its parent
  `a/b/c`, which is why "declared depth" works. **(observed, doc 07 §8.3)**

For a Go port that only implements the two experimental commands, plugin manifests can be skipped
entirely without changing sync/restore behavior. **(assumed: no other package code path reads them)**

---

## 15. Public programmatic API (`src/api/*`)

Not exposed by the Go port, but documented because it names the underlying operations and their
contract. Entry point is `src/index.ts`, which lists every export by name; ESM only; Node ≥ 22.20
(`package.json:5-14`, `:159-161`).

### 15.1 Functions

| Export | Signature (informal) | Underlying operation |
|---|---|---|
| `getSupportedAgents()` | `() => readonly AgentInfo[]` | Static registry incl. `universal`. Never detects. |
| `detectAgents(options?)` | `BaseOptions => { agents }` | `detectInstalledAgents(cwd)`, filtered by `config.agents`. |
| `resolveSource(source, options?)` | `(string, ResolveOptions) => SkillSource` | Parse + fetch + discover, no install. Caller must `dispose()`. |
| `installSkills(source, options?)` | `(string \| SkillSource, InstallOptions) => InstallResult` | Fetch (if string), select, install to agents, optionally record lock, optionally telemetry. |
| `listSkills(options?)` | `ListOptions => ListResult` | Scan disk for project/global/both; join lock source/`renamedFrom`. |
| `removeSkills(names, options?)` | `(string[], RemoveOptions) => RemoveResult` | Remove from agents, keep shared copies still in use, update lock. |
| `version` | `string` | `package.json` version. |
| `diagnostics`, `isDiagnostic` | diagnostic catalog | Error codes/constructors. |

`docs/api.md:33-43`. **(observed)**

### 15.2 Options and defaults

`resolveConfig` merges per-key: call options → `options.config` → defaults (`src/api/config.ts:27-43`):

```
scope: 'project' | 'global'          default 'project'
mode: 'auto' | 'symlink' | 'copy'    default 'auto'
lockfile: boolean                    default true
fullDepth: boolean                   default false
includeInternal: boolean             default false
duplicates: 'first' | 'error'        default 'first'
symlinks: 'follow' | 'reject'        default 'follow'
telemetry: boolean                   default true
agents: { include?, exclude?, default? }
cwd: process.cwd()
```

Invalid enum values throw `SKILLS_INVALID_OPTION_VALUE`; unknown agent ids throw `SKILLS_INVALID_AGENTS`
(`:45-72`, `:79-87`).

Behavioral notes **(observed)**:
- `mode: 'auto'` = `defaultInstallMode`: **copy** when the chosen targets all map to one skills
  directory (or all are Eve), **symlink** otherwise (`src/add.ts:479-490`).
- Without `agents`, targets come from `nonInteractiveTargetAgents`: host agent if running inside one,
  else `['eve']` when Eve is installed, else every detected agent, else *all* agents; universal agents
  are unioned in (`src/add.ts:467-477`). This differs from the CLI sync path, which never falls back
  to "all agents".
- `createMissingAgentRoot` is true only for agents the **caller named** (explicitly or via
  `config.agents.default`), not for auto-detected ones (`src/api/install.ts:84-96`).
- `config.agents` can only narrow: a named-but-disallowed agent or an agent without a global dir
  becomes a per-skill `InstallFailure`; auto-selected ones are dropped silently; zero targets left
  throws `SKILLS_NO_TARGET_AGENTS` (`:237-272`).
- With `lockfile: true` and project scope, `installSkills` validates the lock **before** writing
  anything (`SKILLS_INVALID_LOCAL_LOCK`) and wraps a lock-write failure as `SKILLS_LOCK_WRITE_FAILED`
  (`:71`, `:329-362`) — unlike CLI sync, which swallows lock-write errors.
- `rename` keys must match exactly one selected skill; new names must be safe folder names; two skills
  may not collide. Violations throw before anything is written (`:144-203`).
- `duplicates: 'error'` / `symlinks: 'reject'` throw before writing (`:364-387`).
- `onProgress` emits `resolve:start`, `resolve:done`, `install:start`, `install:done`; `onWarning`
  receives non-fatal diagnostics (skipped invalid `SKILL.md`, unsafe symlinks); without a handler they
  are dropped (`src/warnings.ts:19-25`).
- `signal` is checked between steps; an aborted call rejects with the abort reason; a running clone is
  not interrupted.

### 15.3 Return types

```
InstallResult = { source: SourceInfo,
                  installed: InstalledSkillTarget[],   // frozen
                  skipped: SkippedSkillTarget[],       // reason: 'missing-agent-directory'
                  failed: InstallFailure[] }           // { skill, agent, code, message }
InstalledSkillTarget = { skill, agent, scope, mode: 'symlink'|'copy', path, canonicalPath? }
SkillSource = { source, skills: SkillInfo[], dispose() }
SkillInfo = { name, description, path?, duplicates?, pluginName?, metadata? }
ListResult = { skills: InstalledSkillInfo[] }        // name, description, scope, path, agents, source?, renamedFrom?
RemoveResult = { removed[], notFound[], failed[] }   // failed: { skill, code, message }
```

All result objects are frozen (`src/api/install.ts:287-327`, `src/api/types.ts:166-290`).
`listSkills` reports `path` as the canonical copy when there is one and swallows an unreadable lock
(`src/api/list.ts:38-74`); `removeSkills` instead throws on an unreadable lock
(`src/api/remove.ts:118-121`). **(observed)**

### 15.4 Error model

- Errors are `nostics` diagnostics from the same catalog the CLI prints; each has a stable `SKILLS_*`
  code, a message, and often a `fix`. Check with `diagnostics.SKILLS_X.is(e)` or `diagnostics.is(e)`.
- **Throwing** = nothing could be attempted: invalid option, unknown agent, unknown/ambiguous skill
  name, unreadable lock, source fetch failure, no skills found, disposed source, no target agents.
- **Returning `failed`** = individual skill/agent failures after the operation was attempted.
- Fetch failures are the original git/download/archive diagnostic, not a wrapper; anything else is
  wrapped in `SKILLS_SOURCE_FETCH_FAILED` (`src/api/resolve.ts:126-134`).
- `SKILLS_SOURCE_DISPOSED` when a `SkillSource` is reused after `dispose()`.
- No terminal side effects: no prompts, no spinners, no `process.exit`, no stdout writes.

`docs/api.md:106-132`, `:180-247`. **(observed)**
Two CLI-visible consequences for a port: the *same* catalog is used for human messages, and exit codes
are driven by "did anything fail", not by the code itself.

---

## 16. Surprising / undocumented behavior

1. **`experimental_install` accepts all sync flags.** `--include`, `--exclude`, `--no-cleanup`,
   `--no-remote`, `--recursive`, `--copy` are parsed and forwarded to the node_modules sync fallback;
   `--copy` additionally affects packaged skills installed by that fallback. Nothing in the help text
   or docs mentions this. (`src/install.ts:56`, `src/cli.ts:161-166`) **(observed)**
2. **`--copy` does not apply to remote `skills`-field skills.** They are always installed through
   `installFromSource` (symlink placement). Only packaged skills honour `--copy`.
   (`src/sync.ts:853`, `:929`, `:984-1011`) **(observed)**
3. **Excluding a skill deletes it.** `--include/--exclude` feed the cleanup keep-set, so a filter that
   hides a previously synced skill removes it and its lock entry (unless `--no-cleanup`).
   Test `tests/sync.test.ts:679-687`. **(observed)**
4. **A skipped packaged skill still blocks the same-named git skill.** `claimed` is seeded with all
   discovered packaged names, not only installed ones. (§9.3) **(observed)**
5. **`--frozen-lockfile --no-cleanup` skips the stale-entry check.** The whole cleanup block, including
   the frozen comparison, is inside `if (options.cleanup !== false)`. **(observed)**
6. **Sync swallows lock-write failures for packaged skills.** The skill is installed but untracked, and
   the run still exits 0. The API instead throws `SKILLS_LOCK_WRITE_FAILED`. (`:950-970`) **(observed)**
7. **Deleting `package.json` and syncing prunes everything sync installed.** Discovery returns empty
   lists, so all node_modules lock entries become stale. **(observed from control flow)**
8. **A package with a root `SKILL.md` hides its `skills/` directory** — only the root skill is synced.
   **(observed)**
9. **`experimental_install` drops `via` when it rewrites a restored entry**, changing how later syncs
   treat that skill (it becomes user-owned). (§13.4) **(observed in code)**
10. **`skills install <source>` is `add`, but `skills install` with no source is restore.** Same for
    `i`. (`src/cli.ts:373-383`) **(observed)**
11. **Invalid `--agent` hard-exits** in sync (`process.exit(1)`) instead of setting `exitCode`, so no
    finally-style cleanup/telemetry runs. (`:766-776`) **(observed)**
12. **`--dry-run` can still prompt** for agent selection when not `-y` and several agents are detected.
    **(observed)**
13. **`peerDependencies` are scanned** like normal dependencies — surprising for a "devDependencies"
    mental model. **(observed)**
14. **A dependency's non-array `skills` key is silently ignored**, while the project's is a fatal error.
    Dependencies are expected to use the key for other purposes. (`:337-348`) **(observed)**
15. **Two different frontmatter names that sanitize to the same folder are treated as a same-level
    conflict**, so neither installs. **(observed)**
16. **Cached remote links are created with `createMissingAgentRoot: true`**, whereas packaged skills
    rely on `claude-code`'s `createProjectSkillsDirByDefault`. The same `-a codex` may therefore
    produce a link for a cached remote skill and no link for a packaged one. **(observed)**
17. **Stale remote entries are logged as "no longer provided by a dependency"**, even though they came
    from a git source. **(observed)**
18. **`runSync`'s `args` parameter is dead code.** **(observed)**
19. **`skills experimental_sync` never installs or resolves npm packages**; it only reads what is
    already in `node_modules`. `commands.md:403-405` says so for restore; it is equally true for sync.
20. **`--include` on a remote request matches the declaring package, not the git source.** A project-level
    field entry is matched with package `.`. **(observed)**

---

## Open questions

1. **Is the frozen-lockfile gap for skipped packaged skills intentional?** A conflict-blocked or
   `skills add`-shadowed skill is not hash-checked, so frozen CI can pass while a dependency's skill is
   never installed. No test covers it.
2. **Is losing `via` on `experimental_install` intended?** It silently converts a sync-owned entry into
   a user-owned one. A port that wants stable cleanup semantics must decide whether to preserve `via`
   (and the entry's `sourceType`) when restoring.
3. **Should `--include/--exclude` delete previously synced skills?** Current behavior is "the filter is
   the desired state". It is tested, but it means a narrowed CI run mutates the checkout.
4. **Should a skipped packaged skill still claim its name against git sources?** The `claimed` seeding
   makes a blocked package win by default, which can leave a name with no installed skill.
5. **Is `experimental_install` supposed to accept the sync-only flags?** They are forwarded by accident
   of the shared parser; a Go port needs an explicit decision (accept and emulate, or reject).
6. **`findInstalledPackage` does not realpath the starting directory**, contrary to its comment. Does any
   real layout depend on the non-realpath walk (e.g. workspace dirs that are themselves symlinks)?
7. **Which `package.json#version` is authoritative for the lock's `version` field?** Currently the
   installed package's own `version`. A dependency alias or a workspace link can make that differ from
   the specifier; frozen then reports a version change. Not tested.
8. **Do workspace patterns with `**` (e.g. `packages/**`) behave as expected?** They are globbed as
   `<pattern>/package.json`; only `*`-shaped patterns are tested.
9. **Are `peerDependencies` really meant to be scanned?** They are frequently not installed, and a
   missing peer is skipped silently.
10. **Is the multi-declarer remote-request merge correct for `via`?** `via` becomes the first surviving
    declarer; `declarers` is kept only in memory, so the lock records one arbitrary owner.
11. **What should happen when the project's `skills` field points at a package that is only a
    transitive dependency?** It works today (node_modules walk finds it), which contradicts the
    "only declared packages are scanned" comment in `src/sync.ts:241-249`.
12. **Should sync honour Claude plugin manifests inside a package?** It does not today; the docs do not
    say whether that is permanent.

---

## Evidence index

**CLI dispatch / help** — `src/cli.ts:156-166` (project commands + sync options), `:194-206`
(experimental sync options), `:368-418` (routing, `install` with/without source, `sync` alias),
`:448-459` (top-level diagnostic handling); `docs/commands.md:290-426`; `README.md:66-84`;
`tests/sync.test.ts:1305-1318` (help/banner assertions).

**Sync flags** — `src/sync.ts:48-60` (`SyncOptions`), `:1078-1107` (`parseSyncOptions`);
`src/cli-args.ts:5-14`, `:57-104` (list/boolean semantics); `docs/commands.md:364-376`.

**Sync pipeline** — `src/sync.ts:637-1076`; agent auto-detect `:640-654`; discovery/filters `:671-688`;
lock + frozen `:690-717`; remote planning `:719-739`; early exits `:741-748`, `:846-850`;
agent selection `:758-809`; conflicts `:811-834`; frozen validation `:835-845`; summary/dry-run
`:852-889`; install `:891-943`; lock write `:945-970`; remote install `:972-1024`; report/telemetry
`:1026-1075`.

**Package discovery** — `src/sync.ts:105-134` (types + `readPackageJson`), `:136-171`
(`discoverPackageSkills`), `:173-183` (`findInstalledPackage`), `:234-239` (`DEPENDENCY_FIELDS`),
`:250-275` (workspaces), `:277-386` (`discoverNodeModuleSkills`), `:313-330` (declarer construction),
`:331-383` (`skills`-field walk, npm chains), `:388-395` (helpers).
Tests `tests/sync.test.ts:85-193` (discovery), `:704-859` (field), `:1189-1303` (`--recursive`).

**`skills` field** — `src/skills-field.ts:6-34`, `:36-47`, `:50-80`; `src/add.ts:1961-1982`;
`tests/skills-field.test.ts:1-91`; `docs/commands.md:338-350`.

**Filters** — `src/sync.ts:62-89`, `:187-213`; `tests/sync.test.ts:620-702`, `:940-1002`;
`docs/commands.md:378-380`.

**Ownership / conflicts** — `src/sync.ts:392-421`, `:423-430`, `:488-512`, `:519-568`, `:611-618`;
`tests/sync.test.ts:462-527`, `:793-803`, `:1136-1165`; `docs/commands.md:352-362`.

**Install modes / agents** — `src/sync.ts:852-943`; `src/installer.ts:39-100` (modes, skip rule),
`:303-488` (`installSkillForAgent`), `:676-719` (install/canonical paths); `src/agents.ts:161-172`
(`createProjectSkillsDirByDefault`), `:859-867` (`detectInstalledAgents`), `:908-946`
(universal sets); `tests/sync.test.ts:362-460`; `e2e/npm.e2e.ts:18-107`.

**Cleanup** — `src/sync.ts:432-486`; `src/local-lock.ts:456-470`; `tests/sync.test.ts:529-618`;
`tests/sync-lock-race.test.ts:19-74`.

**Remote entries** — `src/sync.ts:719-739`, `:822-834`, `:904-921`, `:972-1024`; `src/add.ts:2039-2088`
(`fetchSource(s)`), `:2158-2227` (`projectPlacement`, `installFromSource`), `:2239-2271`
(`recordProjectLock`); `tests/sync.test.ts:861-1187`.

**Lock** — `src/local-lock.ts:23-97` (shape), `:99-109` (paths), `:122-196` (read/migrate), `:203-247`
(hashes/state), `:261-320` (mutex), `:338-388` (write), `:405-443` (add + `lockChangeReason`),
`:445-451` (`frozenLockfileError`); `src/skill-hash.ts:11-113`; `README.md:86-130`;
`tests/sync.test.ts:195-360`; `tests/install-from-lock.test.ts:301-384`.

**Frozen / CI** — `src/ci.ts:1-12`; `src/sync.ts:603-609`, `:691`, `:700-717`, `:835-845`;
`src/add.ts:2192-2218`; `tests/sync.test.ts:238-359`; `tests/install-from-lock.test.ts:301-384`.

**Dry run** — `src/sync.ts:852-880`; `tests/sync.test.ts:449-459`, `:608-617`, `:1052-1066`;
`e2e/npm.e2e.ts:81-87`.

**`experimental_install`** — `src/install.ts:16-40` (`resolveRestoreAgents`), `:42-53` (contract
comment), `:54-287` (pipeline), `:289-318` (`linkInstalledSkills`); `src/update-source.ts:67-79`,
`:140-149`; `tests/install-from-lock.test.ts:1-385`; `tests/install.test.ts:25-75`;
`tests/lock-commit-pin.test.ts:60-79`; `tests/invalid-local-lock.test.ts:83-93`;
`e2e/rename.e2e.ts:97-121`; `docs/commands.md:389-426`.

**Plugin manifests** — `src/plugin-manifest.ts:8-20`, `:51-112`, `:120-183`; usage only at
`src/skills.ts:8`, `:210-219`, `:282`; `tests/plugin-manifest-discovery.test.ts:1-586`;
`tests/plugin-grouping.test.ts:1-72`; `docs/creating-skills.md:120-138`; doc 07 §8.

**Public API** — `src/index.ts:1-47`; `src/api/types.ts:1-290`; `src/api/config.ts:27-93`;
`src/api/agents.ts:8-37`; `src/api/resolve.ts:49-134`, `:212-225`; `src/api/install.ts:48-128`,
`:130-203`, `:237-362`, `:364-415`; `src/api/list.ts:9-75`; `src/api/remove.ts:27-121`;
`src/warnings.ts:9-25`; `docs/api.md:33-171`, `:180-268`; `package.json:1-18`, `:159-161`.
