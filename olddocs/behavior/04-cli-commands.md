# Behavior: CLI surface and per-command behavior (`skills` v1.9.0)

Scope: top-level dispatch, argument parsing, and the `list`, `remove`, `update`, `check`,
`use`, `find`, `init` commands, plus diagnostics/warnings/output conventions.

Out of scope (covered by other documents): the `add`/install engine internals, sync /
`experimental_*` internals, lock hashing internals, and source fetching/protocol details.
Where those are needed for context, only the observable CLI contract is described.

Source: `/tmp/opencode/vercel-skills` (clone of vercel-labs/skills v1.9.0, MIT).

**Claim markers.** **observed** = read directly in source or in a checked-in test that
exercises it. **assumed** = inferred from library behavior or from partial code paths and
not verified by running the binary (the clone has no `node_modules`, so nothing was
executed).

---

## 1. Top-level dispatch

Entry point: `src/cli.ts` (bin `skills`, plus legacy bin name `add-skill`).

### 1.1 Command table

| Token | Handler | Aliases | Logo printed? | Notes |
| --- | --- | --- | --- | --- |
| `add` | `parseAddOptions` + `runAdd` | `a` | yes, unless `--json` or in agent | with no positional source it still runs add (interactive listing/pick) |
| `install` | add, **or** restore when no source | `i` | yes (restore: yes) | `install`/`i` **without** a positional source = restore from `skills-lock.json` (`runInstallFromLock`); with a source = `add` |
| `experimental_install` | `runInstallFromLock(restArgs)` | none | yes | always restore |
| `use` | `parseUseOptions` + `runUse` | none | **no** | prompt on stdout, or launch an agent |
| `remove` | `parseRemoveOptions` + `removeCommand` | `rm`, `r` | no | own help screen |
| `list` | `runList` | `ls` | no | |
| `find` | `runFind` | `search`, `f`, `s` | yes (then a blank line) | |
| `init` | `runInit` | none | yes (then a blank line) | synchronous |
| `sync` | `runSync` | `experimental_sync` | yes | out of scope |
| `experimental_sync` | `runSync` | `sync` | yes | out of scope |
| `update` | `runUpdate(restArgs)` | `upgrade` | no | |
| `check` | `runUpdate(restArgs, 'check')` | none | no | **not listed in `--help` or the banner** |
| `--help` / `-h` | `showHelp()` | — | no | |
| `--version` / `-v` | prints version | — | no | |
| anything else | unknown-command diagnostic | — | no | exit code 1 |

Evidence: `src/cli.ts:354-442`. **observed**

`check` exists as a dispatch token and in `README.md`/`docs/commands.md`, but neither the
banner nor `showHelp()` mentions it. **observed** (`src/cli.ts:92-221`, `src/cli.test.ts:141-145`)

### 1.2 `skills` with no arguments

- If **not** running inside a detected AI agent: prints the ASCII-art `skills` logo (6 lines,
  256-color gray gradient), a blank line, the tagline `The open agent skills ecosystem`, then
  a command cheat-sheet block, a `try:` line, and a skills.sh link line. Exit code 0.
- If running inside an agent (see §11.2): prints **nothing at all**. Exit code 0.

Evidence: `src/cli.ts:35-90`, `324-330`, `src/cli.test.ts:49-60`. **observed**

Banner body (exact, minus ANSI colors):

```
  $ npx skills add <package>        Add a new skill
  $ npx skills use <package>@<skill> Use a skill without installing
  $ npx skills remove               Remove installed skills
  $ npx skills list                 List installed skills
  $ npx skills find [query]         Search for skills

  $ npx skills update               Update installed skills

  $ npx skills experimental_install Restore from skills-lock.json
  $ npx skills init [name]          Create a new skill
  $ npx skills experimental_sync    Sync skills from node_modules

try: npx skills add vercel-labs/agent-skills

Discover more skills at https://skills.sh/
```

### 1.3 `--help` / `-h`

`showHelp()` prints one large block to **stdout**, exit code 0. Section order:
`Usage:`, `Manage Skills:`, `Find Options:`, `Updates:`, `Update Options:`, `Project:`,
`Experimental Install Options:`, `Add Options:`, `Use Options:`, `Remove Options:`,
`Experimental Sync Options:`, `List Options:`, `Options:`, `Examples:`, then the skills.sh
footer. **observed** (`src/cli.ts:92-221`); the test asserts presence of `Usage:`,
`Manage Skills:`, `init [name]`, `add <package>`, `use <package>@<skill>`, `update`,
`Add Options:`, `Use Options:`, `-g, --global`, `-a, --agent`, `-s, --skill`, `-l, --list`,
`-y, --yes`, `--all` (`src/cli.test.ts:8-31`).

Notable contents:
- `add <package>` block documents source forms (shorthand, `notion`, Notion page URL, GitHub URL).
- `update [skills...]  Update skills to latest versions (alias: upgrade)`.
- `experimental_install Restore skills from skills-lock.json (alias: install, i)`.
- Global flags section only documents `--help, -h` and `--version, -v`.
- `check` is absent.
- The `Examples:` column width is computed from the longest example that has a description,
  so alignment depends on the example list (implementation detail worth replicating only if
  byte-identical output matters).

### 1.4 Subcommand `--help` / `-h` short-circuit

Before dispatch, if the first token is not one of `--help`, `-h`, `--version`, `-v` **and**
any remaining argument equals `--help` or `-h`, the CLI prints help and returns **without
running the subcommand**. `remove`/`rm`/`r` get the remove-specific help, everything else gets
the top-level help. Exit code 0.

Consequences (**observed**, `src/cli.ts:339-352`, `src/cli.test.ts:88-133`):
- `skills update --help` does **not** run the update flow.
- `skills use --help` prints the **top-level** help — `getUseHelp()` in `src/use.ts:366-381`
  is unreachable from the CLI (only reachable through the programmatic API).
- `skills <unknown> --help` also prints help and exits 0 (no unknown-command error).
- Any flag *value* that literally equals `--help`/`-h` triggers this too, e.g.
  `skills remove --skill --help` prints help instead of erroring (**assumed** consequence of
  the string check; the code compares tokens, not parse results).

### 1.5 `--version`

Prints the `version` field of the package's `package.json` (read relative to the compiled
`dist/`, falling back to `0.0.0` if unreadable). v1.9.0 prints `1.9.0`. Exit code 0.
Extra arguments after `--version` are ignored. **observed** (`src/cli.ts:22-32`, `430-437`,
`src/cli.test.ts:34-47,82-85`).

### 1.6 Unknown command / unknown flags

- Unknown **command**: prints, to **stdout**, the diagnostic `[SKILLS_UNKNOWN_COMMAND] Unknown
  command: <token>` followed by `╰▶ fix: Run skills --help for usage.`, and sets
  `process.exitCode = 1`. Exact snapshot in `src/cli.test.ts:65-69`.
- Unknown **flags** are per-command:
  | Command | Unknown flag behavior |
  | --- | --- |
  | `add`, `install`, `experimental_install`, `sync`, `list`, `remove`, `update`, `check`, `find` | silently ignored (**observed**: parser returns `unknown`, these handlers never inspect it — `src/add.ts:3665-3706`, `src/list.ts:61-69`, `src/remove.ts:582-597`, `src/update.ts:160-173`, `src/find.ts:41-63`, `src/sync.ts:1078-1107`) |
  | `use` | rejected: `Unknown option: --wat`, exit 1 (`src/use.ts:97-113`) |
- A global `--color` / `--no-color` is **not** a real command: `skills --color` falls through to
  unknown-command (exit 1), even though `--color` affects color detection (§10.1).
  **observed** (`src/color.ts:24-26`, `src/cli.ts:439-442`)

### 1.7 `skills install <source>` vs `skills install` (restore)

| Invocation | Behavior |
| --- | --- |
| `skills install owner/repo` | identical to `skills add owner/repo` (same parser, same options) |
| `skills install` (no positional source) | restore from `./skills-lock.json` |
| `skills i` (no source) | restore |
| `skills experimental_install` | restore, always |
| `skills add` (no source) | **not** a restore — runs add's interactive flow |

