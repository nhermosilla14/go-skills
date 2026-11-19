# `skills add` and the installation engine — behavioral specification

Source: `vercel-labs/skills` v1.9.0 (`/tmp/opencode/vercel-skills`, MIT).
Primary files: `src/add.ts`, `src/installer.ts`, `src/install.ts`, `src/agents.ts`,
`src/local-lock.ts`, `src/skill-lock.ts`, `src/api/install.ts`.
Evidence: `src/add.test.ts`, `src/add-prompt.test.ts`, `src/cli-args.test.ts`,
`tests/*.test.ts`, `e2e/rename.e2e.ts`, `docs/commands.md`.

Convention used below: **[obs]** = confirmed in source, tests, or upstream docs;
**[asmp]** = inferred (prompt-library default, untested branch, or reasoning).
Unless a line is tagged **[asmp]**, treat behavior claims as **[obs]**.
Source code is described structurally; only formats, keys, and literal user-facing
strings are quoted.

---

## 1. Command grammar

### 1.1 Dispatch and aliases

```
skills add <source> [options]          # canonical
skills a   <source> [options]          # alias
skills install <source> [options]      # alias; without <source> = restore lock
skills i   <source> [options]          # alias; without <source> = restore lock
```

* **[obs]** Only the **first positional** is used as the source. `skills add a b`
  installs from `a` and silently ignores `b` (no `SKILLS_MULTIPLE_SOURCES` check
  on this path; that diagnostic is only used by `use`).
* **[obs]** `install`/`i` with **no** positional is not `add`: it runs
  `runInstallFromLock` (restore `skills-lock.json`), even if add flags are present.
* **[obs]** `install`/`i` **with** a positional is plain `add`, including all
  unknown-option tolerance described in §1.3.
* **[obs]** The source is one of: GitHub shorthand (`owner/repo[//subpath][@skill]`),
  `github:`/`gitlab:` prefix, GitHub/GitLab/Azure/generic git URL, SSH URL,
  local path, `https://skills.sh/p/<pack>`, a well-known host URL, a Notion page
  or pack URL, or a direct download URL (raw `SKILL.md`, zip/tar archive).
* **[obs]** `owner/repo@v1.2.3` is treated as a **ref pin**, not a skill selector
  (`isVersionPin`); `owner/repo@skill-name` is a skill selector.
* **[obs]** `--help`/`-h` anywhere after the subcommand is intercepted in
  `cli.ts` **before** dispatch and prints the global help (there is no
  add-specific help page).
* **[obs]** `--` option terminator is honored by the argument parser.

### 1.2 Flags

| Flag | Short | Parser type | Repeatable | Default | Effect |
|---|---|---|---|---|---|
| `--global` | `-g` | boolean | no | absent ⇒ project, or interactive scope prompt | Global (user-level) install |
| `--agent <names…>` | `-a` | greedy list | yes (accumulates) | auto-detect / prompt | Target agents; `*` = every agent |
| `--skill <names…>` | `-s` | greedy list | yes (accumulates) | all if 1 skill or `-y`, else prompt | Select skills; `*` = all |
| `--list` | `-l` | boolean | no | false | Print available skills, install nothing, exit 0 |
| `--yes` | `-y` | boolean | no | false | Skip every prompt |
| `--all` | — | boolean | no | false | Sets `skill=['*']`, `agent=['*']`, `yes=true`; **overrides** explicit `--skill`/`--agent` regardless of order |
| `--copy` | — | boolean | no | false | Force copy mode instead of canonical+link |
| `--rename <name>` | — | string | yes (**last wins**) | none | Install the one selected skill under a new name |
| `--metadata <json>` | — | string | yes (**last wins**) | none | Attach valid JSON to the install telemetry event only |
| `--subagent <names…>` | — | greedy list | yes | `[root]` | Eve subagent targets; `root` or `.` = root agent; implies Eve |
| `--full-depth` | — | boolean | no | false | Search all subdirectories even when a root `SKILL.md` exists; also disables the GitHub blob fast path |
| `--json` | — | boolean | no | false | Machine-readable output; requires `-y`/`--all`; incompatible with `--list` |
| `--metadata` (invalid JSON) | — | — | — | — | Parse error: `--metadata must be valid JSON` |
| `--rename` (unsafe name) | — | — | — | — | Parse error (see §7.3) |

**[obs]** Unknown options are accepted and ignored by `add` (the parser returns
them but `parseAddOptions` discards them). Consequences:

* `skills add X --force`, `--frozen-lockfile`, `--no-frozen-lockfile`,
  `--dry-run`, `--project`, `--no-cleanup` are **silent no-ops** on `add`.
* `--project` does **not** select project scope; the scope prompt still appears
  interactively, and non-interactively the default is project.

### 1.3 Argument parsing quirks

* **[obs]** For `list`-type options (`--agent`, `--skill`, `--subagent`), the
  space form consumes **following positionals** until the next option:
  `--agent claude-code cursor` = two agents. Therefore
  `skills add --agent claude-code owner/repo` consumes `owner/repo` as an agent
  and reports `Missing required argument: source`.
* **[obs]** The `=` form takes exactly one value: `--agent=claude-code owner/repo`
  keeps `owner/repo` as the source.
* **[obs]** `-s=foo` is normalized to `-s foo`.
* **[obs]** `--agent -y` is an error: `<option> requires an agent name`
  (`-a`/`-s`/`--subagent`/`--rename`/`--metadata` similarly).
* **[obs]** `--global=1` (a value on a boolean) is an error:
  `--global does not take a value`.
* **[obs]** Values are **not** comma-split: `--skill a,b` looks for a literal
  skill named `a,b`.
* **[obs]** `--agent` is validated by exact id against the agent table **before**
  the source is parsed or fetched. `--agent claude_code` ⇒
  `Invalid agents: claude_code` + `Valid agents: <79 ids>`, exit 1. `*` bypasses
  validation.
* **[obs]** Grouped short switches (`-gy`) and repeated options work.

### 1.4 Invalid combinations and precedence

| Combination | Result |
|---|---|
| `--json` without `-y`/`--all` (and not running inside a detected agent) | **[obs]** exit 1, stderr `The --json flag requires --yes (or --all) to run non-interactively.`, stdout `[{"status":"failed","error":"…"}]` |
| `--json --list` | **[obs]** exit 1, `The --json flag cannot be combined with --list.`, one failed JSON entry |
| `--json` + well-known source | **[obs]** exit 1, `--json is not yet supported for well-known skill sources.` |
| `--json` + invalid option value (`--metadata`, `--rename`) | **[obs]** exit 1, human error on stderr, stdout `[]` (empty array, from `cli.ts`) |
| `--all --rename x` | **[obs]** fetches, then errors (`--rename installs one skill…`) with exit 1 before installing |
| `--rename` with 0 or ≥2 selected skills | **[obs]** exit 1, same error |
| `--all --list` | **[obs]** `--list` wins: listing happens before selection; exit 0 |
| `--rename --list` | **[obs]** rename is ignored (list exits before rename is applied) |
| `--skill`/`--agent` both `*` without `--all`/`-y` | **[obs]** valid; still interactive for scope/method/confirm in a TTY |
| `--subagent X --global` (Eve only) | **[obs]** accepted; each Eve pair fails with `Eve does not support global skill installation` |
| `--agent '*'` (global) | **[obs]** selects all 79 agents, including agents without a global dir; those without one are dropped with one notice (§7.4) |

