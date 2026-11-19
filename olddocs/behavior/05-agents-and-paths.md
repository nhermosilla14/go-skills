# 05 — Agents, paths, detection and filesystem operations

Behavioral documentation of the supported-agent registry in `vercel-labs/skills` v1.9.0,
intended as the specification for a Go reimplementation.

**Source of truth:** `src/agents.ts` (the registry), `src/detect-agent.ts` (host-agent
detection), `src/installer.ts` (path resolution + filesystem effects), `src/add.ts`
(target selection, wildcard, install mode), `src/types.ts` (`AgentConfig` shape),
`src/constants.ts`, `src/skills.ts` (discovery), plus `docs/agents.md`,
`scripts/sync-agents.ts` (the doc generator) and the agent-specific tests.

**Legend:** **(observed)** = read directly from code/tests in the clone.
**(assumed)** = inferred from an external dependency or from documentation that the code
does not spell out. Nothing here is a guess presented as fact.

---

## 1. Overview

The registry is a plain, insertion-ordered map from a canonical string id (an `AgentType`)
to a config record. There is no plugin system, no YAML/JSON agent manifest, and no
runtime registration: adding an agent means adding a branch to a TypeScript union type
and an entry to one object literal (`src/types.ts:1-80`, `src/agents.ts:85-834`)
**(observed)**.

Each record carries, at most:

| Field | Meaning | Default when absent |
|---|---|---|
| `name` | canonical id (same as the map key) | required |
| `displayName` | human label, unique case-insensitively | required |
| `skillsDir` | project-relative skills root, **always POSIX-style with `/`** | required |
| `globalSkillsDir` | absolute global skills root, or `undefined` for project-only | required (may be `undefined`) |
| `detectInstalled(cwd?)` | async predicate, may look at `cwd` | required |
| `showInUniversalList` | appear in the universal-agent set | `true` |
| `showInUniversalPrompt` | appear in the interactive "locked" universal section | `true` |
| `globalReadsCanonical` | a global install is satisfied by `~/.agents/skills` | `true` |
| `createProjectSkillsDirByDefault` | project installs may create a missing skills root | `false` |

Source: `src/types.ts:98-119` **(observed)**.

There are **79 ids** in v1.9.0 **(observed; counted from `src/agents.ts`)**.

There is **no alias field**. The only aliasing in the codebase lives in the
host-agent mapping (`cowork → claude-code`, `cursor-cli → cursor`,
`augment-cli → augment`, `devin → universal`), not in the registry
(`src/detect-agent.ts:41-54`) **(observed)**. A renamed id is *not* kept for
compatibility: `kimi-cli` was replaced by `kimi-code-cli` and the test asserts the old
id is absent (`tests/xdg-config-paths.test.ts:50-59`) **(observed)**.

There is **no per-agent "force copy" flag** anywhere. Copy is a *global install-mode*
decision, not an agent property (§9) **(observed; grep for `forceCopy`/`force-copy`
returns nothing)**.

---

## 2. The registry (paths)

`~` is `os.homedir()` (Section 5). Paths are shown as the code composes them; an
environment override in the global column is spelled out where it exists.