Evidence: `src/cli.ts:368-395`. **observed** (`docs/commands.md` documents the alias explicitly.)

Restore CLI surface (observation only; engine is out of scope):
- Options are parsed by `parseSyncOptions` (so `-a/--agent`, `-y`, `--copy`, `--dry-run`,
  `--frozen-lockfile`, `--no-frozen-lockfile`, `-r`, `--include`, `--exclude`, plus sync-only
  flags) and `--force` is detected by raw `args.includes('--force')`.
  **observed** (`src/install.ts:52-58`)
- Frozen mode defaults to `isCI()` (§11.1).
- Exit code 1 when: frozen and the lock is missing/empty; a source cannot be restored; a locked
  skill is `not found in <source>`; any per-target link failure; or a sync of `node_modules`
  entries fails. A missing lock **without** frozen prints a warning and exits 0.
  **observed** (`src/install.ts:61-77`, `218-316`)
- Messages: `Restoring N skills from skills-lock.json into .agents/skills/`,
  `N skills already installed (use --force to reinstall)`,
  `Restored <names>`, `<names> not found in <source>`,
  `Restored N skills; failed to restore M skills from skills-lock.json: <names>`.
  **observed**

---

## 2. Argument parser (`src/cli-args.ts`)

Built on `node:util.parseArgs` in **non-strict** mode with `allowPositionals: true` and token
collection; validation is then done in this module so a single call can report every problem.

### 2.1 Option spec types

| Type | Space form | `=` form | Notes |
| --- | --- | --- | --- |
| `boolean` | `--json` | `--json=true` → **error** | grouped short switches supported (`-gy`) |
| `string` | `--owner vercel` | `--owner=vercel` | exactly one value per occurrence; repeats accumulate in the array |
| `list` | `--agent a b c` (consumes following positionals until the next option) | `--agent=a` (exactly one value, following token stays positional) | repeats accumulate |

Evidence: `src/cli-args.ts:5-25`, `93-100`; tests `src/cli-args.test.ts:18-36`. **observed**

### 2.2 Rules, with examples

| Rule | Behavior | Evidence |
| --- | --- | --- |
| `--flag=value` vs `--flag value` | same single value for `string`/`list` | `cli-args.ts:76-93`, test `:18-24` |
| short alias | `-a cursor`, `-a=cursor`, `-acursor` all yield `['cursor']` | `cli-args.ts:76-80`, test `:21-23` |
| `list` greediness | space form takes every following **positional**; stops at any option token or `--` | `cli-args.ts:95-100`, test `:26-36` |
| `*` | not expanded by the parser; it is a plain value that commands interpret (`--skill '*'`, `--agent '*'`) | `list.ts:90-104`, `remove.ts:110-114` |
| `--` | terminates option parsing; everything after is positional, including `--global` | `cli-args.ts:59`, test `:47-52` |
| option-like values | space form rejects values starting with `-` (`--agent -y` → error, and `-y` is **swallowed**, not parsed as a switch); `=` form accepts them (`--agent=-x`) | `cli-args.ts:83-91`, test `:54-62`, `:38-41` |
| empty value | `--agent=` and `--metadata` (at end) → `<option> requires <value label>` | `cli-args.ts:83-91`, test `:54-62` |
| boolean with value | `--global=true` → `--global does not take a value`; the value is dropped | `cli-args.ts:70-74`, test `:64-70` |
| unknown options | returned as `unknown` using the raw name (`--foo`, `-F`), never as `errors` by the parser | `cli-args.ts:65-69`, test `:72-78` |
| prototype keys | `--toString`, `--__proto__` are unknown, not spec hits (`Object.hasOwn`) | `cli-args.ts:65`, test `:80-83` |
| error accumulation | all errors collected; commands either print all of them or throw the first | `cli-args.ts:103-109` |
| error text | `${option} requires ${value}` where `value` comes from the spec (`an agent name`, `a skill name`, `a GitHub owner`, `a JSON value`, `a pattern`, `a subcommand name`); or `a value` when unspecified | `diagnostics.ts:24-29`, `cli-args.ts:86-89` |

Two quirks a port must decide about:
1. **Swallowed next token.** `--agent -y` reports an error *and* loses `-y`. **observed**
2. **conflicting-argument error text.** There is no generic "conflicting arguments" error;
   conflicts are per command:
   - `use`: `Conflicting skill selectors: source selects "A" but --skill selects "B". Provide one selector.`
   - `use`: `Only one --skill value can be provided`
   - `remove`: `Cannot combine --all with specific skill names.` + a two-line fix with an example
   - `find`: `--owner must be a valid GitHub owner`
   Evidence: `diagnostics.ts:33-44,105-116`, `use.ts:106-108`, `find.ts:47-60`. **observed**

---

## 3. `list` / `ls`

### 3.1 Options

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `-g, --global` | boolean | false (project) | list the global scope instead of the project scope |
| `-a, --agent <names...>` | list | none | filter to specific agents; validated against known agent keys |
| `--json` | boolean | false | machine-readable output, no ANSI, untruncated agent list |

There is **no** "both scopes" mode: `scope = options.global === true`. **observed**
(`src/list.ts:61-69`, `82-86`; test `src/list.test.ts:86-90` shows `-a` stops at `-g`).

### 3.2 What counts as installed

`listInstalledSkills({global, agentFilter})` scans, in this order:
1. the canonical dir (`<cwd or $HOME>/.agents/skills`),
2. each **detected** agent's skills dir for the scope,
3. for project scope, Eve subagent dirs `agent/subagents/<name>/skills`,
4. fallback: skill dirs of **undetected** agents, but only if the directory exists.

A directory counts only if it is a directory (or a symlink to one) **and** contains a
`SKILL.md` that parses (frontmatter with non-empty string `name` and `description`; skills
marked `internal` are excluded unless `INSTALL_INTERNAL_SKILLS=1`). Results are deduped by
`scope:name`; scanning an agent-specific dir attributes the skill to that agent, scanning the
canonical dir probes each agent's dir for the skill. **observed**
(`src/installer.ts:1211-1420`, `src/skills.ts:41-90`, tests `src/list.test.ts:358-384`)

Order of the human/JSON list follows the scan order (canonical first, then agents as
configured) and directory iteration order; nothing sorts it. **observed** (no sort call in
`src/list.ts`).

### 3.3 Lock comparison and statuses

Lock source: global scope → `getAllLockedSkills()` (global `.skill-lock.json`);
project scope → `skills-lock.json` in cwd. A lock entry is matched by exact key first, then by
`sanitizeName(key)` (so `ce:review` matches folder `ce-review`).

| Status | Condition | Where it appears |
| --- | --- | --- |
| `tracked` | on disk, lock entry exists, hash matches (or entry has no `computedHash`) | JSON + human (`Source: <source>`) |
| `untracked` | on disk, **no** lock entry | JSON + human (`Source: untracked`) |
| `modified` | on disk, lock entry exists and folder hash ≠ `computedHash` (also when the entry has **no** hash) | JSON + human (`modified (differs from the lock)`) |
| `missing` | in the lock, not on disk | JSON + `Missing` section (human) |

- `modified` is computed **only for project scope** (`if (!scope)`) and only when the lock
  entry has a `computedHash`. A lock entry without a hash is reported `tracked` in the CLI list
  path (because `modifiedNames` is only filled for hashed entries) even though
  `getLockedSkillState` would call it `modified`. **observed** (`src/list.ts:129-151` vs
  `src/local-lock.ts:235-247`) — see Open questions.
- `missing` is computed only when **no** `--agent` filter is given (a filter hides on-disk
  skills, so absence would be a false positive). **observed** (`src/list.ts:122-127`, test
  `src/list.test.ts:256-260`)
- An unreadable/corrupt project lock prints `Warning: <diagnostic>` to **stderr** and is
  treated as `{}` (listing is read-only). Exit code stays 0. **observed** (`src/list.ts:71-80`)

### 3.4 Human output

```
Project Skills                       <- "Global Skills" with -g

<name padded> <path padded>
  Agents: Claude Code, Cursor  Source: owner/repo
```