---

## 2. Control flow

### 2.1 Phase order (single source)

1. **[obs]** Print logo unless running inside an agent or `--json` (`cli.ts`).
2. **[obs]** Parse options; print all parse errors (exit 1).
3. **[obs]** `runAdd`: install JSON stdout redirection if `--json`.
4. **[obs]** Missing source ⇒ error block + exit 1 (exit code 1; JSON failed entry).
5. **[obs]** Snapshot explicitly named agents (before defaults), then `--all`
   expansion, then agent-context detection (auto `-y` + auto agent).
6. **[obs]** Validate `--agent`.
7. **[obs]** Intro banner (`skills`) unless running in an agent; if in an agent,
   log `Agent detected — installing non-interactively`; if stdin is not a TTY
   (and not in an agent), print once:
   `Tip: use the --yes (-y) and --global (-g) flags to install without prompts.`
8. **[obs]** Source preparation (Notion prep, well-known discovery, parse),
   then fetch/discover (blob snapshot, git clone, or direct download).
   Canonical GitHub rename detection logs `<old> has moved to <new>`.
9. **[obs]** `--list` ⇒ print list, clean temp, exit 0.
10. **[obs]** Skill selection (flags/auto/prompt).
11. **[obs]** `--rename` application (may error).
12. **[obs]** Agent selection (+ Eve confirm / Eve subagent prompt).
13. **[obs]** Scope decision (global/project).
14. **[obs]** Drop agents without a global dir when appropriate.
15. **[obs]** Read the project lock now (fail early) when project scope.
16. **[obs]** Mode decision (copy/symlink).
17. **[obs]** Overwrite + category-conflict probes, build summary note.
18. **[obs]** Await security audit data, print advisory note.
19. **[obs]** Confirm (`Proceed with installation?`) unless `-y`.
20. **[obs]** Install all skill × target pairs sequentially.
21. **[obs]** Telemetry, hashes, lock writes, result output, failures, category
    warnings, `Done!` outro, optional find-skills prompt.
22. **[obs]** `finally`: restore stdout, remove exit listener, delete temp dir.

### 2.2 What "non-interactive" means

Non-interactive (`options.yes === true`) is set by: `-y`, `--all`, or detection
of a host AI agent. It suppresses **all** prompts: skills, agents, Eve confirm,
Eve subagents, scope, method, proceed confirmation, find-skills. **[obs]**

### 2.3 Prompt matrix

Rows are the situation, columns are the prompt that is shown. `—` = skipped.
"Skills prompt" means the fuzzy multi-select of skills; "Agent prompt" is one of
three different pickers (§2.4).

| Situation | Skills | Eve confirm | Agent picker | Eve subagents | Scope | Method | Proceed | find-skills |
|---|---|---|---|---|---|---|---|---|
| `--list` | — | — | — | — | — | — | — | — |
| `--json` (requires `-y`) | — | — | — | — | — | — | — | — |
| `-y` / in-agent, no `--skill`, >1 skills | — (all, logged) | — | — | — | — | — | — | — |
| `-y` / in-agent, 1 skill | — (auto, logged) | — | — | — | — | — | — | — |
| `-y`, Eve detected | — | — (auto **yes**) | — | — (root only) | — | — | — | — |
| `-y`, no agent detected | — | — | — (all 79) | — | — | — | — | — |
| `-y`, agents detected (non-Eve) | — | — | — (detected + universal) | — | — | — | — | — |
| explicit `--skill`/`@skill` | — | — | — | — | — | — | — | — |
| 1 skill, interactive | — (auto, logged) | see Eve row | | | | | | |
| >1 skills, interactive, no `--skill` | **skills multiselect** | | | | | | | |
| explicit `--agent` | | — | — | only if Eve targeted | if `-g` absent | if mode resolves to symlink | **yes** | if TTY, not dismissed |
| no `--agent`, Eve detected, interactive | | **confirm** (default yes) | only if Eve declined | if Eve targeted | if `-g` absent | if symlink | **yes** | yes |
| no `--agent`, 0 agents installed, interactive | | — | picker over all agents except Eve (defaults: claude-code, opencode, codex, grok) | — | if `-g` absent | if symlink | **yes** | yes |
| no `--agent`, exactly 1 agent installed | | — | — (auto) | if Eve | if `-g` absent | if symlink | **yes** | yes |
| no `--agent`, >1 installed, interactive | | — | locked universal section + selectable others | if Eve | if `-g` absent | if symlink | **yes** | yes |
| `-g` given | | | | | — | if symlink | **yes** | yes |
| `--copy` given | | | | | | — (copy) | **yes** | yes |
| project with ≤1 unique target dir | | | | | | — (copy) | **yes** | yes |
| stdin not a TTY, no `-y` | first prompt immediately cancels | | | | | | | — |

**[obs]** Scope prompt appears only when `--global` was **not** passed, `-y` is
not set, and at least one selected agent has a global directory.
**[obs]** Method prompt appears only when `--copy` was not passed, `-y` is not
set, and the resolved mode is `symlink`.
**[obs]** `Proceed with installation?` appears for every interactive run, even
when every target will be skipped for a category conflict.
**[obs]** find-skills appears only on a TTY, without `-y`, when not previously
dismissed and `find-skills` is not already installed globally for Claude Code.

### 2.4 Prompts in detail

**Skills multiselect** (`searchMultiselect`, `required: true`):
* **[obs]** Sorted by plugin name first, then display name; grouped when any
  skill has a plugin; group headings are selectable (toggle whole group);
  "Select All" row; up to 20 visible rows; searchable only when ungrouped.
* **[obs]** Skills with no plugin appear under `General` when groups exist.
* **[obs]** Required: cannot submit with zero selected.
* **[obs]** Cancelled with Ctrl-C or Esc.
* **[obs]** Selection result order is the order items were toggled, not display
  order (initial selection is empty here); this is the install order.

**Agent picker, branch A — no agent installed** (`promptForAgents`):
* **[obs]** Fuzzy multi-select over **all agents except Eve**; `required: true`.
* **[obs]** Initial selection = last saved selection (from the global lock)
  filtered to valid choices; if empty, defaults `claude-code`, `opencode`,
  `codex`, `grok` filtered to available choices.