| id | display | project dir | global dir | project-only | universal | detection (§7) |
|---|---|---|---|---|---|---|
| `aider-desk` | AiderDesk | `.aider-desk/skills` | `~/.aider-desk/skills` | no | no | `~/.aider-desk` |
| `amp` | Amp | `.agents/skills` | `$XDG_CONFIG_HOME/agents/skills` | no | yes | `$XDG_CONFIG_HOME/amp` |
| `antigravity` | Antigravity | `.agents/skills` | `~/.gemini/config/skills` | no | yes* | `~/.gemini/antigravity` |
| `antigravity-cli` | Antigravity CLI | `.agents/skills` | `~/.gemini/config/skills` | no | yes* | `~/.gemini/antigravity-cli` |
| `astrbot` | AstrBot | `data/skills` | `~/.astrbot/data/skills` | no | no | `cwd/data/skills` **or** `~/.astrbot` |
| `autohand-code` | Autohand Code CLI | `.autohand/skills` | `$AUTOHAND_HOME/skills` | no | no | `$AUTOHAND_HOME` |
| `augment` | Augment | `.augment/skills` | `~/.augment/skills` | no | no | `~/.augment` |
| `bob` | IBM Bob | `.bob/skills` | `~/.bob/skills` | no | no | `~/.bob` |
| `claude-code` | Claude Code | `.claude/skills` | `$CLAUDE_CONFIG_DIR/skills` | no | no | `$CLAUDE_CONFIG_DIR` |
| `openclaw` | OpenClaw | `skills` (repo root) | `~/.openclaw/skills` (see §8) | no | no | `~/.openclaw` **or** `~/.clawdbot` **or** `~/.moltbot` |
| `cline` | Cline | `.agents/skills` | `~/.agents/skills` | no | yes | `~/.cline` |
| `codearts-agent` | CodeArts Agent | `.codeartsdoer/skills` | `~/.codeartsdoer/skills` | no | no | `~/.codeartsdoer` |
| `codebuddy` | CodeBuddy | `.codebuddy/skills` | `~/.codebuddy/skills` | no | no | `cwd/.codebuddy` **or** `~/.codebuddy` |
| `codemaker` | Codemaker | `.codemaker/skills` | `~/.codemaker/skills` | no | no | `~/.codemaker` |
| `codestudio` | Code Studio | `.codestudio/skills` | `~/.codestudio/skills` | no | no | `~/.codestudio` |
| `codex` | Codex | `.agents/skills` | `~/.agents/skills` | no | yes | `$CODEX_HOME` **or** `/etc/codex` |
| `command-code` | Command Code | `.commandcode/skills` | `~/.commandcode/skills` | no | no | `~/.commandcode` |
| `continue` | Continue | `.continue/skills` | `~/.continue/skills` | no | no | `cwd/.continue` **or** `~/.continue` |
| `cortex` | Cortex Code | `.cortex/skills` | `~/.snowflake/cortex/skills` | no | no | `~/.snowflake/cortex` |
| `crush` | Crush | `.crush/skills` | `~/.config/crush/skills` (hard-coded) | no | no | `~/.config/crush` |
| `cursor` | Cursor | `.agents/skills` | `~/.cursor/skills` | no | yes | `~/.cursor` |
| `deepagents` | Deep Agents | `.agents/skills` | `~/.deepagents/agent/skills` | no | yes* | `~/.deepagents` |
| `devin` | Devin for Terminal | `.devin/skills` | `$XDG_CONFIG_HOME/devin/skills` | no | no | `$XDG_CONFIG_HOME/devin` |
| `dexto` | Dexto | `.agents/skills` | `~/.agents/skills` | no | yes* | `~/.dexto` |
| `droid` | Droid | `.agents/skills` | `~/.factory/skills` | no | yes | `~/.factory` |
| `eve` | Eve | `agent/skills` (+ subagents) | — **none** | **yes** | no | `cwd/agent` **and** `package.json` dep `eve` |
| `firebender` | Firebender | `.agents/skills` | `~/.firebender/skills` | no | yes* | `~/.firebender` |
| `forgecode` | ForgeCode | `.forge/skills` | `~/.forge/skills` | no | no | `~/.forge` |
| `fx` | fx | `.agents/skills` | `~/.agents/skills` | no | yes | `~/.fx` |
| `gemini-cli` | Gemini CLI | `.agents/skills` | `~/.gemini/skills` | no | yes | `~/.gemini` |
| `github-copilot` | GitHub Copilot | `.agents/skills` | `~/.copilot/skills` | no | yes | `~/.copilot` |
| `goose` | Goose | `.goose/skills` | `$XDG_CONFIG_HOME/goose/skills` | no | no | `$XDG_CONFIG_HOME/goose` |
| `grok` | Grok Build | `.grok/skills` | `$GROK_HOME/skills` | no | no | `$GROK_HOME` |
| `hermes-agent` | Hermes Agent | `.hermes/skills` | `$HERMES_HOME/skills` | no | no | `$HERMES_HOME` |
| `inference-sh` | inference.sh | `.inferencesh/skills` | `~/.inferencesh/skills` | no | no | `~/.inferencesh` |
| `iflow-cli` | iFlow CLI | `.iflow/skills` | `~/.iflow/skills` | no | no | `~/.iflow` |
| `jazz` | Jazz | `.jazz/skills` | `~/.jazz/skills` | no | no | `~/.jazz` **or** `cwd/.jazz` |
| `junie` | Junie | `.junie/skills` | `~/.junie/skills` | no | no | `~/.junie` |
| `kilo` | Kilo Code | `.agents/skills` | `~/.kilo/skills` | no | yes | `~/.kilo` **or** `~/.kilocode` |
| `kimchi` | Kimchi | `.kimchi/skills` | `~/.config/kimchi/harness/skills` (hard-coded) | no | no | `~/.config/kimchi` |
| `kimi-code-cli` | Kimi Code CLI | `.agents/skills` | `~/.agents/skills` | no | yes | `~/.kimi-code` **or** `~/.kimi` |
| `kiro-cli` | Kiro CLI | `.kiro/skills` | `~/.kiro/skills` | no | no | `~/.kiro` |
| `kode` | Kode | `.kode/skills` | `~/.kode/skills` | no | no | `~/.kode` |
| `lingma` | Lingma | `.lingma/skills` | `~/.lingma/skills` | no | no | `~/.lingma` |
| `loaf` | Loaf | `.agents/skills` | `~/.agents/skills` | no | yes* | `~/.loaf` |
| `mcpjam` | MCPJam | `.mcpjam/skills` | `~/.mcpjam/skills` | no | no | `~/.mcpjam` |
| `minimax-code` | MiniMax Code | `.minimax/skills` | `~/.minimax/skills` | no | no | `~/.minimax` **or** `/Applications/MiniMax Code.app` |
| `mistral-vibe` | Mistral Vibe | `.vibe/skills` | `$VIBE_HOME/skills` | no | no | `$VIBE_HOME` |
| `moxby` | Moxby | `.moxby/skills` | `~/.moxby/skills` | no | no | `~/.moxby` |
| `mux` | Mux | `.mux/skills` | `~/.mux/skills` | no | no | `~/.mux` |
| `neovate` | Neovate | `.neovate/skills` | `~/.neovate/skills` | no | no | `~/.neovate` |
| `opencode` | OpenCode | `.agents/skills` | `$XDG_CONFIG_HOME/opencode/skills` | no | yes | `$XDG_CONFIG_HOME/opencode` |
| `openhands` | OpenHands | `.openhands/skills` | `~/.openhands/skills` | no | no | `~/.openhands` |
| `ona` | Ona | `.ona/skills` | `~/.ona/skills` | no | no | `~/.ona` |
| `pi` | Pi | `.agents/skills` | `~/.agents/skills` | no | yes | `~/.pi/agent` |
| `pochi` | Pochi | `.pochi/skills` | `~/.pochi/skills` | no | no | `~/.pochi` |
| `posit-assistant` | Posit Assistant | `.posit/assistant/skills` | `~/.posit/assistant/skills` | no | no | `~/.posit/assistant` **or** `~/.positai` (legacy) |
| `promptscript` | PromptScript | `.agents/skills` | — **none** | **yes** | yes* | `cwd/.promptscript` **or** `cwd/promptscript.yaml` |
| `qoder` | Qoder | `.qoder/skills` | `~/.qoder/skills` | no | no | `~/.qoder` |
| `qoder-cn` | Qoder CN | `.qoder/skills` | `~/.qoder-cn/skills` | no | no | `~/.qoder-cn` |
| `qwen-code` | Qwen Code | `.qwen/skills` | `~/.qwen/skills` | no | no | `~/.qwen` |
| `reasonix` | Reasonix | `.reasonix/skills` | `$REASONIX_HOME/skills`, else Windows `%APPDATA%/reasonix/skills`, else `~/.reasonix/skills` | no | no | same resolved dir |
| `replit` | Replit | `.agents/skills` | `$XDG_CONFIG_HOME/agents/skills` | no | yes† | `cwd/.replit` |
| `rovodev` | Rovo Dev | `.rovodev/skills` | `~/.rovodev/skills` | no | no | `~/.rovodev` |
| `roo` | Roo Code | `.roo/skills` | `~/.roo/skills` | no | no | `~/.roo` |
| `sarvam-code` | Sarvam Code | `.agents/skills` | `~/.agents/skills` | no | yes* | `$SARVAM_HOME` |
| `tabnine-cli` | Tabnine CLI | `.tabnine/agent/skills` | `~/.tabnine/agent/skills` | no | no | `~/.tabnine` |
| `terramind` | Terramind | `.terramind/skills` | `~/.terramind/skills` | no | no | `~/.terramind` |
| `tinycloud` | Tinycloud | `.tinycloud/skills` | `~/.tinycloud/skills` | no | no | `~/.tinycloud` |
| `trae` | Trae | `.trae/skills` | `~/.trae/skills` | no | no | `~/.trae` |
| `trae-cn` | Trae CN | `.trae/skills` | `~/.trae-cn/skills` | no | no | `~/.trae-cn` |
| `universal` | Universal | `.agents/skills` | `$XDG_CONFIG_HOME/agents/skills` | no | yes† | never (`false`) |
| `warp` | Warp | `.agents/skills` | `~/.agents/skills` | no | yes | `~/.warp` |
| `windsurf` | Windsurf | `.windsurf/skills` | `~/.codeium/windsurf/skills` | no | no | `~/.codeium/windsurf` |
| `zed` | Zed | `.agents/skills` | `~/.agents/skills` | no | yes | `$XDG_CONFIG_HOME/zed` **or** `%APPDATA%/Zed` **or** `$FLATPAK_XDG_CONFIG_HOME/zed` |
| `zcode` | ZCode | `.zcode/skills` | `~/.zcode/skills` | no | no | `~/.zcode` **or** `/Applications/ZCode.app` |
| `zencoder` | Zencoder | `.zencoder/skills` | `~/.zencoder/skills` | no | no | `~/.zencoder` |
| `zenflow` | Zenflow | `.zencoder/skills` | `~/.zencoder/skills` | no | no | `~/.zencoder` |
| `adal` | AdaL | `.adal/skills` | `~/.adal/skills` | no | no | `~/.adal` |

\* universal project dir, but hidden from the interactive universal section
(`showInUniversalPrompt: false`).
† universal project dir but excluded from the universal-agent set
(`showInUniversalList: false`), so it is never auto-added; it must be named explicitly.

`$XDG_CONFIG_HOME` above stands for the resolved config home, which is
`$XDG_CONFIG_HOME` when non-empty else `~/.config` on **every** platform (§5).

### 2.1 Set membership counts (observed, `src/agents.ts:903-954`)

| Set | Definition | Size |
|---|---|---|
| all ids | registry keys | 79 |
| universal (`skillsDir == .agents/skills`) | `isUniversalAgent` | 24 |
| `getUniversalAgents()` | universal minus `showInUniversalList:false` (`replit`, `universal`) | 22 |
| `getVisibleUniversalAgents()` | above minus `showInUniversalPrompt:false` (`antigravity`, `antigravity-cli`, `deepagents`, `dexto`, `firebender`, `loaf`, `sarvam-code`, `promptscript`) | 14 |
| `getNonUniversalAgents()` | complement of universal | 55 |
| `readsCanonicalGlobally()` | universal minus `globalReadsCanonical:false` (`antigravity`, `antigravity-cli`) | 22 |
| universal agents auto-added by `add` | `getUniversalAgents()` **and** `readsCanonicalGlobally()` — i.e. the 22 above minus `replit` and `universal` | 20 |