- `path` is `shortenPath(canonicalPath)`: **homedir prefix is replaced by `~` first**, then a
  cwd prefix becomes `.` — so a project under `$HOME` prints as `~/proj/.agents/skills/x`.
  **observed** (`src/list.ts:38-47`)
- `Agents:` lists display names; at most 5 are shown, then ` +N more` (`formatList`).
  With zero agents: `not linked` (yellow). **observed** (`src/list.ts:52-59`, `208-210`)
- `Source:` is the lock `source` (sanitized, single line) or `untracked`; when the skill was
  installed with `--rename`, ` (renamed from <original>)` is appended.
- `modified` appends `modified (differs from the lock)`.
- Alignment padding is per group with the maximum name/path length in that group.
- If lock entries carry `pluginName`, skills are grouped: groups sorted alphabetically, header
  is Title Case of the kebab-case plugin name, skills indented by two spaces; remaining skills
  go under a `General` header (only when at least one group exists). Otherwise a flat list.
- Missing section:

```
Missing (in the lock file, not on disk)
  gone-skill  Source: owner/repo
```

- Empty state (no installed and no missing):

```
No project skills found.
Try listing global skills with -g
```
(global variant: `No global skills found.` / `Try listing project skills without -g`)

Evidence: `src/list.ts:184-327`, tests `src/list.test.ts:171-176,234-254,263-283`. **observed**

### 3.5 `--json` schema

`JSON.stringify(array, null, 2)` — pretty-printed, no ANSI, exit code 0.

```json
[
  {
    "name": "beta",
    "path": "/abs/.agents/skills/beta",
    "scope": "project",
    "agents": ["Claude Code", "Codex"],
    "source": "owner/repo",
    "sourceUrl": "https://github.com/owner/repo",
    "sourceType": "github",
    "renamedFrom": "alpha",
    "status": "tracked"
  },
  {
    "name": "gone-skill",
    "path": null,
    "scope": "project",
    "agents": [],
    "source": "owner/repo",
    "sourceUrl": null,
    "sourceType": "github",
    "renamedFrom": null,
    "status": "missing"
  }
]
```

- `agents` = agent **display names** (not keys), never truncated in JSON.
- Absent lock fields serialize as `null` (`source`, `sourceUrl`, `sourceType`, `renamedFrom`).
- `scope` is `"project"`/`"global"` from the entry's scope; missing entries use the requested scope.
- Empty list → `[]` on one line.
- `status` is one of `tracked`, `modified`, `untracked`, `missing`.
- The CLI test asserts no `\x1b[` in JSON output.

Evidence: `src/list.ts:153-182`; tests `src/list.test.ts:107-169,234-245`, `tests/rename.test.ts:198-204`.

### 3.6 Exit codes

| Situation | Code |
| --- | --- |
| normal (including empty) | 0 |
| invalid `--agent` value(s) | **1** — via `process.exit(1)`, message printed to **stdout** (`Invalid agents: ...` + `Valid agents: ...`), so `--json` output is not JSON in this case |
| corrupt project lock | 0 (warning on stderr) |

Evidence: `src/list.ts:88-104`; test `src/list.test.ts:343-348`. **observed**

---

## 4. `remove` / `rm` / `r`

### 4.1 Options and selection grammar