* **[obs]** Saving happens on submit (not on cancel).

**Agent picker, branch B — >1 installed** (`selectAgentsInteractive`):
* **[obs]** Universal agents (`showInUniversalList !== false`) appear in a
  locked, always-included section titled `Universal (.agents/skills)`;
  hidden ones are counted as `…and N more`.
* **[obs]** Eve is never listed here; other agents are selectable with their
  skills dir as hint; last saved selection pre-selects only non-universal agents.
* **[obs]** For `-g`, agents without a global dir are filtered out entirely.

**Eve confirm**: message `Detected an eve project. Install <skills> for your eve
agent to use?`, `initialValue: true`. **[obs]** Answering yes sets targets to
**`['eve']` only** — other detected agents are dropped. Declining falls through
to the agent picker.

**Eve subagents** (`p.multiselect`, `initialValues: ['']`, `required: true`):
shown only when Eve is targeted, `agent/subagents/` exists and is non-empty, and
`--subagent`/`-y` were not given. Options: `Root agent` (`agent/skills`) plus one
per subagent (`agent/subagents/<name>/skills`). **[obs]** Subagent names are
discovered dynamically and sorted; names passed via `--subagent` are not
validated against them.

**Scope** (`p.select`): `Installation scope` with `Project` ("Install in current
directory (committed with your project)") and `Global` ("Install in home
directory (available across all projects)"). **[asmp]** With no `initialValue`,
the first option (`Project`) is highlighted, so Enter selects project.

**Method** (`p.select`): `Installation method` with `Symlink (Recommended)`
("Single source of truth, easy updates") and `Copy to all agents` ("Independent
copies for each agent"). **[asmp]** Symlink is the highlighted default.

**Proceed**: `p.confirm({ message: 'Proceed with installation?' })`.
**[asmp]** Clack's default initial value is `true`, so Enter proceeds; y/Enter
proceed, n/No or cancel abort.

**find-skills**: one-time confirm `Install the find-skills skill? It helps your
agent discover and suggest skills.` with the dim line
`One-time prompt - you won't be asked again if you dismiss.` On accept it
recursively runs `add vercel-labs/skills --skill find-skills -g -y --agent
<same targets minus replit>`; on decline it prints
`You can install it later with: npx skills add vercel-labs/skills@find-skills`
and stores a dismissal flag in the global lock. **[obs]**

### 2.5 Cancel / Ctrl-C / EOF

* **[obs]** All prompt types treat Ctrl-C and Esc as cancellation; EOF on stdin
  also cancels (the custom multiselect resolves a cancel symbol on `close`).
* **[obs]** Cancellation before installation prints `Installation cancelled`
  and:
  * TTY: exits **0** (deliberate cancel);
  * non-TTY: prints the `Interactive prompt required but stdin is not a TTY.
    Nothing was installed. Use --agent <name> (or --agent '*') and -y to run
    non-interactively.` diagnostic to stderr and exits **1**.
* **[obs]** Cancellation at any prompt (skills, Eve confirm, agent picker,
  subagents, scope, method, proceed) installs nothing and cleans the temp dir.
* **[obs]** Cancelling the find-skills prompt does **not** abort the install; it
  only dismisses the prompt.

### 2.6 Effect of each non-interactive switch

| Switch | Effect |
|---|---|
| `-y` | No prompts; scope = project unless `-g`; mode from `defaultInstallMode`; find-skills skipped; skills = all when `--skill` absent |
| `--all` | `-y` + all skills + all 79 agents (includes Eve root) |
| `--list` | Stops after discovery; no rename/agent/scope/method/confirm; exit 0 |
| `--skill` | Skips the skills prompt, even if it names one skill or `*` |
| `--agent` | Skips all agent prompts (including the Eve confirm) |
| `-g` | Skips the scope prompt, selects global |
| `--copy` | Skips the method prompt, selects copy |
| `--rename` | No prompt effect; requires exactly one selected skill |
| `--subagent` | Skips the subagent prompt; adds Eve to targets |
| `--json` | No prompts at all (requires `-y`) |

### 2.7 Agent-context mode

* **[obs]** Detection uses `@vercel/detect-agent`; Cursor requires
  `CURSOR_AGENT` or `CURSOR_EXTENSION_HOST_ROLE=agent-exec` (a plain
  `CURSOR_TRACE_ID` in Cursor's terminal is not enough).
* **[obs]** In an agent: `yes=true`; the logo and `skills` intro are suppressed;
  a banner `<name>` + `Agent detected — installing non-interactively` is logged.
* **[obs]** If no `--agent` was given, targets = detected agent mapped to an
  agent id (plus universal agents that read the canonical global dir); if the
  mapped id is unknown, the target list stays empty and the normal detection
  path runs.
* **[obs]** `--json` works without `-y` inside an agent, because agent mode sets
  `yes` before the JSON check.

### 2.8 JSON mode mechanics

* **[obs]** `process.stdout.write` is redirected to stderr during the run, so all
  human output (spinners, notes, summary, errors) goes to stderr.
* **[obs]** Exactly one JSON array is written to stdout: normally at the end,
  and also via a `process.once('exit')` safety net.
* **[obs]** One entry per selected skill; status is `failed` if any target
  failed, otherwise `installed`, and `skipped` for a requested name with no
  match or a skill skipped for a category conflict.
* **[obs]** Entry keys: `name`, `status`, `source`, `ref`, `hash`, `path`,
  `scope`, `agents`, `mode`, `security`, `fallback` (only when a blob fast path
  fell back), plus `reason` (skipped) or `error` (failed).
* **[obs]** `agents` lists display names, excluding skipped targets.
* **[obs]** Exit code is 1 when any result failed or any entry was skipped;
  otherwise 0.

---

## 3. Discovery and selection (only what `add` depends on)

* **[obs]** Discovery requires a `SKILL.md` with non-empty string `name` and
  `description`; otherwise the file is skipped with a warning.
* **[obs]** Priority locations: the search root, `skills/`, `skills/.curated`,
  `skills/.experimental`, `skills/.system`, every agent project skills dir, plus
  dirs declared in `skills.sh.json`; those containers are walked up to 3 levels
  deep. Plugin-manifest paths are depth 1.
* **[obs]** If a root `SKILL.md` exists, discovery stops there **unless**
  `--full-depth` is set. If an explicitly requested skill is not found at the
  default depth, discovery silently retries with full depth (both local and
  cloned sources).
* **[obs]** `--full-depth` also disables the GitHub snapshot fast path, forcing
  a git clone.
* **[obs]** A skill is treated as internal when `metadata.internal` is `true`,
  `'true'`, or `'1'`. Internal skills are hidden unless
  `INSTALL_INTERNAL_SKILLS=1|true` is set, or a specific skill is requested
  (`--skill name` or `@name`). `--skill '*'` does **not** include them.
* **[obs]** `--skill` matching: exact display-name match, case-insensitive;
  otherwise normalized match (lowercase, spaces/underscores → `-`). This makes
  `--skill "Convex Best Practices"` and `--skill convex-best-practices`
  equivalent.
* **[obs]** Duplicate names within a source: the first discovered wins; later
  ones are dropped (the programmatic API can opt into duplicates).
* **[obs]** `@skill` in the source is merged into `options.skill` (and passed to
  the snapshot fast path); if it names an unknown skill the run fails with
  `No matching skills found for: …` after fetching.
* **[obs]** `--list` prints **all discovered skills**, i.e. `--list --skill x`
  does not filter the listing (it only broadens internal-skill inclusion and can
  trigger the full-depth retry).

---

## 4. Install locations

### 4.1 Canonical vs agent directories

* **[obs]** Project canonical dir: `<cwd>/.agents/skills/<sanitized-name>`.
* **[obs]** Global canonical dir: `~/.agents/skills/<sanitized-name>`.
* **[obs]** `<sanitized-name>` = `sanitizeName`: lowercase; runs of characters
  other than `a-z0-9._` become `-`; leading/trailing `.`/`-` removed; max 255
  chars; empty ⇒ `unnamed-skill`. Sanitization is applied by the installer even
  if discovery produced an unsafe name; a name that escapes the base directory is
  refused with `Invalid skill name: potential path traversal detected`.
* **[obs]** Each agent has a **project** `skillsDir` and a **global**
  `globalSkillsDir` (undefined = no global support). Universal agents use
  `.agents/skills` in a project.
* **[obs]** `globalReadsCanonical: false` makes an agent use its own global dir
  even though it uses `.agents/skills` in projects (Antigravity, Antigravity CLI).

Notable entries in the agent table (v1.9.0, 79 ids):

| Agent id | Display | Project dir | Global dir | Notes |
|---|---|---|---|---|
| `claude-code` | Claude Code | `.claude/skills` | `$CLAUDE_CONFIG_DIR` or `~/.claude` + `/skills` | `createProjectSkillsDirByDefault: true` |
| `codex` | Codex | `.agents/skills` | `~/.agents/skills` | detected by `$CODEX_HOME` or `/etc/codex` |
| `opencode` | OpenCode | `.agents/skills` | `$XDG_CONFIG_HOME/opencode/skills` | |
| `cursor` | Cursor | `.agents/skills` | `~/.cursor/skills` | project universal, global own dir |
| `openclaw` | OpenClaw | `skills` (**repo root**) | `~/.openclaw/skills`, else `~/.clawdbot/skills`, else `~/.moltbot/skills` | root-level install risk |
| `hermes-agent` | Hermes Agent | `.hermes/skills` | `$HERMES_HOME/skills` | category dirs under it are protected |
| `antigravity` | Antigravity | `.agents/skills` | `~/.gemini/config/skills` | never reads `~/.agents/skills`; excluded from auto-universal |
| `antigravity-cli` | Antigravity CLI | `.agents/skills` | `~/.gemini/config/skills` | same |
| `eve` | Eve | `agent/skills` | **undefined** (no global) | subagents at `agent/subagents/<n>/skills`; detected by `agent/` **and** `eve` in `package.json` deps |
| `droid` | Droid | `.agents/skills` | `~/.factory/skills` | global dir kept for legacy cleanup |
| `zed` | Zed | `.agents/skills` | `~/.agents/skills` | |
| `pi`, `warp`, `fx`, `loaf`, `sarvam-code`, `dexto` | … | `.agents/skills` | `~/.agents/skills` | canonical both scopes |
| `universal` | Universal | `.agents/skills` | `$XDG_CONFIG_HOME/agents/skills` but reads canonical | pseudo-agent, selected by `--agent '*'` |

**[obs]** Universal (project) set = agents with `skillsDir === '.agents/skills'`.
**[obs]** Auto-included universal set (used by `-y`/detected) excludes agents
hidden from the universal list (`replit`, `universal`) and agents that do not
read the canonical global dir (`antigravity`, `antigravity-cli`).

### 4.2 Choosing copy vs symlink

`defaultInstallMode` **[obs]**:

```
uniqueDirs = { agent.skillsDir | "eve:subagent:<name>" for each target }
allEve     = every target is Eve
mode       = (uniqueDirs.size <= 1 || allEve) ? copy : symlink
```

Consequences:

* **[obs]** A **single target** always gets a plain copy, even a non-universal
  one. `add … -a claude-code` writes `.claude/skills/<name>` and **does not**
  create `.agents/skills/<name>`.
* **[obs]** Several universal agents (e.g. codex + cursor + cline) share one
  directory ⇒ copy (deduplicated path output).
* **[obs]** Eve always gets copies (no links), including into subagents.
* **[obs]** Two or more distinct dirs (`claude-code` + `kiro-cli`, or
  `claude-code` + `codex`) ⇒ symlink by default.
* **[obs]** The computation uses **project** `skillsDir` values even for global
  installs. `-g -a claude-code` ⇒ copy into `~/.claude/skills`.
* **[obs]** `--copy` forces copy. `link` mode exists in the installer type but
  `add` never selects it (only sync uses it).

### 4.3 Symlink mode

* **[obs]** The skill is first copied to the canonical dir
  (`.agents/skills/<name>`, or Eve's own dir), then each agent dir gets a
  relative symlink to it.
* **[obs]** The link target is computed as `relative(realpath(dirname(link)), canonical)`
  — relative links survive moving the whole tree; if the agent skills directory
  is itself a symlink (e.g. dotfiles), the link is computed from its real
  location.
* **[obs]** Before creating a link, realpaths of target and link path are
  compared (both directly and with parent symlinks resolved). If they are the
  same physical location, the installer returns success **without creating
  anything** — this is how `.claude/skills -> .agents/skills` avoids self-loops.
* **[obs]** An existing path at the link location is removed first: a symlink is
  unlinked, anything else is removed recursively. The link is then created.
* **[obs]** Windows: symlink type is `junction` and the target is **absolute**;
  POSIX: default symlink type and a relative target.
* **[obs]** If link creation still fails, the installer falls back to copying
  into the agent dir and reports `symlinkFailed: true` (still a success).
* **[obs]** A symlink at the destination is never followed when deleting, so the
  canonical directory is not destroyed.

### 4.4 Copy mode

* **[obs]** Copies directly into each agent dir; never uses the canonical dir.
  If a canonical copy already exists from an earlier install, it is left alone.
* **[obs]** The destination is removed recursively and recreated before copying
  (`cleanAndCreateDirectory`); renamed or deleted files from a previous install
  therefore disappear.
* **[obs]** Excluded: file `metadata.json`; directories `.git`, `__pycache__`,
  `__pypackages__` (also `node_modules`, `dist`, `build` are skipped during
  *discovery*, not during copying). Dotfiles such as `.prettierrc` are kept.
* **[obs]** Symlinks inside the skill are dereferenced (content copied). Links
  that are broken, cyclic, or resolve outside the skill root are skipped with a
  warning; directory links are walked with the same per-entry check.
* **[obs]** File mode bits are preserved (`chmod` to `source mode & 0o777`);
  directories are created with the process umask; blobs/well-known files are
  written with default file permissions.
* **[obs]** A blob/well-known skill writes only the files in its snapshot,
  skipping excluded paths and any path that would escape the target dir.

### 4.5 Skips and special results

| Condition | Result | Reported as |
|---|---|---|
| Global install for an agent with no global dir | failure `"<Agent> does not support global skill installation"` | `Failed to install N` line |
| Skill source path overlaps the agent destination | success, `skipped: true`, no reason; nothing written | counts as installed |
| Symlink mode, source overlaps the canonical destination | success, `skipped: true`; canonical path reported | counts as installed |
| Global install, agent reads canonical globally | canonical copy only, no agent-dir link | `universal:` |
| Project symlink, non-universal agent, agent's project root missing, agent not explicitly selected, agent lacks `createProjectSkillsDirByDefault` | success, `skipped`, reason `missing-agent-project-directory` | `skipped: X (project directory not found)`; omitted from JSON `agents` |
| Destination is an "agent category directory" | pair skipped (add) or failure (installer-level) | `conflicts with category: …` |

**[obs]** "Explicitly selected" means named with `--agent` or chosen in an
interactive picker. `--agent '*'` and `--all` do **not** count as naming agents
because the explicit set is captured before wildcard expansion.

---

## 5. Conflict, overwrite, idempotency, relocation

### 5.1 Existing skill

* **[obs]** Detection: `isSkillInstalled` checks for the existence of
  `<agentBase>/<sanitized-name>` for each planned target (agent-specific base,
  not canonical). It does not parse content.
* **[obs]** There is **no overwrite prompt** and no "already installed" short
  circuit for `add`. The summary note gains a line
  `overwrites: <agent, agent>`; the confirmation is only the generic
  `Proceed with installation?`.
* **[obs]** Installation replaces the destination wholesale (delete then write),
  so local edits in an installed skill (non-symlink) are lost.
* **[obs]** If the agent path is a symlink, only the link is replaced/removed.
* **[obs]** A changed `SKILL.md` `name` between versions changes the destination
  directory only if the source folder name changed; otherwise the same directory
  is replaced.

### 5.2 Category directories (protection)

* **[obs]** A path is a category directory when it is a real directory, has no
  `SKILL.md`, and at least one immediate child directory contains `SKILL.md`
  (e.g. `~/.hermes/skills/research/arxiv/SKILL.md`).
* **[obs]** `add` probes the planned install path per target before installing.
  A conflicting target is skipped for that skill, the rest proceed, and the
  summary shows
  `conflicts with category: <Agent> (<dir>/ contains <a, b, …>)`.
* **[obs]** After installation, one warning per conflicting path is printed to
  stderr: `Refusing to replace <path>: it is a category directory with nested
  skills (<names>). Installing would delete them.` followed by
  `Move or rename the category directory, then install again.`
* **[obs]** The installer itself refuses such a path at every write step, before
  writing anything for that pair, so a canonical path that is a category
  directory yields a pair failure rather than data loss.
* **[obs]** Category-conflict skips do not fail the human-mode run (exit 0) but
  do make `--json` exit 1 with a `skipped` entry
  (`reason: Conflicts with a category directory`).

### 5.3 Idempotency

* **[obs]** Re-running the same `add` is idempotent in content: the destination
  is recreated from the source, links are recreated, and lock entries are
  rewritten with a new `updatedAt` (global) or fresh `computedHash` (project).
* **[obs]** Canonical directories are not duplicated; the second run reports
  `overwrites:` and still succeeds.
* **[obs]** Universal/global agents that already read the canonical dir get no
  second copy.

### 5.4 Relocation

* **[obs]** `add` has **no** relocation logic. The relocation heuristics
  (`skill-relocation.ts`: exact path wins, otherwise single normalized-name
  match, ambiguous fails closed) are used by `update`, not by `add`.
* **[obs]** The closest `add` behavior is replacement: a pre-existing real
  directory at the agent path is deleted and replaced by a symlink to the
  canonical copy, effectively relocating the content to `.agents/skills`.
* **[obs]** A GitHub repo that was renamed upstream is detected from the repo
  lookup; the new `owner/repo` is used for the lock/telemetry and the move is
  logged as `old has moved to new` (a case-only change is not logged).
* **[obs]** A symlink that points somewhere else is unlinked and recreated, so a
  stale/old location does not survive.

---

## 6. Execution model

### 6.1 Loops and ordering

* **[obs]** `installToTargets`: **skills outer, targets inner**, each pair awaited
  sequentially — no parallelism between pairs.
* **[obs]** Pair order = selection order for skills × target order. Target order
  is `--agent` order (or table order for `*`), with Eve expanded into its
  root/subagent targets in the order returned by the prompt or `--subagent`.
* **[obs]** Copying one skill directory copies its entries in parallel
  (`Promise.all` per directory level); directory recursion is sequential per
  branch.
* **[obs]** Overwrite probes run in parallel (`Promise.all` over all pairs).
* **[obs]** Multiple **sources** are fetched concurrently with limit 4
  (`SKILLS_CLONE_CONCURRENCY` overrides); this applies to pack-pointer installs,
  restore, and sync, not to a single-source `add`.
* **[obs]** Blob fast path: GitHub sources under allowed owners/repos use a
  snapshot download (no clone); any eligible failure falls back to a clone with
  a message; `--full-depth` skips it.

### 6.2 Partial failures

* **[obs]** Failures are per skill × target and do not abort the remaining pairs.
* **[obs]** A canonical copy can succeed while an agent link fails; the
  fallback copy then succeeds. A hard failure (e.g. canonical write error)
  yields `success: false` for that pair.
* **[obs]** Human mode prints `Failed to install <n>` followed by
  `✗ <skill> → <agent>: <error>` lines and **still exits 0** (unless a lock
  write failed, which sets exit code 1).
* **[obs]** JSON mode turns any failed pair into a failed skill entry and exits 1.
* **[obs]** Lock entries are written only for skills with at least one
  successful target.

### 6.3 Cleanup

* **[obs]** Clone/download temp dirs are removed in `finally` on the normal
  path, explicitly before interactive cancels, and by `resolveSkills` when
  fetching/discovery throws.
* **[obs]** Fatal exits (`process.exit`) on error paths bypass `finally`; because
  the catch block ends in `process.exit`, a thrown error **after** a temp dir
  exists (e.g. an unreadable project lock detected just before install) can
  leave the temp dir behind. **[obs]** structurally; practical impact limited to
  those paths.
* **[obs]** A failed download of one skill's supporting files aborts before any
  install or lock write (well-known flow).

---

## 7. Post-install side effects

### 7.1 Project lock (`skills-lock.json`) — note only

* **[obs]** Written at `<cwd>/skills-lock.json`, schema version 2, keys sorted
  alphabetically, no timestamps, 2-space JSON + trailing newline, atomic
  temp-file rename, guarded by a sibling `.lock` mutex (10 s acquire timeout,
  5 s stale threshold).
* **[obs]** Entry fields: `source`, optional `sourceUrl`, `ref`, optional
  `commit`, `sourceType`, optional `skillPath`, optional `skillName` (renamed
  skills), optional `via`, `computedHash`, optional `subagents`, optional
  `wellKnownDigest`, optional `installMode: "copy"`.
* **[obs]** `installMode: copy` is recorded only when copy was chosen and was
  **not** the automatic default (explicit `--copy`, or copy chosen while the
  default would have been symlink).
* **[obs]** Written per successful skill; a write failure logs
  `Installed <name> but could not record it in <path>: <reason>` and sets
  exit code 1 without undoing the install.
* **[obs]** Project lock is read before installing (when project scope) so an
  invalid/merge-conflicted lock fails before disk writes.

### 7.2 Global lock (`.skill-lock.json`) — note only

* **[obs]** Path: `$XDG_STATE_HOME/skills/.skill-lock.json` if set, else
  `~/.agents/.skill-lock.json`; schema version 3; entries carry
  `installedAt`/`updatedAt` ISO timestamps.
* **[obs]** Written only for global installs with at least one success, and only
  when the source can be re-fetched later (normalized owner/repo, local path, or
  a recorded download). Same failure message/exit-code behavior as above.
* **[obs]** Also stores `lastSelectedAgents` and dismissed-prompt flags.

### 7.3 `--rename`: restrictions, mechanics

* **[obs]** Parse-time validation: the value must already equal `sanitizeName(value)`
  — lowercase letters, digits, `.`, `_`, `-`, not starting/ending with `.`/`-`,
  non-empty, ≤255 chars. Error:
  `--rename must be a skill name of lowercase letters, digits, ".", "_" and "-" (e.g. <sanitized>)`.
* **[obs]** Exactly one skill must be selected **after** selection/`@skill`
  merging and **before** agent selection. Zero or many ⇒
  `--rename installs one skill under a new name, but <n> skills were selected.`
  with fix `Pick one skill with --skill <name> or <source>@<skill>.`, exit 1,
  nothing installed, no lock file created.
* **[obs]** Effects: folder name, lock key, and the installed `SKILL.md`
  `name` field all become the new name. The original name is kept as
  `renamedFrom` in memory and `skillName` in both locks; `update`/restore use it.
* **[obs]** Renaming to the source name is a no-op (no `renamedFrom`, no
  `skillName`).
* **[obs]** Frontmatter rewrite (`setFrontmatterName`): replaces the existing
  `name:` line including its YAML continuation lines, or inserts `name:` first;
  every other byte is preserved. A file without frontmatter is left unchanged
  (so its hash is the source hash).
* **[obs]** The rewrite is applied to the canonical copy and to each copied
  agent dir; symlinks are skipped (their target is rewritten on its own). It is
  skipped when the install was skipped because source == destination.
* **[obs]** The source repository/temp clone is never modified.
* **[obs]** `--rename` is ignored by `--list`; it is rejected for wildcard
  selections (`--all`, `--skill '*'`).
* **[obs]** In the well-known flow, rename is applied after downloading the
  selected skill, with the same one-skill restriction and exit 1.
* **[obs]** Aliases never leave the machine: audit and telemetry use the source
  name (`sourceSkillName`), and the alias is not hashed as upstream content.
* **[obs]** Lock hash is computed over the **installed** (rewritten) files, so
  `check`/`list` verify as `tracked`.

### 7.4 Per-agent special cases

| Agent | Special behavior |
|---|---|
| **Eve** (`eve`) | No global install; detected only when `agent/` exists **and** `package.json` depends on `eve`. Targets `agent/skills`; subagents target `agent/subagents/<sanitized>/skills`. In symlink mode its "canonical" dir is its own agent dir, and `defaultInstallMode` gives it copy anyway. Blob skills with no `SKILL.md` are written as a flat `agent/skills/<name>.md` (no directory). Frontmatter is reduced to `name`, `description`, `license`, `compatibility`, `version`, `metadata`; other keys are dropped. Messages use the literal label `eve agent`, not `Eve`. |
| **Claude Code** | `createProjectSkillsDirByDefault: true`: a project install creates `.claude/skills` even when `.claude` is absent, for auto-selected agents too. Global dir honors `CLAUDE_CONFIG_DIR`. |
| **Codex** | Universal: project `.agents/skills`, global `~/.agents/skills` (reads canonical, no own link). Detected via `$CODEX_HOME` or `/etc/codex`; legacy `.codex/skills` is only searched during discovery. |
| **OpenClaw** | Project dir is the repo-root `skills/`, so `add ./skills --all` can hit the source-equals-destination skip. Global dir falls back `~/.openclaw` → `~/.clawdbot` → `~/.moltbot`. |
| **Antigravity / Antigravity CLI** | Universal in projects but `globalReadsCanonical: false`: a global install writes/links `~/.gemini/config/skills` and never `~/.agents/skills`. Not auto-added by `-y` when undetected. |
| **Cursor** | Universal in projects; global `~/.cursor/skills` (so symlink/copy, not canonical). |
| **Hermes Agent** | `~/.hermes/skills` may be a category tree; protected by the category checks. |
| **Droid** | Universal project dir; global remains `~/.factory/skills` for cleanup of older installs. |
| **Zed / Pi / Warp / Fx / Loaf / Sarvam / Dexto** | Read canonical `.agents/skills` in both scopes; global installs touch only the canonical dir. |
| **Replit** | Hidden from the universal list; excluded from the find-skills install targets. |
| **`universal`** | Pseudo-agent used by tests/`--agent '*'`; maps to the canonical directory. |

**[obs]** Agents with no global dir (`eve`, `promptscript`) are dropped from a
global install with one message `Skipping X: no global skills directory` when
they were auto-selected, but keep the per-pair failure when named with
`--agent`. If **all** candidates lack a global dir, none are dropped (so the
error is visible).

### 7.5 Warnings (non-fatal, stderr)

| Trigger | Text |
|---|---|
| Auto-selected agents lack a global dir | `Skipping <names>: no global skills directory` |
| Destination is a category dir (after install) | `Refusing to replace <path>: it is a category directory with nested skills (<names>). Installing would delete them.` + fix line |
| Symlink escape attempt | `Skipping symlink that points outside the skill: <path>` |
| Broken/cyclic symlink | `Skipping broken symlink: <path>` / cycle warning |
| Symlink creation failed | `Symlinks failed for: <agents>` + `  Files were copied instead. On Windows, enable Developer Mode for symlink support.` |
| Download URL not lockable | one warning per URL: download not recorded because it has a query/userinfo |
| Lock write failure | `Installed <skill> but could not record it in <path>: <reason>` |

### 7.6 Telemetry / security audit gating

* **[obs]** An install event is sent only for public GitHub sources (privacy
  lookup must return `false`); unknown privacy ⇒ skip. Local paths and direct
  downloads are not tracked here. `--metadata` is attached verbatim.
* **[obs]** Audit (`skills.sh`) is queried only after GitHub positively confirms
  a public repo; private/unknown repos never send skill names. The audit note is
  advisory (failures ignored) and titled `Security Risk Assessments`.
* **[obs]** Renamed aliases are replaced by source names in both.

---

## 8. User-facing messages worth matching

```
ERROR  Missing required argument: source
  Usage:
    npx skills add <source> [options]

Source: <url|localPath> [@ <ref>] [(subpath)] [@<skill>]
Found <n> skills
Selected <n> skills: <a, b>
Installing all <n> skills
Skill: <name>
Installing to: <Agent>
Installing to all <n> agents
Installation Summary
Security Risk Assessments
Proceed with installation?
Installation method
Installation scope
Installation complete
Installed <n> skills
✓ <canonical-short-path>
✓ <skill> (copied)
→ <short-path>
universal: <agents>
symlink → <agents>
copy → <agents>
symlinked: <agents>
copied: <agents>
skipped: <agents> (project directory not found)
overwrites: <agents>
Failed to install <n>
✗ <skill> → <agent>: <error>
Done!  Review skills before use; they run with full agent permissions.
Installation failed
Installation cancelled
```

**[obs]** Paths in output are shortened: `~` for the home prefix, `.` for the cwd
prefix (home wins when cwd is under home).

Additional exact diagnostics:

* `No valid skills found. Skills require a SKILL.md with name and description.`
* `No matching skills found for: <names>` (+ `Available skills:` list)
* `Local path does not exist: <path>`
* `Invalid agents: <ids>` (+ `Valid agents: …`)
* `--metadata must be valid JSON`
* `The --json flag requires --yes (or --all) to run non-interactively.`
* `The --json flag cannot be combined with --list.`
* `Interactive prompt required but stdin is not a TTY. Nothing was installed. Use --agent <name> (or --agent '*') and -y to run non-interactively.`

---

## 9. Exit codes

| Code | When |
|---|---|
| 0 | Successful install; `--list`; human-mode run with some pair failures; interactive cancel on a TTY; `--skill`/`--agent` nothing to do |
| 1 | Missing source; parse errors (`--metadata`, `--rename`, missing option value); invalid agent; missing local path; no valid skills; no matching skills; multi-skill `--rename`; JSON+list; JSON without `-y`; JSON with any failed/skipped entry; non-TTY prompt cancel; well-known integrity/scope/download errors; unreadable/invalid lock; lock write failure; any thrown install-source error |
| 1 (via `process.exit(1)`) | Fatal paths inside `runAdd` (they bypass `finally`) |

**[obs]** There is no dedicated code 2; Node's default uncaught-error exit is not
used because errors are caught.

---

## 10. Related entry points on the same engine

### 10.1 Restore (`install`/`i` without source, `experimental_install`)

**[obs]** `runInstallFromLock` (`src/install.ts`):

* Reads the project lock (migrating v1→v2 unless frozen/dry-run).
* Targets: `--agent` if given (`*` = all), else the saved `lastSelectedAgents`
  filtered to valid ids, else detected agents; universal agents always added.
* Skills already on disk and matching their hash are skipped
  (`<n> skills already installed (use --force to reinstall)`); modified ones are
  restored over; `--force` reinstalls everything.
* Entries grouped by source + copy/symlink + download + pinned ref; sources are
  fetched concurrently (limit 4); each group installs sequentially.
* Git sources are fetched at the locked `commit` (not the branch tip) while the
  lock keeps tracking `ref`.
* Failure of one source/skill fails only its own entries; the summary ends with
  `Failed to restore <n> skills from skills-lock.json: …` and exit code 1.
* `node_modules` entries are handed to `sync` (with `yes: true`).
* Project installs use `projectPlacement`: symlink by default, unless the entry
  says `installMode: copy`; `createMissingAgentRoot` is always true, so restore
  creates missing agent roots.

### 10.2 Programmatic API (`src/api/install.ts`)

**[obs]** `installSkills(source, options)` is the non-interactive analogue of
`add`, with these differences:

* No prompts and no logo; failures of individual skill/agent pairs are returned
  in `failed` instead of being printed.
* Throws instead of exiting: no matching skill, ambiguous selector, unknown
  agent, no usable target agents, duplicate names (`duplicates: 'error'`),
  symlinks (`symlinks: 'reject'`), or lock write failure.
* Options default to `scope: 'project'`, `mode: 'auto'` (same
  `defaultInstallMode`), `lockfile: true`, `fullDepth: false`,
  `includeInternal: false`, `duplicates: 'first'`, `symlinks: 'follow'`,
  `telemetry: true`.
* Targets without an explicit `agents.default` use
  `nonInteractiveTargetAgents` (host agent, else Eve alone, else all agents when
  none detected, else detected + universal).
* `rename` is a **map** of source name → new name, validated for safe names,
  single match per key, and distinct destination names; errors are thrown
  before writing.
* `mode: 'copy'` is recorded as `installMode: 'copy'`; a `symlinkFailed` result
  is reported as `mode: 'copy'` in the API result.
* Lock recording is atomic per call; telemetry is flushed with a 3 s budget.

---

## 11. Surprising or undocumented behavior (checklist for a port)

1. **[obs]** `skills add a b` silently ignores `b` (no multiple-source error).
2. **[obs]** Unknown flags are ignored, so `--dry-run` and `--force` on `add`
   do nothing (the latter is meaningful only for `install`/restore).
3. **[obs]** `--agent`/`--skill` greedily eat following positionals; the source
   must come first or use `--agent=value`.
4. **[obs]** `--list` ignores `--skill` for filtering and exits 0 even when the
   named skill does not exist.
5. **[obs]** Human-mode `add` exits **0** when some skills fail to install.
6. **[obs]** A single target means **copy**, so `-a claude-code` never writes
   `.agents/skills`.
7. **[obs]** `-y` in an Eve project installs to Eve **only**, dropping other
   detected agents.
8. **[obs]** `-y` with no detected agents targets all 79 agents; most produce
   `missing-agent-project-directory` skips unless they are universal,
   Claude Code, or explicitly named.
9. **[obs]** `--agent '*'` does not create missing agent roots (explicit set is
   empty), whereas naming each agent does.
10. **[obs]** Copy-mode summaries print the canonical `.agents/skills/<name>`
    path even though copy mode never writes it.
11. **[obs]** Reinstall is `rm -rf` + copy: user edits and stale files are lost,
    with no confirmation beyond `Proceed with installation?`.
12. **[obs]** `--rename` requires the value to be **already sanitized**; it is
    not sanitized for you (`Bad Name` fails, suggesting `bad-name`).
13. **[obs]** Eve installs **drop unknown frontmatter keys** (`allowed-tools`,
    custom keys) and keep only a fixed allow-list.
14. **[obs]** Symlinks are relative on POSIX (computed through realpaths) but
    absolute junctions on Windows.
15. **[obs]** If the agent skills dir and canonical dir are the same physical
    path, the installer creates nothing and reports success.
16. **[obs]** Installing a skill whose name collides with an existing category
    directory skips that agent instead of overwriting (warning, exit 0).
17. **[obs]** `--json` requires non-interactive mode; inside a detected agent it
    silently becomes non-interactive.
18. **[obs]** Fatal error paths end in `process.exit`, so `finally` (temp-dir
    cleanup, stdout restore) does not run.
19. **[obs]** `--metadata` accepts any JSON value (not just objects) and only the
    last occurrence is used.
20. **[obs]** `--skill a,b` is a single literal name; no comma splitting.
21. **[obs]** A skill installed from a signed download URL is not recorded in the
    lock (query string/userinfo), so it cannot be updated later.
22. **[obs]** `--all --rename x` fetches the whole source first, then fails.
23. **[obs]** Global + Eve (`-g`, `--subagent`) is accepted, then every Eve pair
    fails with "does not support global skill installation".
24. **[obs]** The find-skills prompt can trigger a nested `add` run that writes
    the **global** lock, even for a project-scoped install.

---

## 12. Open questions

1. **[asmp→open]** Does the scope `p.select` highlight `Project` on Enter, and
   does `Proceed with installation?` default to yes in the pinned
   `@clack/prompts` version? Tests never drive the real prompts, so a Go port
   must choose explicit defaults (recommended: Project, yes).
2. Is exit 0 on human-mode partial install failure intentional or an oversight?
   There is no test asserting it; only the JSON path forces exit 1.
3. Is `--list --skill x` printing every skill intentional? `docs/commands.md`
   says "List available skills", and the test only asserts the named skill is
   present.
4. Is the temp-dir leak on fatal exits known/accepted? `resolveSkills` cleans up
   on fetch failure, but later thrown errors bypass `finally`.
5. Which prompt is shown when the target set contains Eve **and** other agents
   detected **and** Eve was named explicitly (e.g. `--agent eve claude-code`)?
   The Eve confirm is only in the auto-detection branch, so it is skipped; the
   code implies Eve is installed silently — untested.
6. What exactly does `--json` do with Notion sources? `--json` is rejected for
   well-known sources but the Notion path is not covered by tests; the code
   suggests it may run.
7. Should `--copy` also populate the canonical dir? Today it does not, so agents
   that read `.agents/skills` rely on their own target entry; with `--agent
   claude-code --copy` no canonical copy exists for other universal agents.
8. `--rename` on an Eve flat (`.md`) skill: `setInstalledSkillName` supports a
   file path, but no test covers blob→flat Eve + rename.
9. Are renamed skills' `skillPath` values (source-relative) stable when the
   rename also changes the folder name? Tests assert `skillPath` stays the
   source path; a port must keep that.
10. Does `--subagent` with a nonexistent subagent name intentionally create a new
    `agent/subagents/<name>/skills` tree, or should it be validated? Code does
    not validate.
11. Whether `--global` with a mixed target list should silently drop Eve (it only
    does when auto-selected), or fail the whole run.

---

## 13. Evidence

| Topic | Location |
|---|---|
| Add flag spec / defaults | `src/add.ts:3650-3706` |
| Arg parsing (greedy lists, `=`, errors) | `src/cli-args.ts:35-104` |
| Dispatch, aliases, logo, help | `src/cli.ts:321-443` |
| `--all`, agent-context, JSON gating, agent validation | `src/add.ts:2458-2604` |
| Source/parse/blob/clone pipeline | `src/add.ts:2606-2776`, `1641-1770` |
| Skill selection branches | `src/add.ts:2778-2867` |
| Agent selection branches, Eve confirm/subagents, scope | `src/add.ts:2882-3100` |
| Mode default | `src/add.ts:484-490` |
| Overwrite probe, summary, security note, confirm | `src/add.ts:3100-3233` |
| Install loop, results, lock writes, JSON output | `src/add.ts:3235-3423` |
| Human result output / failures / outro | `src/add.ts:3425-3537` |
| Cancel semantics / exit codes | `src/add.ts:543-558` |
| Well-known flow | `src/add.ts:967-1522` |
| `--rename` validation/application | `src/add.ts:1786-1826`, `3686-3691` |
| Lock record helpers | `src/add.ts:1961-2011`, `2239-2374` |
| Installer: paths, canonical/agent dirs | `src/installer.ts:116-167` |
| Symlink creation (relative, junction, realpath) | `src/installer.ts:207-301` |
| Install decision tree / skips | `src/installer.ts:303-488` |
| Frontmatter rewrite / Eve stripping | `src/installer.ts:490-538`, `src/frontmatter.ts` |
| Copy semantics / exclusions / modes / permissions | `src/installer.ts:562-625` |
| Category-dir detection | `src/installer.ts:169-205`, `643-698` |
| Eve flat skills | `src/installer.ts:627-641`, `1020-1067` |
| Agent table / detection / universal rules | `src/agents.ts:85-954` |
| Project lock format/path | `src/local-lock.ts:23-104`, `338-420` |
| Global lock format/path | `src/skill-lock.ts:6-127` |
| Concurrency limit | `src/concurrency.ts:1-25` |
| Restore behavior | `src/install.ts:22-318` |
| API install mapping | `src/api/install.ts:48-362`, `src/api/config.ts` |
| Tests: flags/JSON/internal/exit | `src/add.test.ts` |
| Tests: agent prompt defaults/save | `src/add-prompt.test.ts` |
| Tests: rename lifecycle | `tests/rename.test.ts`, `e2e/rename.e2e.ts` |
| Tests: symlink/copy/category/global-skip | `tests/installer-symlink.test.ts`, `tests/installer-copy.test.ts`, `tests/installer-category-protection.test.ts`, `tests/global-agent-skip.test.ts` |
| Tests: sync/lock failure | `tests/add-lock-write-failure.test.ts` |
| Docs: user-facing option table | `docs/commands.md` (`skills add`) |