---

## 3. Registry flags that change behavior

| id(s) | flag | concrete effect |
|---|---|---|
| `claude-code` | `createProjectSkillsDirByDefault: true` | a project install may create a missing `.claude/` root, unlike every other non-universal agent (`src/installer.ts:83-100`, comment cites #1138/#1607) |
| `antigravity`, `antigravity-cli` | `globalReadsCanonical: false` | global installs are written/linked into `~/.gemini/config/skills`; they are **not** considered satisfied by `~/.agents/skills` (`src/installer.ts:397-434`, `tests/antigravity-global-install.test.ts:46-51`) |
| `antigravity`, `antigravity-cli`, `deepagents`, `dexto`, `firebender`, `loaf`, `sarvam-code`, `promptscript` | `showInUniversalPrompt: false` | installed anyway, only hidden from the interactive locked list (`src/agents.ts:916-929`) |
| `replit`, `universal` | `showInUniversalList: false` | not in `getUniversalAgents()`, so not auto-added by `ensureUniversalAgents` / restore; still installed by `--agent '*'` (`src/agents.ts:908-914`, `src/add.ts:449-460`) |
| `eve`, `promptscript` | `globalSkillsDir: undefined` | project-only: a global install returns an error for a named agent, is silently skipped for auto-detected agents (§7.4) |
| `eve` | (implicit) subagent fan-out | targets expand to `agent/skills` and/or `agent/subagents/<name>/skills` (`src/add.ts:396-411`, `src/agents.ts:878-901`) |

All flags are optional and default to the "boring" behavior **(observed)**.

---

## 4. Filesystem operations vocabulary

The port needs four distinct operations; conflating them breaks agents.

1. **canonical dir** — `<base>/.agents/skills/<skill>`, where `<base>` is the project
   cwd or the home dir (`src/installer.ts:116-119`) **(observed)**.
2. **agent dir** — `<agent base>/<skill>`, where the agent base is resolved per scope
   (§6) **(observed)**.
3. **link** — a symlink/junction at the agent dir pointing at the canonical dir, used in
   `symlink` mode **(observed)**.
4. **copy** — a full recursive copy of the skill into a directory, used in `copy` mode
   and as the symlink-failure fallback **(observed)**.

Both destination paths are validated with a prefix check on normalized absolute paths
(canonical base contains canonical dir, agent base contains agent dir) before anything
is written; failures surface as "Invalid skill name: potential path traversal detected"
(`src/installer.ts:72-81`, `340-357`) **(observed)**. Skill directory names are
lower-cased, non-`[a-z0-9._]` runs collapsed to `-`, leading/trailing dots and hyphens
stripped, capped at 255 chars, empty → `unnamed-skill` (`src/sanitize.ts:73-88`)
**(observed)**.

Before replacing a directory, the installer refuses if the destination is a **category
directory**: a real directory with no root `SKILL.md` and at least one child directory
that has one (e.g. Hermes' `skills/<category>/<skill>/SKILL.md`). This prevents deleting
unrelated skills (`src/installer.ts:169-187`, diagnostic `SKILLS_CATEGORY_DIR_CONFLICT`)
**(observed)**.

---

## 5. Path resolution rules

### 5.1 Project base

`project base = cwd` — the process working directory, or the `cwd` passed down by the
API layer. **There is no project-root discovery:** nothing walks up looking for
`.git`, `package.json`, a lock file or a marker directory (grep for
`findProjectRoot`/`dirname(cwd)` returns nothing) **(observed)**. Installing from a
subdirectory therefore creates `.agents/skills` in that subdirectory **(assumed
consequence, not exercised by a test)**.

OpenClaw is the one id whose *project* skills dir is not dotted: it is `skills/` at the
repo root (`src/agents.ts:177-189`) **(observed)**.

### 5.2 Home and platform resolution

- `home = os.homedir()` once at module load (`src/agents.ts:7`). On POSIX that honours
  `$HOME`; on Windows `os.homedir()` honours `$USERPROFILE` (Node semantics — the code
  never reads either variable itself) **(observed for "never reads it"; assumed for
  Node's env precedence)**.
- `configHome = xdgConfig ?? ~/.config` where `xdgConfig` comes from the `xdg-basedir`
  package (`src/agents.ts:8-9`). The in-code comment states this is deliberate, to match
  OpenCode/Amp/Goose behavior **on all platforms**, and the test asserts the macOS
  `~/Library/Preferences` and Windows `AppData` forms are *not* used
  (`tests/xdg-config-paths.test.ts:1-48`, `62-73`) **(observed intent; the dependency is
  not vendored in the clone, so its exact fallback is assumed)**.
- `$XDG_DATA_HOME` is **never** consulted for agent paths. It only appears in the test
  environment fixture (`src/test-utils.ts:81`) **(observed)**.
- `$XDG_STATE_HOME` is consulted, but only for the global lock file
  (`$XDG_STATE_HOME/skills/.skill-lock.json`, else `~/.agents/.skill-lock.json`) — not
  for any agent directory (`src/skill-lock.ts:71-82`, `tests/xdg-config-paths.test.ts:86-103`)
  **(observed)**.
- `%APPDATA%` (`process.env.APPDATA`) is read in exactly two places: the Reasonix home
  and Zed's detection. `%USERPROFILE%` and `%LOCALAPPDATA%` are never read
  (`src/agents.ts:17-23`; grep) **(observed)**.
- `FLATPAK_XDG_CONFIG_HOME` is read for Zed only (`src/agents.ts:23`, `:748-761`)
  **(observed)**.

### 5.3 Env-var overrides (all trimmed; empty/whitespace means "unset")

| Agent | Variable | Applies to | Notes |
|---|---|---|---|
| `codex` | `CODEX_HOME` | detection only | global install path stays `~/.agents/skills` — the override does **not** move the skills dir (`src/agents.ts:10`, `235-243`) |
| `claude-code` | `CLAUDE_CONFIG_DIR` | detection **and** global path | `$CLAUDE_CONFIG_DIR/skills` (`:11`, `:166-176`) |
| `mistral-vibe` | `VIBE_HOME` | both | `$VIBE_HOME/skills` (`:12`) |
| `hermes-agent` | `HERMES_HOME` | both | `$HERMES_HOME/skills` (`:13`) |
| `autohand-code` | `AUTOHAND_HOME` | both | `$AUTOHAND_HOME/skills` (`:14`) |
| `grok` | `GROK_HOME` | both | `$GROK_HOME/skills` (`:15`) |
| `sarvam-code` | `SARVAM_HOME` | detection only | global path is `~/.agents/skills` regardless (`:16`, `:675-684`) |
| `reasonix` | `REASONIX_HOME` | both | wins over `%APPDATA%` on Windows (`:17-21`) |

**(all observed)**

`Sarvam` is the odd one out: the variable moves what is *detected* but not where a
global skill is *installed* **(observed)**.

### 5.4 Agent base resolution (the only rule that matters)

Given an agent id and a scope:

- **global scope and `readsCanonicalGlobally(id)`** → canonical home dir
  `~/.agents/skills`.
- **project scope and `isUniversalAgent(id)`** → canonical project dir
  `./.agents/skills`.
- **Eve with a subagent, project scope** → `<cwd>/agent/subagents/<sanitized-name>/skills`.
- **otherwise** → `global ? globalSkillsDir : join(cwd, skillsDir)`.
- **global scope with `globalSkillsDir === undefined`** → the resolver falls back to
  `join(homedir(), skillsDir)` even though the agent has no global support. Callers are
  supposed to reject this earlier (`src/installer.ts:139-167`) **(observed)** — see §13.

**(observed, `src/installer.ts:139-167`)**

### 5.5 Special-cased agents, summarized

| Agent | Deviation |
|---|---|
| **OpenCode** | global is `$XDG_CONFIG_HOME/opencode/skills`, **not** `~/.opencode/skills`; project is the shared `.agents/skills`; detection is the XDG config dir (`src/agents.ts:563-571`) |
| **Crush** | global is `~/.config/crush/skills` **hard-coded**, so `XDG_CONFIG_HOME` is ignored even though the path shape looks XDG (`:271-279`) |
| **Kimchi** | global is `~/.config/kimchi/harness/skills`, hard-coded; detection is `~/.config/kimchi` (`:463-471`, `:62-67`) |
| **Windsurf** | global lives under Codeium: `~/.codeium/windsurf/skills` (`:739-747`) |
| **Cortex** | global lives under Snowflake: `~/.snowflake/cortex/skills` (`:262-270`) |
| **Antigravity / Antigravity CLI** | share one global dir, `~/.gemini/config/skills`, which is **not** the canonical dir; both skip `~/.agents/skills` for global reads (`:104-129`, `tests/xdg-config-paths.test.ts:75-84`) |
| **Deep Agents** | global is `~/.deepagents/agent/skills` (an `agent/` level) (`:289-298`) |
| **Droid** | installs into `.agents/skills` / `~/.agents/skills` for compat, but keeps `globalSkillsDir = ~/.factory/skills` so `remove` still cleans legacy installs (`:318-330`, comment) |
| **Amp / Replit / Universal** | one shared global dir, `$XDG_CONFIG_HOME/agents/skills`; `replit` and `universal` are hidden from universal auto-selection (`:95-103`, `:638-647`, `:826-833`) |
| **OpenClaw** | project dir is `skills/`; global resolves `.openclaw` → `.clawdbot` → `.moltbot`, defaulting to `.openclaw` (`:39-53`) |
| **Eve** | project-only; `agent/skills` plus per-subagent dirs; copy-mode writes nesting-aware frontmatter (§10.2) |
| **PromptScript** | project-only, shared `.agents/skills`, hidden from the universal prompt (`:807-816`) |
| **Zed** | detection spans three homes: XDG config, roaming `%APPDATA%/Zed` (capital Z), and Flatpak config (`:748-761`) |
| **Reasonix** | only agent whose path is platform-dependent: Windows → `%APPDATA%/reasonix`, elsewhere → `~/.reasonix` (`:17-21`, `:648-656`) |
| **Kilo** | detection also accepts legacy `~/.kilocode`; install still goes to `~/.kilo/skills` (`:452-462`) |
| **Posit Assistant** | detection also accepts the pre-rename `~/.positai` (`:76-83`) |
| **Kimi Code CLI** | detection accepts `~/.kimi-code` or legacy `~/.kimi` (`:472-480`) |
| **ZCode, MiniMax Code** | additionally detect a macOS `.app` bundle under `/Applications` (§7.2) |

**(all observed)**

---

## 6. Project vs global install placement, concretely

| situation | canonical write | per-agent write |
|---|---|---|
| project + universal agent | `./.agents/skills/<skill>` | none — agent dir *is* the canonical dir |
| project + non-universal agent, symlink mode | `./.agents/skills/<skill>` | `./<skillsDir>/<skill>` symlink → canonical |
| project + non-universal agent, copy mode | none | `./<skillsDir>/<skill>` full copy |
| global + universal agent that reads canonical | `~/.agents/skills/<skill>` | none (early return) |
| global + `globalReadsCanonical:false` (Antigravity), symlink mode | `~/.agents/skills/<skill>` | `~/.gemini/config/skills/<skill>` symlink → canonical |
| global + `globalReadsCanonical:false`, copy mode | none | copy straight into `~/.gemini/config/skills/<skill>` |
| global + non-universal agent, symlink mode | `~/.agents/skills/<skill>` | symlink into its `globalSkillsDir` |
| global + non-universal agent, copy mode | none | copy into its `globalSkillsDir` |

**(observed, `src/installer.ts:303-488`, `727-862`, `871-1013`, `1020-1195`;
`tests/antigravity-global-install.test.ts:53-77`)**

Project installs for a non-universal agent are **skipped** (skill stays only in
`.agents/skills`) when the agent's top-level project directory does not already exist —
unless the mode is copy, the user created the root, or the agent sets
`createProjectSkillsDirByDefault` (`src/installer.ts:83-100`, `440-456`) **(observed)**.
The "top-level directory" is the first `/`-segment of `skillsDir`, e.g. `.windsurf`,
`.kiro`, `data` (AstrBot), `.posit`, `skills` (OpenClaw) **(observed)**.

---

## 7. Detection

### 7.1 Mechanism

Every detector is an `async () => boolean` wrapping **filesystem existence checks**
(`existsSync`). There are:

- **no `$PATH` / binary lookups**,
- **no process-name checks**,
- **no version checks**,
- **no config-content parsing** except one: Eve reads `package.json` and looks for an
  `eve` entry in `dependencies` or `devDependencies` (`src/agents.ts:25-37`, `331-341`).

**(observed; grep for `which`/`PATH` in `src/agents.ts` yields nothing)**

`detectInstalledAgents(cwd)` runs all 79 detectors concurrently and then filters,
preserving **registry insertion order** — the result is *not* sorted
(`src/agents.ts:859-867`) **(observed)**. There is no caching: every call re-stats every
path **(observed)**. Detectors never throw in practice (the only risky one, the
`package.json` read, swallows errors), but a Go port should treat any error as
"not installed" **(assumed best practice)**.

Detectors that take `cwd` (project markers): `astrbot`, `codebuddy`, `continue`, `eve`,
`jazz`, `promptscript`, `replit` **(observed)**. All others ignore `cwd` and look only
at home.

### 7.2 Non-obvious detector details

| Agent | Heuristic detail |
|---|---|
| `zed` | `exists(configHome/zed)` **or** `APPDATA` non-blank and `exists(APPDATA/Zed)` (capital Z) **or** `FLATPAK_XDG_CONFIG_HOME` non-blank and `exists(.../zed)` (`src/agents.ts:748-761`) |
| `zcode` | `exists(~/.zcode)` **or** `exists('/Applications/ZCode.app')` (`:55-60`) |
| `minimax-code` | `exists(~/.minimax)` **or** `exists('/Applications/MiniMax Code.app')` (`:69-74`) |
| `kimchi` | `exists(~/.config/kimchi)` — hard-coded, XDG ignored (`:62-67`) |
| `posit-assistant` | `exists(~/.posit/assistant)` **or** `exists(~/.positai)` (`:76-83`) |
| `codex` | `exists($CODEX_HOME)` **or** `exists('/etc/codex')` (`:235-243`) |
| `openclaw` | any of `~/.openclaw`, `~/.clawdbot`, `~/.moltbot` (`:182-188`) |
| `kilo` | `~/.kilo` or legacy `~/.kilocode` (`:457-461`) |
| `kimi-code-cli` | `~/.kimi-code` or legacy `~/.kimi` (`:477-479`) |
| `astrbot` | `cwd/data/skills` **or** `~/.astrbot` (`:135-137`) |
| `eve` | `cwd/agent` **and** `package.json` declares `eve` (`:336-340`) |
| `promptscript` | `cwd/.promptscript` **or** `cwd/promptscript.yaml` (`:813-815`) |
| `replit` | `cwd/.replit` (`:644-646`) |
| `codebuddy` / `continue` | `cwd/<dir>` **or** `~/<dir>` (`:213-215`, `:258-260`) |
| `jazz` | `~/.jazz` **or** `cwd/.jazz` (`:430-432`) |
| `universal` | always `false` — it is a *target*, never a detection (`:832`) |

**(all observed)**

The two `/Applications/...` probes are **not gated on `process.platform === 'darwin'`**
(`src/agents.ts:59`, `:73`) **(observed)**. On Linux/macOS a directory with that exact
name would be "detected"; on Windows the leading `/` resolves to the current drive root
**(assumed)**. A faithful port either reproduces this or documents the deviation.

### 7.3 Ordering / confidence

There is **no confidence score and no priority ordering**. All detectors are equal;
"detected" is a boolean set. Ordering only matters for presentation and for the
"exactly one detected" shortcut (§7.4), both of which use registry order
**(observed)**.

### 7.4 Behavior with zero (or one) detected

`skills add` (regular source, `src/add.ts:2890-2983`) **(observed)**:

| detected | flags | result |
|---|---|---|
| Eve detected, not in a host agent | `-y` or user confirms the Eve prompt | targets = `['eve']` **only** |
| Eve detected, user declines | interactive | normal agent picker (Eve still listed) |
| 0 | `-y` | **all 79 ids** ("Installing to all agents") |
| 0 | interactive | fuzzy picker over all ids **except `eve`** |
| 1 | any | that agent + `ensureUniversalAgents(...)`, no prompt |
| ≥2 | `-y` | detected + `ensureUniversalAgents(...)` |
| ≥2 | interactive | fuzzy picker, universal agents shown as a locked section |

`ensureUniversalAgents` appends every universal agent that `readsCanonicalGlobally`
(i.e. the 20 of §2.1, including hidden-from-prompt ones but excluding Antigravity,
`replit` and `universal`) — so a single detected agent usually produces a target list of
about 21 ids that all resolve to the same directory (`src/add.ts:440-460`) **(observed)**.

`skills add` for a **well-known URL** source has a *separate* copy of this logic with two
divergences: it has **no Eve special case**, and its interactive picker **includes**
`eve` (`src/add.ts:1118-1178`, `1142-1146`) **(observed)**.

If stdin is not a TTY and a prompt would be needed, the CLI fails with
`SKILLS_PROMPT_REQUIRES_TTY` and installs nothing (`src/diagnostics.ts:45-49`)
**(observed)**.

### 7.5 Running inside a coding agent

`src/detect-agent.ts` wraps `@vercel/detect-agent` (dev dependency `^1.2.7`, not
vendored). It:

- caches the first result for the process lifetime and records it for telemetry
  (`src/detect-agent.ts:5`, `60-67`) **(observed)**;
- **suppresses the Cursor false positive**: `CURSOR_TRACE_ID` alone (present in Cursor's
  integrated terminal) is *not* enough. A real agent run needs `CURSOR_AGENT` non-blank
  or `CURSOR_EXTENSION_HOST_ROLE === 'agent-exec'`; otherwise the result is demoted to
  "not an agent" (`:12-35`, `src/detect-agent.test.ts:13-44`) **(observed)**;
- renames `cursor` → `cursor-cli` when the strong signal is present (`:29-32`)
  **(observed)**;
- maps host names to ids: `cursor`/`cursor-cli` → `cursor`, `claude`/`cowork` →
  `claude-code`, `devin` → `universal`, `replit` → `replit`, `gemini` → `gemini-cli`,
  `codex` → `codex`, `antigravity` → `antigravity`, `augment-cli` → `augment`,
  `opencode` → `opencode`, `github-copilot` → `github-copilot` (`:41-54`)
  **(observed)**.

What changes when a host agent is detected:

| Command | Effect |
|---|---|
| any | the banner/intro is suppressed (`src/cli.ts:321-330`, `src/add.ts:2576-2585`) |
| `add` | `--yes` is forced; if `--agent` was not given, targets become `[mapped agent] + universal-that-read-canonical` (`src/add.ts:2552-2563`) |
| `remove` | `--yes` is forced (`src/remove.ts:98-108`) |
| `sync` (`experimental_sync`) | `--yes` forced; targets become `[mapped agent] + **all** `getUniversalAgents()` — note this is *not* filtered by `readsCanonicalGlobally`, unlike `add` (`src/sync.ts:640-653`) |
| `find` | treated as non-interactive (`src/find.ts:411`) |

**(all observed)**

If the host name does not map to an id, `getAgentType` returns `null` and the CLI falls
back to the ordinary detection path (`src/add.ts:2558-2561`) **(observed)**.

The precise env-var set that `@vercel/detect-agent` inspects is outside this repo; only
the Cursor variables are visible here **(assumed for everything else)**.

---

## 8. The canonical/shared directory concept

`.agents/skills` is the single physical home for skill content; per-agent directories
are either aliases of it (universal agents) or links/copies into it (non-universal
agents). Constants: `AGENTS_DIR = '.agents'`, `SKILLS_SUBDIR = 'skills'`,
`UNIVERSAL_SKILLS_DIR = '.agents/skills'` (`src/constants.ts:1-3`) **(observed)**.

### 8.1 When the canonical dir is *not* used

- **copy mode** — content goes straight into each agent dir; no canonical write
  (`src/installer.ts:374-384`) **(observed)**.
- **Eve in symlink mode** — the "canonical" base becomes Eve's own
  `agent/skills` (or the subagent dir), because Eve owns its directories
  (`src/installer.ts:329-338`) **(observed)**.
- **global + `globalReadsCanonical:false` in copy mode** — only
  `~/.gemini/config/skills` receives content **(observed)**.

### 8.2 Link creation algorithm (`createSymlink`)

1. Resolve both paths to absolute; `realpath` both (falling back to the input on error).
2. If the real paths are identical → **succeed without creating anything**. This is how
   a universal agent "links" to the canonical dir: no link exists at all
   (`src/installer.ts:240-247`) **(observed)**.
3. Compare again after resolving **only the parent** through symlinks. This catches the
   common dotfiles setup where `~/.claude/skills` is itself a symlink into
   `~/.agents/skills`, so `~/.claude/skills/x` and `~/.agents/skills/x` are the same
   inode (`:216-226`, `249-257`) **(observed)**.
4. Refuse to proceed if the link path is a category directory (throws, does not fall
   back to copy) (`:259-260`, `298`) **(observed)**.
5. Remove an existing link outright; remove an existing real directory recursively;
   tolerate `ELOOP` by force-removing the broken link (`:262-282`) **(observed)**.
6. `mkdir -p` the link's parent (`:284-285`).
7. Compute the target:
   - **Windows:** `symlink(target, link, 'junction')` with the **absolute** resolved
     target (`:292-295`) **(observed)**.
   - **other platforms:** a **relative** link computed from the *realpath of the link's
     parent* to the (absolute) target (`:287-293`) **(observed)**.
8. Any failure (permissions, filesystem without symlinks, EPERM on Windows) returns
   `false`, and the caller falls back to a full copy and reports `symlinkFailed: true`
   (`:297-300`, `460-472`) **(observed)**.

Relative-on-POSIX matters: a repo `.agents/skills/x` linked from `.claude/skills/x`
yields a link target like `../../.agents/skills/x`, so the repo stays relocatable
**(observed; `tests/antigravity-global-install.test.ts:73` asserts the resolved link
equals the canonical dir)**.

### 8.3 Deduplication when paths collide

- Two agent ids mapping to the same directory (e.g. `zencoder`/`zenflow`,
  `qoder`/`qoder-cn` in a project, `amp`/`replit`/`universal` globally): installs are
  simply repeated against the same path. `cleanAndCreateDirectory` is destructive but
  idempotent, so the last write wins and the content is identical **(observed)**.
- List/attribution: `listInstalledSkills` deduplicates scan scopes by **exact path
  string** and attributes a skill found in an agent dir to the **first** agent id that
  claimed that path, so `zenflow` never appears in a listing even when the skill is
  present in `~/.zencoder/skills` (`src/installer.ts:1257-1301`, `1330-1348`)
  **(observed)**.
- Install targets are **not** deduplicated by path before installing; only
  `ensureUniversalAgents` deduplicates by id (`src/add.ts:451-457`) **(observed)**.
- `validate-agents.ts` *defines* a duplicate-`skillsDir`/`globalSkillsDir` check but the
  call is commented out with the note "it's fine to have duplicate skills dirs"
  (`scripts/validate-agents.ts:87-91`) **(observed)**.

---

## 9. Install mode: symlink vs copy

`InstallMode` at the CLI/API surface is `auto | symlink | copy`
(`src/api/types.ts:18-22`); the internal installer also knows `link` (symlink the
canonical dir itself at the source, no copy) but **no caller in v1.9.0 reaches it**
(grep for `'link'` install-mode call sites is empty) **(observed; treat as dead code)**.

Selection order for `skills add` (`src/add.ts:3069-3096`) **(observed)**:

1. `--copy` → copy.
2. else `defaultInstallMode(targets)`:
   - **copy** if all targets resolve to **one** project skills dir (`uniqueDirs.size <= 1`), or
   - **copy** if every target is Eve, else
   - **symlink**.
3. else (interactive, no `--copy`, mode is symlink) → prompt "Installation method"
   (Symlink recommended / Copy to all agents).
4. `--yes` skips the prompt and keeps the computed default.

`defaultInstallMode` keys on `agents[t.agent].skillsDir` (project-relative names), not on
resolved global paths (`src/add.ts:484-490`) **(observed)**. Consequences: a global
install to a single agent is **copy** mode even though the "unique dir" reasoning was
about project dirs; `--copy` is recorded in the project lock only when it changed the
outcome (`src/add.ts:3092-3096`) **(observed)**.

The well-known-URL flow instead defaults to plain `symlink` and never applies
`defaultInstallMode` (`src/add.ts:1219`) **(observed)** — a real divergence between the
two `add` paths.

Copy mode is *not* a per-agent property: there is no "this agent must always be copied"
flag. Antigravity only ends up copied because it is usually the sole target
(`tests/antigravity-global-install.test.ts:53-63`), and is linked once other agents
join (`:65-77`) **(observed)**.

---

## 10. Agent-specific install quirks

### 10.1 Frontmatter rewriting

Only **two** rewrites exist **(observed)**:

1. **Eve** — on every copy/write, `SKILL.md` frontmatter is rebuilt keeping only
   `name`, `description`, `license`, `compatibility`, `version`, `metadata`; everything
   else (`allowed-tools`, `context`, custom keys) is **dropped**, and the YAML is
   re-serialized as nested YAML rather than inline JSON
   (`src/installer.ts:507-538`, `613-616`, `786-813`, `944-949`) **(observed)**. If no
   whitelisted key survives, the frontmatter block is removed entirely and the leading
   blank line is stripped. This applies to disk skills, remote skills, well-known
   multi-file skills, and blob skills.
2. **`--rename`** — for any agent, the installed copy's frontmatter `name` is rewritten
   to the new name; symlinks are skipped (the target copy is rewritten on its own), and
   the block is otherwise preserved byte-for-byte except the `name` line (and its
   continuation lines) (`src/installer.ts:490-505`, `src/frontmatter.ts:18-45`)
   **(observed)**.

No agent gets `allowed-tools` rewriting, no agent gets a `description` rewrite, and no
agent gets path/URL rewriting **(observed)**.

### 10.2 Eve

- Project-only (`globalSkillsDir: undefined`); a *named* global install fails with
  "`<display>` does not support global skill installation"
  (`src/installer.ts:313-321`) **(observed)**.
- Root target `agent/skills`; subagents at `agent/subagents/<name>/skills`, names
  discovered by reading `agent/subagents/*` directories and **sorted**
  (`src/agents.ts:878-901`) **(observed)**.
- Subagent names from user input are sanitized before being used as a path segment, and
  a name like `../../etc` stays under `agent/subagents`
  (`tests/eve-agent.test.ts:156-161`) **(observed)**.
- `--subagent root` and `--subagent .` both mean the root agent
  (`src/add.ts:2999-3003`) **(observed)**.
- Default mode for an Eve-only install is **copy**; if other agents are also targeted,
  mode is symlink and Eve's directory becomes the canonical base
  (`src/add.ts:484-490`, `src/installer.ts:329-338`) **(observed)**.
- Detection requires `cwd/agent` **and** an `eve` dependency in `package.json`
  (`src/agents.ts:336-340`) **(observed)**.

### 10.3 OpenClaw

- Project skills root is `skills` (not dotted). The general "skip if the agent's
  top-level project dir is missing" rule then means: in symlink mode, a project install
  is **skipped** unless `./skills` already exists — unless mode is copy, or
  `createMissingAgentRoot` is set (restore-from-lock sets it)
  (`src/installer.ts:83-100`, `440-456`; `src/install.ts:294-309`) **(observed)**.
- Because `./skills` is often the *source* repository's own skill folder, the installer
  refuses to install a skill onto itself: if the destination overlaps the source, it
  returns `success: true, skipped: true` instead of deleting the user's source
  (`src/installer.ts:359-372`) **(observed)**.
- Global resolution prefers the existing legacy dirs rather than migrating: `.openclaw`
  → `.clawdbot` → `.moltbot` → default `.openclaw` (`src/agents.ts:39-53`,
  `tests/openclaw-paths.test.ts:8-29`) **(observed)**.

### 10.4 Other naming quirks

| Agent | Quirk |
|---|---|
| `astrbot` | project dir `data/skills` at the repo root (no dot) |
| `tabnine-cli` | an extra `agent/` level: `.tabnine/agent/skills` |
| `posit-assistant` | two levels: `.posit/assistant/skills` |
| `deepagents` | global has an `agent/` level but the project dir does not |
| `kimchi` | global has a `harness/` level: `~/.config/kimchi/harness/skills` |
| `qoder` / `qoder-cn` | share the project dir `.qoder/skills`, differ only globally |
| `trae` / `trae-cn` | share the project dir `.trae/skills`, differ only globally |
| `zencoder` / `zenflow` | share **both** dirs, and share the detector |
| `antigravity` / `antigravity-cli` | share the global dir, differ only in detector |

**(all observed)**

### 10.5 Removal-side quirks (affects path ownership)

- `skills remove` without `--agent` targets **all 79 ids**, not just detected ones, to
  clean ghost links (`src/remove.ts:240-248`) **(observed)**.
- Removal scans the canonical dir plus every agent's dir; project scope also scans Eve
  subagent dirs (`src/installer.ts:1257-1301`, `src/remove.ts:299-357`) **(observed)**.
- The canonical copy is deleted only when no other agent still holds the skill; the
  decision is made from the detected-agent set (`tests/remove-canonical.test.ts:44-104`)
  **(observed)**.
- `listInstalledSkills` also scans agent dirs for **undetected** agents whose directory
  happens to exist, so a skill installed with `--agent X` survives `~/.X` disappearing
  (`src/installer.ts:1285-1300`) **(observed)**.

---

## 11. Discovery-side use of the registry

The registry's `skillsDir` list also drives *source-repo* discovery. `discoverSkills`
searches, in order: the search root, `skills/`, `skills/.curated/`,
`skills/.experimental/`, `skills/.system/`, then every `AGENT_PROJECT_SKILL_DIRS` entry,
then repo-declared container paths (`src/skills.ts:260-282`) **(observed)**.

`AGENT_PROJECT_SKILL_DIRS` is the **de-duplicated, lexicographically sorted** union of
all `skillsDir` values plus eight **legacy** dirs kept for existing skill repositories:
`.cline/skills`, `.codex/skills`, `.factory/skills`, `.github/skills`, `.kilo/skills`,
`.kilocode/skills`, `.opencode/skills`, `.pi/skills`. The literal `skills` entry is
filtered out because the standard root is handled separately
(`src/agents.ts:836-857`) **(observed)**.

Consequence for a Go port: adding a new agent id silently changes what skill repos the
*installer* discovers, not just where it installs **(observed)**. And opposing this,
`docs/creating-skills.md`'s discovery list is generated from the same data
(`scripts/sync-agents.ts:63-77`) **(observed)**.

---

## 12. Ordering, wildcards, presentation

### 12.1 Ordering

- `Object.keys(agents)` = **insertion order** of the object literal (aider-desk first,
  universal last). Nothing sorts the registry **(observed)**.
- `detectInstalledAgents` preserves that order **(observed)**.
- `AGENT_PROJECT_SKILL_DIRS` is sorted alphabetically **(observed)**.
- `getEveSubagents` sorts **(observed)**.
- `docs/agents.md` rows are grouped by `(skillsDir, globalSkillsDir)` in first-seen
  order, with co-located agents' display names joined in encounter order — e.g.
  "Amp, Replit, Universal" and "Cline, Codex, Dexto, fx, Kimi Code CLI, Loaf, Pi, Sarvam
  Code, Warp, Zed" (`scripts/sync-agents.ts:21-61`) **(observed)**.
- The interactive picker lists non-universal agents in registry order with `skillsDir`
  (or `globalSkillsDir` under `-g`) as the hint, preceded by a **locked, always-selected**
  universal section (`src/add.ts:639-699`) **(observed)**.
- Skill listings and removal candidates are sorted by name, not by agent
  (`src/remove.ts:356`) **(observed)**.

### 12.2 Wildcard `*`

- `--agent '*'` expands to **all 79 ids**, including `universal`, `replit`, and the
  project-only `eve`/`promptscript` (`src/add.ts:1122-1125`, `2893-2896`;
  `src/install.ts:27`) **(observed)**.
- `--all` = `--skill '*' --agent '*' --yes` (`src/add.ts:2545-2550`) **(observed)**.
- On a **global** install, `*` (and auto-detection with `-y`) **silently drops** agents
  with no global dir (Eve, PromptScript) with a single notice — unless *every* target
  lacks one, in which case the list is kept and each install errors
  (`src/add.ts:614-633`, `tests/global-agent-skip.test.ts:49-54`) **(observed)**.
- A **named** `--agent eve` global install keeps the hard error
  (`tests/global-agent-skip.test.ts:35-40`) **(observed)**.
- `*` is **not** positional or glob-expanded against other ids; it is a literal token
  checked with `includes('*')` **(observed)**.
- `skills use --agent` **rejects** `*` and accepts exactly one agent
  (`src/diagnostics.ts:324-329`, `src/use.ts:475-479`) **(observed)**.

### 12.3 Validation of `--agent`

`--agent` is validated against registry keys *before* fetching or prompting, and the
error lists every valid id (`src/add.ts:2590-2604`, `src/diagnostics.ts:20-23`)
**(observed)**. The API surface (`getSupportedAgents`) exposes
`{id, name: displayName, projectSkillsDir, globalSkillsDir|null, universal}`
(`src/api/agents.ts:8-26`) **(observed)**.

### 12.4 Cost note for `*`

`installSkillForAgent` is called once per (skill, target). For universal targets, the
canonical directory is cleaned and re-copied **per agent** because the "source is the
canonical dir" short-circuit does not apply to a temp clone
(`src/installer.ts:386-421`) **(observed)**. `--agent '*'` therefore rewrites
`.agents/skills/<skill>` roughly 20+ times. Output is correct and idempotent; a Go port
would naturally install the canonical copy once **(assumed improvement, not current
behavior)**.

---

## 13. Surprises and undocumented behavior

1. **No project-root discovery.** `cwd` *is* the project. Running from a subdirectory
   creates a nested `.agents/skills` **(observed §5.1)**.
2. **`XDG_DATA_HOME` is never used**, despite the name of the test file; only
   `XDG_CONFIG_HOME` (agents) and `XDG_STATE_HOME` (lock) matter **(observed)**.
3. **Crush and Kimchi hard-code `~/.config`** and ignore `XDG_CONFIG_HOME`, while Amp,
   Devin, Goose, OpenCode, Replit and Universal honour it **(observed)**.
4. **Codex's `CODEX_HOME` moves detection but not installation**, and Sarvam's
   `SARVAM_HOME` likewise **(observed)**.
5. **`/Applications/*.app` probes are not gated on macOS** **(observed)**.
6. **The duplicate-directory validator is disabled** even though several ids share
   directories; `listInstalledSkills` only ever attributes such a directory to the first
   id **(observed)**.
7. **Two `add` code paths differ**: the well-known-URL path has no Eve special case,
   includes Eve in its picker, and defaults to `symlink` instead of the
   `defaultInstallMode` decision **(observed)**.
8. **`sync`'s in-agent default adds *all* universal agents**, including
   `globalReadsCanonical:false` ones, unlike `add` **(observed)**.
9. **`getAgentBaseDir` invents `~/agent/skills` for a global Eve install** when called
   with `global: true`; only the installer's earlier guard prevents it
   (`src/installer.ts:158-163`) **(observed)**.
10. **`installSkillForAgent` skips rather than fails** when the destination overlaps the
    source, and reports `success: true` **(observed §10.3)**.
11. **Universal "links" are usually not links**: identical realpaths short-circuit
    before any filesystem write **(observed §8.2)**.
12. **`skills remove` with no `--agent` iterates all 79 agents**, so removal cost and
    scope are decoupled from detection **(observed)**.
13. **`--agent '*'` on a global install leaves two ids out** (Eve, PromptScript) while
    still claiming "all agents" in the log **(observed)**.
14. **The registry is duplicated in four places**: the `AgentType` union, the registry
    object, `package.json` `keywords`, and `docs/agents.md`; only the latter two are
    generated/checked by `scripts/sync-agents.ts` **(observed)**.

---

## 14. Where code and `docs/agents.md` disagree

The table in `docs/agents.md` is generated from the registry by
`scripts/sync-agents.ts:21-61`, and every generated row **matches the code at v1.9.0**
(verified id-by-id, 79 ids → 66 grouped rows) **(observed)**. What the docs *omit* or
*flatten*:

| Docs claim / omission | Code reality | Evidence |
|---|---|---|
| Everything is detected then the user is prompted | `-y` installs to **every** agent; a single detected agent is auto-selected; inside a coding agent it installs to that agent only; a non-TTY aborts | `docs/agents.md:88-89` vs `src/add.ts:1135-1138`, `1159-1161`, `2934-2963` |
| OpenClaw global is `~/.openclaw/skills/` | legacy `~/.clawdbot/skills` and `~/.moltbot/skills` are preferred when present | `docs/agents.md:18` vs `src/agents.ts:39-53` |
| Reasonix global is `~/.reasonix/skills/` | Windows uses `%APPDATA%\reasonix\skills` (or the `AppData\Roaming` fallback) | `docs/agents.md:61` vs `src/agents.ts:17-21` |
| Antigravity global `~/.gemini/config/skills/` presented like any other row | it is the **only** pair that does not read `~/.agents/skills` globally, so global placement is link/copy rather than "already there" | `docs/agents.md:12` vs `src/agents.ts:111`, `124`, `src/agents.ts:952-954` |
| Eve project path `agent/skills/` | also `agent/subagents/<name>/skills`, plus frontmatter stripping | `docs/agents.md:32` vs `src/agents.ts:331-341`, `878-901`, `src/installer.ts:507-538` |
| No mention of hidden universal agents | 8 ids are installed as universal but hidden from the prompt (`antigravity`, `antigravity-cli`, `deepagents`, `dexto`, `firebender`, `loaf`, `sarvam-code`, `promptscript`); `replit`/`universal` are hidden from the universal set entirely | `docs/agents.md:19` vs `src/agents.ts:112`, `125`, `294`, `313`, `347`, `513`, `680`, `812`, `643`, `831` |
| No mention of `createProjectSkillsDirByDefault` | `claude-code` alone may create `.claude/` on demand | `docs/agents.md:17` vs `src/agents.ts:172` |
| Kimchi global `~/.config/kimchi/harness/skills/` | matches, but the docs imply an XDG path while the code hard-codes `.config` | `docs/agents.md:45` vs `src/agents.ts:463-471` |
| Table shows `~` paths | paths are generated by string-replacing the generator machine's `homedir()`, so a doc regenerated on Windows would print `C:\Users\...` and env overrides are invisible either way | `scripts/sync-agents.ts:50` |

No row is missing, no row has a wrong path, and no id is documented that does not exist
**(observed)**.

---

## 15. Porting notes (Go)

Suggested decomposition that preserves the observed semantics:

- A single ordered slice of records → gives insertion order for free; a map lookup by id
  for validation.
- Record fields mirroring §1, with `GlobalSkillsDir *string` (nil = project-only).
- A `Home` struct resolved once at startup: `home`, `configHome`, `appData`,
  `flatpakConfigHome`, plus the eight env overrides — injectable for tests.
- `Detect` as a per-record closure receiving `cwd` and a `PathExists func(string) bool`,
  so the four special detectors (`zcode`, `minimax-code`, `kimchi`,
  `posit-assistant`, `openclaw`) are testable exactly as in the existing tests.
- Two path helpers: `CanonicalDir(global, cwd)` and `AgentBaseDir(id, global, cwd,
  subagent)` implementing §5.4 verbatim, including the `~/<skillsDir>` fallback for
  `globalSkillsDir == nil`.
- One `Link(canonical, agentDir)` implementing §8.2 (realpath equality, parent-symlink
  equality, category guard, remove-then-link, relative-on-POSIX /
  absolute-junction-on-Windows, `ErrFallbackToCopy`).
- Mode selection as one function implementing §9, and a documented decision on which of
  the two `add` paths (regular vs well-known) the port keeps.

---

## 16. Evidence index (special cases)

| Topic | Location |
|---|---|
| home / configHome / env overrides | `src/agents.ts:7-23` |
| `package.json` dependency probe (Eve) | `src/agents.ts:25-37` |
| OpenClaw global fallback | `src/agents.ts:39-53` |
| ZCode / MiniMax / Kimchi / Posit detectors | `src/agents.ts:55-83` |
| Antigravity + Antigravity CLI flags | `src/agents.ts:104-129` |
| Claude Code project-dir flag | `src/agents.ts:166-176` |
| OpenClaw project dir `skills` | `src/agents.ts:177-189` |
| Droid compat comment | `src/agents.ts:318-330` |
| Eve record | `src/agents.ts:331-341` |
| Kimchi record | `src/agents.ts:463-471` |
| OpenCode / Crush / Cortex / Windsurf records | `src/agents.ts:271-279`, `262-270`, `563-571`, `739-747` |
| Replit / Universal hidden flags | `src/agents.ts:638-647`, `826-833` |
| PromptScript record | `src/agents.ts:807-816` |
| Zed detector | `src/agents.ts:748-761` |
| Reasonix record | `src/agents.ts:648-656` |
| Legacy discovery dirs + sorted union | `src/agents.ts:836-857` |
| `detectInstalledAgents` (concurrent, ordered) | `src/agents.ts:859-867` |
| Eve subagents | `src/agents.ts:878-901` |
| universal/visible/non-universal helpers | `src/agents.ts:903-954` |
| Cursor demotion + host-name mapping | `src/detect-agent.ts:12-54` |
| host-agent cache + telemetry | `src/detect-agent.ts:60-67` |
| project-symlink skip rule | `src/installer.ts:83-100` |
| canonical dir | `src/installer.ts:116-119` |
| Eve subagent dir + sanitization | `src/installer.ts:126-129` |
| agent base resolution | `src/installer.ts:139-167` |
| category-directory guard | `src/installer.ts:169-187` |
| parent-symlink resolution | `src/installer.ts:216-226` |
| symlink creation incl. junction | `src/installer.ts:232-301` |
| overlap-with-source skip | `src/installer.ts:359-372` |
| copy mode bypasses canonical | `src/installer.ts:374-384` |
| universal global early return | `src/installer.ts:424-434` |
| symlink→copy fallback | `src/installer.ts:458-472` |
| Eve frontmatter whitelist | `src/installer.ts:507-538` |
| recursive copy + symlink safety | `src/installer.ts:549-625` |
| list scopes + dedupe + undetected agents | `src/installer.ts:1211-1301` |
| `buildInstallTargets` (Eve fan-out) | `src/add.ts:396-411` |
| `ensureUniversalAgents` (canonical filter) | `src/add.ts:440-460` |
| `nonInteractiveTargetAgents` | `src/add.ts:467-477` |
| `defaultInstallMode` | `src/add.ts:484-490` |
| global-dir skip rule | `src/add.ts:614-633` |
| interactive universal locked section | `src/add.ts:639-699` |
| well-known flow agent selection | `src/add.ts:1118-1178` |
| well-known flow default mode | `src/add.ts:1219-1245` |
| in-agent auto non-interactive | `src/add.ts:2540-2563` |
| `runAdd` agent selection + Eve special case | `src/add.ts:2890-2983` |
| mode selection / lock recording | `src/add.ts:3069-3096` |
| `sync` in-agent universal list | `src/sync.ts:640-653` |
| `skills use` supported agents | `src/use.ts:83-88` |
| remove targets all agents | `src/remove.ts:240-248` |
| remove scan incl. Eve subagents | `src/remove.ts:299-357` |
| XDG lock path | `src/skill-lock.ts:71-82` |
| constants | `src/constants.ts:1-6` |
| doc generator | `scripts/sync-agents.ts:21-77` |
| disabled duplicate-dir validator | `scripts/validate-agents.ts:87-91` |
| tests: XDG | `tests/xdg-config-paths.test.ts:21-115` |
| tests: OpenClaw | `tests/openclaw-paths.test.ts:5-30` |
| tests: Antigravity global | `tests/antigravity-global-install.test.ts:45-110` |
| tests: Eve | `tests/eve-agent.test.ts:42-298` |
| tests: Reasonix | `tests/reasonix-agent.test.ts:13-80` |
| tests: universal membership (fx) | `tests/fx-agent.test.ts:11-23` |
| tests: global-dir skip | `tests/global-agent-skip.test.ts:14-65` |
| tests: remove canonical protection | `tests/remove-canonical.test.ts:44-104` |

---

## Open questions

1. **`xdg-basedir` exact behavior on Windows/macOS.** The in-tree comment and tests
   assert "always `$XDG_CONFIG_HOME` or `~/.config`", but the package is not vendored
   here, so its real fallback chain (and whether it ever reads `%APPDATA%`) could not be
   verified **(assumed)**. A Go port must decide between "match xdg-basedir" and "match
   the platform convention".
2. **Should `listInstalledSkills` attribute a shared directory to *every* agent id that
   maps to it** (`zencoder`/`zenflow`, `qoder`/`qoder-cn`), or keep today's
   first-claimant behavior? Today's behavior is observable but arguably a bug.
3. **Which `add` implementation is authoritative for well-known URLs** — the duplicated
   block at `src/add.ts:1118-1245` and the main flow at `2890-3096` disagree on the Eve
   special case, the picker contents, and the default install mode. No test pins the
   well-known path's agent selection.
4. **Is the internal `link` install mode (`src/installer.ts:39-44`) intended to stay?**
   No caller in the tree uses it; if a Go port drops it, nothing observable changes
   today, but the public API type does not expose it either.
5. **Are the `/Applications/*.app` checks supposed to be macOS-only?** The code does not
   gate them; no test covers a non-macOS host.
6. **Does `@vercel/detect-agent` need a faithful Go reimplementation?** Its env-var
   matrix is external and only the Cursor refinement is visible in this repo; the Go
   port needs an explicit decision (full parity vs. the ten host-name mappings in §7.5).
7. **`getAgentBaseDir(id, global=true)` for a project-only agent** returns
   `~/<skillsDir>`; is any caller relying on that fallback, or should it be an error in
   a port? Current callers guard it, but the exported function does not.
8. **Should the canonical copy be written once per skill instead of once per universal
   target?** Correctness is unaffected; the current per-target rewrite is observable
   only as I/O cost.