| Flag | Type | Effect |
| --- | --- | --- |
| positional names | strings | skills to remove (also accepted via `-s/--skill`, both are concatenated: `[...skill values, ...positionals]`) |
| `-s, --skill <names...>` | list | same as positionals |
| `-g, --global` | boolean | operate on the global scope (`~/.agents/skills` + agents' global dirs + global lock) |
| `-a, --agent <names...>` | list | restrict removal to these agents; **no agent filter → all known agents** |
| `-y, --yes` | boolean | skip confirmation |
| `--all` | boolean | remove every **lock-tracked** skill; implies `-y` |

Selection rules (**observed**, `src/remove.ts:98-297`, `51-88`, `582-597`):

1. `skills remove --skill '*'` is rewritten to `--all` (the `*` name is filtered out).
2. `--all` **plus** any named skill (positional or `--skill`) is refused:
   `Cannot combine --all with specific skill names.` + fix block, `process.exit(1)`.
3. A requested name resolves by `sanitizeName(name)` against (a) on-disk folder names and
   (b) lock keys, with **lock keys winning**, so `ce:review` ↔ folder `ce-review` both work,
   matching is case-insensitive, duplicates collapse, and lock-only entries (folder already
   gone) are still removable.
4. If a requested name matches no skill/lock key, it is treated as an **exact lock source**:
   every lock entry whose `source` equals the string is selected
   (`skills remove owner/repo`). The comparison is exact string equality, case-sensitive.
5. No names and no `--all` → interactive `multiselect` over `scanInstalledSkillNames()`
   (`Select skills to remove (space to toggle)`, `required: true`).

`--agent '*'` is **not** handled in `remove`: `*` fails agent validation
(`Invalid agents: *`) and exits 1, despite `docs/commands.md` documenting
`skills remove my-skill --agent '*'`. **observed** (`src/remove.ts:161-173`, `240-248`;
contrast `docs/commands.md` ~line 300). See Open questions.

### 4.2 Lock-tracked-only rules for `--all`

- `--all` selects `Object.keys(lock)` — including **stale** keys with no folder — never the
  disk scan.
- Installed folders with no lock key are reported and kept:
  `Skipped N untracked skill(s) not recorded in the lock file: a, b` with fix
  `Remove them by name, e.g. skills remove <name>` (warning level, stdout via clack).
- With nothing tracked: warning above, then `No lock-tracked skills found to remove.`
  (global wording: `No globally managed skills found to remove.`), exit 0.
- Named removal of an untracked skill is allowed (point 4/3 above).

Evidence: `src/remove.ts:146-153,177-238`; tests `src/remove.test.ts:634-682`. **observed**

### 4.3 What actually gets deleted

For each selected skill, for each target agent (all known agents unless `-a`):

1. Compute install path(s): the agent dir path, plus the legacy/native dir
   (`<agent dir>/<sanitizedName>`), plus Eve subagent dirs for the `eve` agent. The canonical
   path is skipped in this loop (handled separately).
2. Skip paths that do not exist (`lstat`). Remove with `rm -r -f` — a symlink is **unlinked**,
   not followed (test: a symlinked directory's target survives).
3. Guard: for a **non-symlink** path and project scope, if the path is an *unmanaged* project
   dir it is **kept** with `Kept <path>: it is not an unmodified copy installed by skills`:
   - first path segment relative to cwd starts with `.` (dot dirs are always agent-owned → removable),
   - otherwise kept if there is no lock entry,
   - kept if the lock entry is `sourceType: 'local'` and the source path overlaps,
   - kept if the folder hash differs from the lock's `computedHash`.
   This is the protection for plain agent dirs that double as source folders (`skills/`,
   `data/skills`, `agent/skills`).
4. Canonical folder: removed **only if** no remaining (non-target) *detected* agent still has a
   path for the skill. Symlink-mode skills therefore survive as long as another agent links them.
5. Lock entry: deleted only when the canonical copy was removed (`!isStillUsed`); otherwise the
   entry is kept so the skill stays updatable (test: removing from one of two agents keeps the key).

Evidence: `src/remove.ts:404-539`, helpers `381-392`; tests `src/remove.test.ts:236-269,687-753,862-933`.

### 4.4 Output, prompts, idempotency

Non-interactive sequencing (**observed**):
1. Agent detection may print ` <Agent> Agent detected — removing non-interactively` and force `-y`.
2. Spinner `Scanning for installed skills…` → `Found N unique installed skill(s)`.
3. Validation of `--agent` values (before any selection prompt).
4. Scan output can include `Could not scan directory <dir>: <err>` warnings.
5. If no `-a`: `Targeting N potential agent(s)`.
6. Without `-y`: blank line, `Skills to remove:`, one `  • <name>` per skill, blank line, then
   `Are you sure you want to uninstall N skill(s)?`; decline or cancel →
   `Removal cancelled`, **exit 0**.
7. Spinner `Removing skills…` → `Removal process complete`.
8. Green `Successfully removed N skill(s)`; on failures red `Failed to remove N skill(s)` plus
   `  ✗ <skill>: <error>`.
9. Blank line, then green `Done!`.

Empty/edge results:
| Situation | Message | Exit |
| --- | --- | --- |
| nothing installed and nothing resolvable | `No skills found to remove.` | 0 |
| named skill matches nothing (with installs present) | `No matching skills found for: <names>` (error styling, but no exit code set) | **0** |
| directory/symlink without `SKILL.md` | not offered, `No matching skills found` | 0 |
| project-scope remove of a globally tracked skill (cwd is `$HOME`) | `Tracked in the global lock: <names>` + `Remove globally with: skills remove <names> -g`, exit 1, nothing deleted | 1 |
| invalid `--agent` | `Invalid agents: ...` / `Valid agents: ...` | 1 |
| `--all` + names | conflict error | 1 |
| corrupt `skills-lock.json` | diagnostic via `main()` catch on stderr | 1 |

Idempotency: removing an already-removed skill is harmless — the second run finds no
installed folder and no lock key, prints `No skills found to remove.` (or
`No matching skills found for: ...` when other skills are still installed / lock still exists)
and exits 0. Removing a lock-only (stale) entry reports success and cleans the lock.
**observed** (`src/remove.ts:155-158,183-196`; tests `src/remove.test.ts:90-155,283-288`)

Note the asymmetry: the *first* run of `remove --all` can print `Skipped ... untracked ...`,
the second prints `No skills found to remove.` because the scan is empty.

Removal is not transactional: each skill is independent, failures are collected per skill, and
the process still exits 0 even when some skills failed (only the messages differ). **observed**
(`src/remove.ts:279-297`; no `exitCode` assignment there).

---

## 5. `update` / `upgrade`

`runUpdate(args, mode='update')`. Options: `-g/--global`, `-p/--project`, `-y/--yes`, and
positional skill names (case-insensitive exact match filter). Unknown flags ignored.
**observed** (`src/update.ts:160-173`, `262-266`)

### 5.1 Scope detection

| Inputs | Scope | Prompt? |
| --- | --- | --- |
| any skill names + `-g` | `global` | no |
| any skill names + `-p` | `project` | no |
| any skill names, no scope flag | **`both`** | no |
| `-g` only | `global` | no |
| `-p` only | `project` | no |
| `-g` and `-p` | `both` | no |
| nothing, `-y` **or stdin not a TTY** | `project` if `hasProjectSkills(cwd)` else `global` (never `both`) | no |
| nothing, TTY, no `-y` | interactive `select` with `Project` / `Global` / `Both`; cancel → `Cancelled`, exit 0 | yes |

`hasProjectSkills(cwd)` is true when `./skills-lock.json` exists **or**
`./.agents/skills/<dir>/SKILL.md` exists for at least one subdirectory.
Evidence: `src/update.ts:181-260`; test `tests/update.test.ts:1495-1518`. **observed**

Header output: targeted update → `Updating <a, b>…`; otherwise
`Checking for skill updates…`, then a blank line. In `both` scope (unfiltered) it prints
`Global Skills` and `Project Skills` headers around each phase. **observed**
(`src/update.ts:1391-1424`)

### 5.2 Global phase

1. Read the global lock. Empty (and no filter) →
   `No global skills tracked in lock file.` + `Install skills with npx skills add <package> -g`.
2. Classify each locked skill:
   - `well-known` with `sourceBaseUrl` + `wellKnownDigest` → grouped by base URL, digest check;
   - `download` with `sourceUrl` → re-download;
   - missing `skillFolderHash` or `skillPath` → **skipped** with reason:
     `Local path`, `Git URL`, `Well-known skill`, `Private or deleted repo` (no hash),
     `No skill path recorded` (no path), else `No version tracking`.
3. Group remaining by `source` **and** `ref` (so two refs of one repo are checked separately).
4. Check per group:
   - `github`: Trees API (`fetchRepoTree`); if unavailable or if a locked `skillPath` is absent
     from the tree, fall back to a Git clone and print
     `  Skill paths changed; resolving via Git clone` or
     `  GitHub API unavailable; checking via Git clone`.
   - other sources: always clone.
   - Prints `Checking skills from source: <source>` for each group (line cleared first on TTY).
   - Failure → `  ✗ Failed to check skills from <source>` and the group is not updated
     (no fail count increment in that branch — see Open questions).
5. An update is needed when the upstream hash differs **or** the skill is missing from disk.
6. Upstream deletions: `Warning: The following skills from <source> appear to have been deleted upstream:`
   with a bullet list, then (interactive only) `Would you like to remove the local copies of these deleted skills?`;
   on confirm it calls `removeCommand([name], {yes: true, global: true})`. Non-interactive prints
   `Skipping deletion in non-interactive mode.` and keeps everything. Ambiguous relocations are
   listed under `Warning: Multiple current paths match these skills from <source>; skipping them rather than deleting or migrating the wrong skill:`.
7. No updates: `✓ All global skills are up to date` (suppressed when another category already
   reported changes, or when a name filter was given).
8. Updates: `Found N global update(s)`, then for each `Updating <name>…`, then
   `  ✓ Updated <name>` or `  ✗ Failed to update <name>: <error>` (plus per-agent
   `  ✗ <agent>: <error>` lines).
9. Skipped entries are printed at the end:
   `N skill(s) cannot be checked automatically:` grouped by install source, each with
   `To update: npx skills add <source> -g -y`.

Evidence: `src/update.ts:744-1024`, `276-332`, `351-414`; tests `tests/update.test.ts:726-1448`. **observed**

### 5.3 Project phase

1. `getProjectSkillsForUpdate(filter)` reads `skills-lock.json`, skipping
   `sourceType: 'node_modules'` and `'local'`. Empty (and no filter) →
   `No project skills to update.` + `Install project skills with npx skills add <package>`.
2. Entries without `skillPath` are "legacy" and cannot be updated in place:
   `N project skill(s) cannot be updated automatically (installed before skillPath tracking):`
   with `To refresh: npx skills add <reinstall> -y` (or a message that only an ambiguous
   shorthand is available).
3. `well-known` and `download` entries are handled like the global phase (digest / re-download,
   with `installMode` and `subagents` preserved from the lock).
4. Remaining entries are grouped by `sourceUrl||source` **and** `ref`, cloned concurrently
   (`src/update-source.ts` builds the clone URL; GitHub shorthand expands to
   `https://github.com/<source>.git`; a generic-git/gitlab entry with only an ambiguous
   shorthand and no `sourceUrl` **fails closed**: `✗ Cannot update <source>: skills-lock.json is
   missing sourceUrl for this generic Git source`).
5. Target agent list for display: agents whose project root dir exists, plus `Universal` when
   `.agents` exists → `Updating for: Universal, Claude Code, ...`, then
   `Checking N skill(s)…`.
6. Changed-content detection: hash the clone the way `add` records `computedHash`, compare to
   the lock's `computedHash`, and require the skill to be present on disk. Path relocation
   (locked path ≠ discovered path) alone also triggers a reinstall.
7. Install mode is preserved (`entry.installMode === 'copy'` → copy, otherwise the default),
   Eve `subagents` are fanned out again, and renamed entries are reinstalled under the installed
   name while the new lock keeps `skillName`.
8. Success/failure lines as in the global phase; lock is updated only when the hash is known and
   a target accepted the skill.
9. Up-to-date summary: `✓ All project skills are up to date` when every updatable skill and
   download is current and no well-known work happened, else
   `N project skill(s) already up to date`.

Evidence: `src/update.ts:1059-1322`, `1328-1372`; tests `tests/update.test.ts:144-724`,
`tests/update-install-mode.test.ts`, `tests/update-lock-only-walk.test.ts`,
`tests/update-missing-skill.test.ts`, `tests/rename.test.ts:142-161`. **observed**

### 5.4 Targeted updates, partial failures, exit codes

- Targeted (`skills update foo bar`) filters both scopes' lock keys by case-insensitive exact
  name. If nothing matched: `No installed skills found matching: foo, bar`.
- Per-skill failures are counted, not fatal; the run continues.
- Summary (always followed by a blank line):

| Line | Condition | Exit |
| --- | --- | --- |
| `✓ Updated N skill(s)` | `totalSuccess > 0` | — |
| `Failed to update N skill(s)` | `totalFail > 0` | **1** |
| `N skill(s) can be updated; run npx skills update to install` | `check` mode found outdated | **1** |
| `N skill(s) differ from skills-lock.json; run npx skills install --force to restore them` | `check` mode found modified project skills | **1** |

Evidence: `src/update.ts:1374-1460`; tests `tests/update.test.ts:1471-1492`. **observed**

`update` never exits 1 merely because nothing was found.

### 5.5 Source reconstruction (relevant to "re-resolution of source/ref")

`src/update-source.ts` decides the `add`-style source string used to refetch:

- `formatSourceInput(url, ref)` → `url#ref` when a ref exists.
- `skillPath: 'skills/x/SKILL.md'` → folder `skills/x`; root `SKILL.md` → empty folder, so no
  trailing slash (`owner/repo`, not `owner/repo/`).
- The folder is appended only when the source can carry a subpath: not `git@…`, not `ssh://…`,
  not ending in `.git`, and for `http(s)` only on `github.com`/`gitlab.com`.
- Non-GitHub lock entries prefer `sourceUrl`; a bare `owner/repo` with `sourceType` `git` or
  `gitlab` and no `sourceUrl` returns `null` (fails closed rather than guessing GitHub).
- When the folder cannot be appended, updates use `--full-depth` discovery
  (`shouldUseFullDepthForUpdate`).

Evidence: `src/update-source.ts:17-149`; tests `src/update-source.test.ts`. **observed**

---

## 6. `check`

`check` is `runUpdate(args, 'check')` with, additionally:
- `-y` forced (never prompts, never installs),
- `reportOutdated(name)` replaces installation and prints `↑ <name> has an update`,
- for the project scope only, a modified-skill report: `✎ <name> has local changes`
  (hash differs from `computedHash`, including entries with no hash),
- deletions upstream are only reported (non-interactive skip path),
- targeted names (`check foo`) filter both scopes.

Output order: `Checking for skill updates…`, blank line, per-scope messages, then the summary.

Exit code is **1** when any of: an update is available, a project skill has local changes, or an
install/check failure was counted. Exit 0 when everything is current (including
`✓ All project skills are up to date`). Only line-ending-only differences (CRLF↔LF) do **not**
count as local changes.

Evidence: `src/update.ts:1374-1460`, `1328-1351`; tests `tests/check.test.ts:48-84`. **observed**

`check` is absent from `--help`/banner but present in `README.md` and `docs/commands.md`.

---

## 7. `use`

`skills use <source>[@<skill>] [options]`. Never prints a logo. Source resolution follows
`add`'s parser (out of scope); the observable contract:

| Flag | Meaning |
| --- | --- |
| `-s, --skill <name>` | select the skill; repeating → `Only one --skill value can be provided` |
| `-a, --agent <name>` | exactly one, must be `claude-code`, `codex` or `sarvam-code`; `*` → `skills use --agent does not support '*'; specify exactly one agent.`; two values → `skills use --agent accepts exactly one agent.` |
| `--full-depth` | search nested dirs like `add --full-depth` |
| `-h, --help` | unreachable via CLI (§1.4) |

Validation errors are collected and printed (stderr, one per line) followed by `exit 1`;
a missing source additionally prints the `use` help text.

Evidence: `src/use.ts:90-113`, `159-187`, `469-496`, `570-575`; test `src/use.test.ts:103-117`. **observed**

### 7.1 Temp dir behavior

- A temp root is created with `mkdtemp(<os tmpdir>/skills-use-)`; the skill is materialized
  into `<tempRoot>/<sanitizeName(directoryName || name)>` (blob snapshots and well-known file
  maps are written with path-traversal checks; disk skills are copied recursively).
- `hasSupportingFiles` is true when the materialized folder contains anything besides
  `SKILL.md` (case-insensitive, recursively).
- The **clone/download** temp dir is cleaned up after materialization; the `skills-use-` temp
  dir is **kept** (it is referenced by the prompt so the agent can read supporting files).
  **observed** (`src/use.ts:135-157`, `298-306`) — nothing deletes `tempRoot`.

### 7.2 Prompt format (exact)

Sections joined with `\n\n`, then a trailing `\n`:

```
You are being given a Skill to execute for the user's next request.

Use the following SKILL.md as your instructions:

<SKILL.md>
<verbatim SKILL.md contents>
</SKILL.md>
```

When supporting files exist, a fourth section is appended:

```
Supporting files for this skill were downloaded to:
<absolute skillDir>

When the SKILL.md references relative paths, read them from that directory.
```

Evidence: `src/use.ts:115-133`; tests `src/use.test.ts:120-141,366-391`. **observed**

### 7.3 stdout purity and `--agent`

- Without `--agent`, the prompt is written with `process.stdout.write(prompt)` — no banner, no
  headings, no extra newline beyond the trailing `\n`, stderr empty. Tests assert
  `result.stderr === ''` and absence of logo / `skills add` text. Useful as
  `skills use … | claude`.
- Errors go to **stderr** via `console.error(formatError(...))`, then `process.exit(1)`; the test
  for a well-known file failure asserts stdout is exactly empty.
- With `--agent`, the prompt is **not** printed; instead the agent is spawned:
  `claude` (claude-code), `codex` (codex), `sarvam-code` (sarvam-code), invoked as
  `<command> <prompt>` (prompt as a single argv element, no shell) with `stdio: 'inherit'`
  (child owns the tty, interactive). The parent exits with the child's exit code when non-zero
  (`process.exit(code)`), 0 otherwise. `ENOENT` → `Could not launch <Agent>: command not found:
  <command>`, exit 1. `close` with a null code is treated as 1.
- Selection errors (`multiple skills, no selector`, `no matching skill`, ambiguous selector,
  conflicting `@skill` vs `--skill`) abort before any download and exit 1.
  `This source contains multiple skills. Specify exactly one skill:` lists names and gives two
  example commands.

Evidence: `src/use.ts:302-364`, `397-422`; tests `src/use.test.ts:186-238,353-443`. **observed**

Only `vercel`, `vercel-labs`, `heygen-com`, `remotion-dev` GitHub owners take the blob fast
path (relevant only as an optimization detail). **observed** (`src/use.ts:82-88`)

---

## 8. `find`

`skills find [query...] [--owner <owner>]`, aliases `search`, `f`, `s`. Prints the logo first
(unless in an agent) and a blank line.

### 8.1 Options and query

- `--owner <owner>`: last occurrence wins, trimmed, lowercased, validated against
  `^[a-z0-9](?:[a-z0-9-]{0,38})$` (case-insensitive). Invalid → `--owner must be a valid GitHub
  owner`; missing value → `--owner requires a GitHub owner`.
- Query = all positionals joined with a single space (so `find react native --owner Vercel` →
  query `react native`, owner `vercel`).
- Other flags are ignored (unknown options are not errors here).

Evidence: `src/find.ts:39-63`; tests `src/find.test.ts:10-35`. **observed**

### 8.2 Search API

- Base: `process.env.SKILLS_API_URL || 'https://skills.sh'`.
- `GET <base>/api/search?q=<query>[&owner=<owner>]` (URLSearchParams encoding; **no** `limit`
  or page parameter is ever sent).
- Response shape used:

```json
{ "skills": [ { "id": "<owner>/<repo>/<skill>", "name": "<skill>", "installs": 1234, "source": "<owner>/<repo>" } ] }
```

- Mapping: `name` = `sanitizeMetadata(skill.name)`, `slug` = `sanitizeMetadata(skill.id)`,
  `source` = `sanitizeMetadata(skill.source || '')`, `installs` as-is.
- Non-2xx, network error, or throw → `[]` (no error output, no non-zero exit).
- Result order is the API order (relevance); install counts are never used to sort the exact
  results.
- **Partial-match fan-out** (`searchSkills`): when the exact query returns **< 10** results and
  the query has **2–6** unique whitespace-separated terms, it re-queries once per dropped term
  (all in parallel), merges unseen slugs, sorts candidates by (number of subqueries that
  returned them desc, best rank asc, installs desc), and appends them, capped at **25 total**.
  `exactCount` marks how many leading entries matched every term.

Evidence: `src/find.ts:12-20,66-146`; tests `src/find.test.ts:37-186`. **observed**

### 8.3 Keyword mode (non-interactive) rendering

Triggered whenever a query is present (regardless of TTY). Exit code **0** in all cases,
including no results and invalid `--owner`.

```
Install with npx skills add <owner/repo@skill>

<source>@<name> [<installs badge>]
└ https://skills.sh/<slug>

... (blank line after each result)
```

- `installs` badge: `N installs` / `1 install`, `1.2K installs`, `3.4M installs` (one decimal,
  `.0` stripped); omitted when `installs <= 0`.
- When partial matches follow exact ones, a divider is printed before the first partial result:

```
No skills matched every term in "<query>". Partial matches:
```
or, when some exact matches existed:
```
Partial matches (some terms of "<query>"):
```
- No results:
  `No skills found for "<query>"` with ` from owner "<owner>"` appended when `--owner` was used.
- Errors (invalid owner / missing value) go to **stderr**, then
  `Usage: npx skills find <query> [--owner <owner>]`, and the function `return`s — **exit 0**.
- Every keyword search emits a `find` telemetry event (unless disabled).

Evidence: `src/find.ts:356-408`; tests `src/find.test.ts:82-108,176-186`. **observed**

### 8.4 Interactive mode

Only when there is no query **and** stdin is a TTY **and** not running inside an agent,
otherwise:

```
Tip: if running in a coding agent, follow these steps:
  1) npx skills find [query] [--owner <owner>]
  2) npx skills add <owner/repo@skill>

Usage: npx skills find <query> [--owner <owner>]
```
exit 0. **observed** (`src/find.ts:359-362,410-416`)

The interactive prompt is a hand-rolled fzf-style UI on raw stdin/stdout:
- hides the cursor, redraws in place (`\x1b[<n>A` + `\x1b[J`), restores cursor and raw mode on exit;
- min 2 characters before searching; adaptive debounce `max(150, 350 - len*50)` ms;
- shows up to **8** results; header `Search skills: <query>_`, hints
  `up/down navigate | enter select | esc cancel`, states `Start typing to search (min 2 chars)`,
  `Searching…`, `No skills found`;
- keys: printable ASCII `0x20–0x7e`, backspace, up, down, enter, esc, ctrl-c;
- enter with no highlighted result resolves `null`;
- on selection: prints blank, `Installing <bold name> from <pkg>…`, blank, then runs `add` for
  `<pkg> --skill <name>` in-process, then a footer:
  `View the skill at https://skills.sh/<slug>` when the owner/repo is confirmed **public**,
  else `Discover more skills at https://skills.sh`.
- cancel/no selection: `Search cancelled` + blank line, exit 0.

Evidence: `src/find.ts:148-335,418-458`. **observed** (UI text and keys), **assumed** for exact
redraw fidelity on exotic terminals.

---

## 9. `init`

`skills init [name]`. Prints the logo + blank line first (unless in agent). Synchronous.

| Input | Target | Displayed path |
| --- | --- | --- |
| `init my-skill` | `./my-skill/SKILL.md` (dir created recursively) | `my-skill/SKILL.md` |
| `init` (no arg) | `./SKILL.md`; skill name = `basename(cwd)` | `SKILL.md` |
| extra args | ignored (`args[0]` only) | — |

- If the target `SKILL.md` already exists: prints `Skill already exists at <displayPath>` and
  returns; **no overwrite, exit 0**.
- The name is YAML-escaped with `yaml.stringify(...).trimEnd()`, so names like `demo: x`,
  `my skill # best`, `true`, `123`, `[demo]`, `say "hello"` round-trip as strings.
- No name sanitization for the directory: `init ../x` would create outside cwd (**assumed**
  consequence of `join(cwd, skillName)` with no validation — see Open questions).

Template written verbatim (ends with a newline):

```markdown
---
name: <name>
description: A brief description of what this skill does
---

# <name>

Instructions for the agent to follow when this skill is activated.

## When to use

Describe when this skill should be used.

## Instructions

1. First step
2. Second step
3. Additional steps as needed
```

Output after writing:

```
Initialized skill: <name>

Created:
  <displayPath>

Next steps:
  1. Edit <displayPath> to define your skill instructions
  2. Update the name and description in the frontmatter

Publishing:
  GitHub:  Push to a repo, then npx skills add <owner>/<repo>
  URL:     Host the file, then npx skills add https://example.com/<displayPath>

Browse existing skills for inspiration at https://skills.sh/
```

(plus a trailing blank line). Evidence: `src/cli.ts:254-315`; tests `src/init.test.ts:22-127`
(inline snapshots). **observed**

---

## 10. Output conventions

### 10.1 Color / TTY

Single source of truth in `src/color.ts`:

```
colorEnabled = !(NO_COLOR || argv includes '--no-color')
            && !!(FORCE_COLOR || argv includes '--color' || CI || (stdout.isTTY && TERM !== 'dumb'))
```

- Implements no-color.org `NO_COLOR` (any non-empty value) and `FORCE_COLOR`.
- `CI` forces color on (unusual: most tools force it off).
- There is **no** win32 "always on" shortcut.
- Named colors are 256-color: `DIM` = `38;5;102`, `TEXT` = `38;5;145`, logo gradient
  `250,248,245,243,240,238`; plus `BOLD`, `CYAN` (36), `YELLOW` (33), `RESET`.
- `CLEAR_LINE` (`\r\x1b[K`) is emitted only when stdout is a real TTY, even if color is forced.
- Rule enforced by a test: no raw `\x1b[` literals outside `color.ts` (`src/color.test.ts`).
  `src/find.ts` and `src/prompts/search-multiselect.ts` are the exception (`\x1b[` cursor codes
  there).

**observed** (`src/color.ts:1-44`).

### 10.2 Unicode vs ASCII

No ASCII fallback mode. Output uses box-drawing/symbols unconditionally: `✓ ✗ • └ ▶ ↑ ✎ ◇ ◆ ■ ● ○ ◐ ❯ ▸ ▾`, the `███` logo, and the `╰▶` fix marker. Terminal width detection falls back
to 80 columns. **observed**

### 10.3 Terminal sanitization

All untrusted strings (skill names, descriptions, lock `source`, paths in warnings) pass
through `sanitizeMetadata` / `stripTerminalEscapes` before printing: CSI, OSC, DCS/PM/APC,
simple ESC sequences, C1 controls and raw control chars (except `\t`, `\n`) are stripped;
newlines collapse to spaces for metadata. This is CWE-150 defense and is tested
(`list` prints `Source: owner/ FORGED` for a multi-line source; discovery warnings strip
escapes from the reason). **observed** (`src/sanitize.ts:1-65`; tests `src/list.test.ts:263-283`,
`tests/skill-matching.test.ts:279-298`)

### 10.4 stdout vs stderr

| Producer | Stream |
| --- | --- |
| normal command output, help, banner, `--version` | stdout (`console.log`) |
| unknown-command diagnostic | **stdout** |
| `list` invalid agents | **stdout** |
| `find` option errors + usage | **stderr** (`console.error`) |
| `use` all errors + `use` help on missing source | **stderr** |
| `add` option errors (`Error: ...`), plus `[]` on stdout in `--json` mode | stderr / stdout |
| warnings via `warn()` (discovery skips, unmanaged dir kept) | **stderr** (`console.warn`) |
| diagnostics rethrown to `main().catch` (invalid lock, option errors from commands using `throwFirstError`) | **stderr**, `Error: <formatted>` |
| clack prompts/spinners/`p.log.*` | stdout (clack writes to stdout) |
| `use` prompt (the product of the command) | **stdout only**, nothing else |

Diagnostic formatting: `[CODE] why` plus an optional second line `╰▶ fix: <fix>` (ANSI-colored
when enabled, `[CODE]` bold).

**observed** (`src/diagnostics.ts:484-490`, `src/remove.ts`, `src/find.ts`, `src/use.ts`,
`src/cli.ts:386-392,445-459`)

### 10.5 Prompts and cancellation

| Prompt | Cancel key/answer | Result |
| --- | --- | --- |
| `remove` multiselect | esc / ctrl-c | `Removal cancelled`, exit 0 |
| `remove` confirm | `n` / esc | `Removal cancelled`, exit 0 |
| `update` scope select | esc / ctrl-c | `Cancelled`, exit 0 |
| `update` deletion confirm | decline | deletions skipped, run continues |
| `find` interactive | esc / ctrl-c | `Search cancelled`, exit 0 |
| add's search-multiselect | esc / ctrl-c / stdin EOF | resolves a cancel symbol; EOF is treated as cancel instead of hanging |

`remove` has **no** `!isTTY` guard: with stdin not a TTY and no names/`--all`, it enters the
clack multiselect anyway. Behavior there is clack-internal (**assumed**: prompt resolves as a
cancel or throws; either way it should be made explicit in a port). `update` and `add` do guard
non-TTY (`update`: auto scope; `add`: `SKILLS_PROMPT_REQUIRES_TTY`).

Evidence: `src/remove.ts:203-215,258-266`, `src/update.ts:229-259`, `src/find.ts:286-294`,
`src/prompts/search-multiselect.ts:602-615,709-715`, `src/diagnostics.ts:45-49`. **observed**

### 10.6 Concurrency / batching knobs (affect output ordering)

- `fetchConcurrency()` = `SKILLS_CLONE_CONCURRENCY` (positive integer) or **4**;
  `mapConcurrent` keeps result order while limiting in-flight fetches.
- `update` fetches **all** installable sources before installing any (test asserts a single
  `fetchSources` batch, then one install per skill), so the "Fetching N sources…" line appears
  once. **observed** (`src/concurrency.ts:1-25`, `tests/update.test.ts:1406-1448`)

---

## 11. Environment / agent detection (diagnostics inputs)

### 11.1 CI

`isCI()` = any of `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `CIRCLECI`, `TRAVIS`, `BUILDKITE`,
`JENKINS_URL`, `TEAMCITY_VERSION`. Used for the frozen-lockfile default and (through
`color.ts`) to force color. **observed** (`src/ci.ts:1-11`)

### 11.2 Agent detection

`detectAgent()` wraps `@vercel/detect-agent` (env-var based) and caches the result; it also
records the agent name for telemetry. Additional refinement: a Cursor result is only accepted
when `CURSOR_AGENT` is set or `CURSOR_EXTENSION_HOST_ROLE === 'agent-exec'` (the integrated
terminal also sets `CURSOR_TRACE_ID`); bare `cursor` is normalized to `cursor-cli`, which then
maps to agent type `cursor`.

`isRunningInAgent()` changes CLI behavior: no logo/banner, no interactive `find` prompt, and
`remove` forces `-y` with an informational line. Known env vars the upstream detector uses
(kept in sync by the test helper): `AI_AGENT`, `ANTIGRAVITY_AGENT`, `AUGMENT_AGENT`,
`CLAUDE_CODE`, `CLAUDE_CODE_IS_COWORK`, `CLAUDECODE`, `CODEX_CI`, `CODEX_SANDBOX`,
`CODEX_THREAD_ID`, `COPILOT_ALLOW_ALL`, `COPILOT_GITHUB_TOKEN`, `COPILOT_MODEL`, `CURSOR_AGENT`,
`CURSOR_EXTENSION_HOST_ROLE`, `CURSOR_TRACE_ID`, `GEMINI_CLI`, `OPENCODE_CLIENT`, `REPL_ID`.

**observed** (`src/detect-agent.ts:1-92`, `src/test-utils.ts:10-58`)

### 11.3 Other env vars that change CLI behavior

| Variable | Effect |
| --- | --- |
| `NO_COLOR`, `FORCE_COLOR`, `CI`, `TERM` | color (§10.1) |
| `DISABLE_TELEMETRY`, `DO_NOT_TRACK` | disable telemetry/audit requests |
| `SKILLS_API_URL` | override `find`'s base URL |
| `SKILLS_CLONE_CONCURRENCY` | parallel fetch limit |
| `SKILLS_CLONE_TIMEOUT_MS` | clone timeout (mentioned in the clone-timeout fix text) |
| `INSTALL_INTERNAL_SKILLS` | `1`/`true` → include `metadata.internal` skills in discovery/list |
| `GH_HOST` | GitHub host; public `github` lock entries are deliberately pinned to `github.com` during update checks |
| `GITHUB_TOKEN`, `GH_TOKEN` | API auth (source fetching) |
| `SKILLS_DOWNLOAD_MAX_BYTES`, `SKILLS_EXTRACT_MAX_BYTES`, `SKILLS_EXTRACT_MAX_FILES` | download/archive caps |

**observed** for the first four and `INSTALL_INTERNAL_SKILLS`/`GH_HOST`; the caps and timeout
are from diagnostic strings and docs.

### 11.4 Warnings

`warn(diagnostic)` prints `formatError(warning)` with `console.warn` (stderr) unless a handler
is installed via `withWarnings()` (used by the programmatic API). CLI warnings in scope:
- `SKILLS_SKILL_SKIPPED` — `⚠ Skipped <path> — <reason>` (unreadable/malformed SKILL.md during
  discovery; reason is escape-stripped);
- `SKILLS_REMOVE_UNMANAGED_DIR_KEPT` — `Kept <path>: it is not an unmodified copy installed by
  skills` + `fix: Delete it by hand if you no longer need it`;
- `SKILLS_BROKEN_SYMLINK`, `SKILLS_SYMLINK_OUTSIDE_SKILL`, `SKILLS_SYMLINK_CYCLE` — skip
  symlink problems during copy/install;
- `list`'s local-lock warning (`Warning: [SKILLS_INVALID_LOCAL_LOCK] ...`).

**observed** (`src/warnings.ts:1-24`, `src/skills.ts:67-76`, `src/remove.ts:381-392`,
`src/list.ts:71-80`, `src/diagnostics.ts:139-142,203-231`)

---

## 12. Exit codes (consolidated)

| Code | Situations (observed) |
| --- | --- |
| **0** | no args (banner or silent); `--help`/`-h`/`--version`/`-v`; any subcommand `--help`; `list` normal/empty/corrupt-lock-warning; `remove` success, "nothing to remove", non-matching name, user cancel; `update` success or nothing to do; `check` all-current; `use` success (prompt or agent exit 0); `find` any keyword outcome (including no results and invalid `--owner`), interactive cancel; `init` created or already-exists; `install` non-frozen with empty lock |
| **1** | unknown command; `add`/`install` option errors (and `[]` on stdout if `--json`); invalid `--agent` for `list`/`remove`; `remove --all` + names; project remove of a globally tracked skill; `use` any validation/selection/fetch error, and agent spawn `ENOENT`; `update` with ≥1 failed skill; `check` with ≥1 update or ≥1 locally modified project skill; `install --frozen-lockfile` with missing/empty lock or hash/source mismatch |
| **child's code** | `use --agent <a>` propagates the agent process's non-zero exit code |
| **1 (uncaught)** | unexpected exceptions are rethrown from `main()`, producing a Node stack trace on stderr. Only `SKILLS_INVALID_LOCAL_LOCK`, `SKILLS_OPTION_REQUIRES_VALUE`, `SKILLS_OPTION_TAKES_NO_VALUE` are caught and printed as `Error: <diagnostic>` |
| 1..N | reserved by signals/agent; nothing else sets custom codes |

Evidence: `src/cli.ts:386-392,430-442,445-459`, `src/use.ts:323-364,570-575`, `src/update.ts:1430-1449`,
`src/list.ts:88-104`, `src/remove.ts:119-125,161-173,219-238,279-297`, `src/find.ts:364-368,428-432`,
`tests/check.test.ts`, `tests/update.test.ts:1471-1492`. **observed**

---

## 13. Surprising / undocumented behavior (port checklist)

1. `skills install`/`i` with **no** source is a restore; `skills add` with no source is *not*.
   `experimental_install` is the explicit restore (`src/cli.ts:368-395`).
2. Subcommand `--help` is intercepted globally, so `skills use --help` never shows
   `use`'s own help text (`src/cli.ts:339-352` vs `src/use.ts:366-381`).
3. Unknown flags are silently ignored by every command **except `use`**
   (`src/cli-args.ts:65-69` plus per-command call sites).
4. `remove --agent '*'` errors with `Invalid agents: *`, contradicting
   `docs/commands.md` and the help text's "(use '*' for all agents)" phrasing for `-a`
   (`src/remove.ts:161-173`; help at `src/cli.ts:189`).
5. `remove` non-matching name uses error styling but exits **0** (`src/remove.ts:186-196`).
6. `remove` does not add a non-zero exit for failed removals (`src/remove.ts:288-296`).
7. `list --json` can emit a non-JSON error to stdout for an invalid `--agent`
   (`src/list.ts:94-104`).
8. `list` marks a hashed-less project lock entry `tracked`, while
   `getLockedSkillState` calls the same entry `modified` (`src/list.ts:129-151`).
9. `check` is a real command but hidden from `--help` and the banner.
10. `find` aliases include the single letters `f` and `s`, and `search` — undocumented.
11. `find` exits 0 even when `--owner` is invalid (stderr message only).
12. `update` with explicit skill names and no `-g/-p` silently means **both scopes** (no prompt),
    whereas `update` with no names auto-picks one scope (`src/update.ts:212-231`).
13. `update`'s non-interactive scope choice is project-if-project-skills-exist, else global —
    never "both" (`src/update.ts:229-231`).
14. `--all` on `remove` is a lock-ownership operation, not "everything on disk"; untracked
    skills are reported and kept (`src/remove.ts:146-182`).
15. Removing a skill from one agent can legitimately keep the canonical folder **and** the lock
    entry when another detected agent still links it (`src/remove.ts:476-520`).
16. `init` names are YAML-escaped but not path-sanitized (`src/cli.ts:254-270`).
17. `use` leaves its `skills-use-*` temp dir behind on purpose; no cleanup command exists.
18. `CI` forces **color on** (`src/color.ts:24-26`).
19. `--color`/`--no-color` are sniffed from `process.argv` but are not valid commands or
    parsed options; `skills --color` is an unknown command.
20. `list`'s empty-JSON branch is dead code (`src/list.ts:186-190`); empty JSON comes from the
    earlier `JSON.stringify([])`.

---

## Open questions

1. **`remove --agent '*'`.** Docs and help imply `'*'` means all agents, but the code validates
   it as an invalid agent and exits 1. Is the Go port expected to accept `'*'` (fixing the
   mismatch) or reproduce the error? `src/remove.ts:161-173,240-248`; `src/cli.ts:189`;
   `docs/commands.md` (`skills remove` table). **observed mismatch**
2. **`list` status for a hash-less lock entry.** CLI reports `tracked`; `getLockedSkillState`
   would say `modified`. Which is the intended contract for `list` and for `check`
   (`reportModifiedProjectSkills` uses `getLockedSkillState`, so `check` reports local changes
   while `list` does not). `src/list.ts:129-151`, `src/local-lock.ts:235-247`,
   `src/update.ts:1328-1351`.
3. **`remove` on a non-TTY with no names.** No guard exists; clack's multiselect behavior under
   a pipe is not verified here. Should a port fail fast with
   `SKILLS_PROMPT_REQUIRES_TTY`-style messaging, or cancel quietly?
   `src/remove.ts:197-215`.
4. **Whether `use --help` should print the command's own help.** The pre-dispatch check makes
   `getUseHelp()` unreachable; a port that keeps per-command help would diverge.
   `src/cli.ts:339-352`, `src/use.ts:366-381`.
5. **Global-phase check failures.** `✗ Failed to check skills from <source>` does not increment
   `failCount` in the tree/clone branch (unlike the project phase, which does), so `update`
   can exit 0 despite an unreachable source. Intentional or bug?
   `src/update.ts:871-874` vs `1206-1218`.
6. **Ordering guarantees for `list`.** No sort is applied to installed skills; JSON consumers
   may not assume alphabetical order. Is stable ordering part of the contract?
   `src/list.ts:106-127,153-182`.
7. **`init` path traversal.** `init ../x` writes outside cwd; is that an accepted hazard or
   should the port sanitize? `src/cli.ts:254-270`.
8. **Exact byte-level help/banner text.** Only structural assertions exist in tests; if the Go
   port must match snapshots, the `Examples:` column width and 256-color codes need
   verification against a real terminal. `src/cli.ts:35-221`, `src/cli.test.ts:8-31`.
9. **`find` fan-out cost/limits.** The 10-result / 6-term / 25-result caps are constants, not
   configurable; confirm they are contractual. `src/find.ts:95-146`.
10. **`install` (restore) accepting sync flags.** `runInstallFromLock` parses its args with
    `parseSyncOptions`, so `--dry-run`, `-r`, `--include`, `--exclude`, `--no-cleanup` are
    accepted on `skills install`; only some are meaningful. Which subset is the public
    contract? `src/install.ts:52-58`, `src/sync.ts:1078-1107`.

---

## Evidence index (tricky parts)

| Topic | Location |
| --- | --- |
| Dispatch table, `install` vs restore, logo suppression | `src/cli.ts:354-442` |
| Global `--help` pre-check | `src/cli.ts:339-352` |
| Banner / logo text | `src/cli.ts:35-90` |
| Full help text (sections, examples) | `src/cli.ts:92-221` |
| Remove-specific help | `src/cli.ts:223-252` |
| `init` template + output | `src/cli.ts:254-315` |
| Top-level error handling / exit codes | `src/cli.ts:445-459` |
| Parser rules (`--x=v`, list greediness, `--`, dash values) | `src/cli-args.ts:35-104` |
| Parser behavior tests | `src/cli-args.test.ts:17-99` |
| Diagnostic catalog (messages, codes) | `src/diagnostics.ts:16-380` |
| `formatError` rendering | `src/diagnostics.ts:484-490` |
| Color/TTY decision, `CLEAR_LINE` | `src/color.ts:20-44` |
| Terminal sanitization | `src/sanitize.ts:1-88` |
| Warnings channel | `src/warnings.ts:1-24` |
| Fetch concurrency + ordering | `src/concurrency.ts:1-25` |
| `list` options/scope/statuses/JSON | `src/list.ts:61-198` |
| `list` human rendering, groups, Missing | `src/list.ts:200-327` |
| Installed-skill scanning semantics | `src/installer.ts:1211-1420` |
| Skill parsing requirements (name/description) | `src/skills.ts:41-90` |
| Lock state (`tracked`/`modified`/`missing`) | `src/local-lock.ts:235-247` |
| `remove` selection resolution (+source match) | `src/remove.ts:51-88` |
| `remove` flow, prompts, guards | `src/remove.ts:98-297` |
| `remove` disk/lock mutation rules | `src/remove.ts:404-539` |
| `remove` parse (positional + `-s`, `--all` implies `-y`) | `src/remove.ts:582-597` |
| `update` scope detection | `src/update.ts:160-260` |
| `update` skip reasons / hints | `src/update.ts:276-332` |
| Global phase (API vs clone, deletions, summary) | `src/update.ts:744-1024` |
| Project phase (legacy, grouping, install mode) | `src/update.ts:1059-1322` |
| `check` mode + exit codes | `src/update.ts:1374-1460` |
| Source reconstruction rules | `src/update-source.ts:17-149` |
| `use` prompt builder / temp dir | `src/use.ts:115-157` |
| `use` run flow / stdout purity / agent launch | `src/use.ts:159-364` |
| `use` option validation | `src/use.ts:469-496` |
| `find` parser / API / fan-out | `src/find.ts:41-146` |
| `find` non-interactive rendering | `src/find.ts:356-408` |
| `find` interactive UI | `src/find.ts:148-335` |
| Restore CLI messages/exits | `src/install.ts:51-316` |
| Agent detection / CI detection | `src/detect-agent.ts:1-92`, `src/ci.ts:1-11` |
