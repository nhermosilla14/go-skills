# 07 — Skill format, validation, and discovery

Behavioral reference for `vercel-labs/skills` v1.9.0 (MIT), reconstructed from source and tests.
Purpose: enough precision to reimplement in Go without re-reading TypeScript.

Evidence convention: **(observed)** = read directly in source/tests (file:line at the end);
**(assumed)** = inferred from library semantics or unexercised code paths, not verified by running.

---

## 1. What a skill is on disk

A skill is a **directory** that contains a file named exactly `SKILL.md` (case-sensitive on
Linux/macOS filesystems). **(observed)**

- `hasSkillMd(dir)` stats `<dir>/SKILL.md` and requires `isFile()`. Not a directory, not a
  symlink-to-directory, not `skill.md`. (`src/skills.ts:47-55`)
- Disc skill payload = the whole directory: `scripts/`, `references/`, `assets/`, etc. are copied
  recursively (symlinks dereferenced; escaping/cyclic links skipped). (`src/installer.ts:562-625`)
- Root-level skills are a supported shape: `<repo>/SKILL.md` means the repo itself is the skill
  directory. On the blob fast path the payload is trimmed to `SKILL.md` only. **(observed)**
  (`src/skills.ts:245-258`, `src/blob.ts:744-752`)
- Eve is an exception: it consumes a **flat** `<sanitized-name>.md` file, not a directory.
  (`src/installer.ts:631-641`, `:785-792`)

The skill's identity everywhere (folder name, lock key, CLI display) is the **frontmatter `name`**
after terminal sanitization — not the directory name. Directory names in a source repo are
irrelevant except for fallback matching. **(observed)** (`src/skills.ts:103`, `:122-128`)

---

## 2. The `SKILL.md` frontmatter contract

### 2.1 Delimiters

Parsing is a hand-written regex, not gray-matter. (`src/frontmatter.ts:8-16`)

```
^\uFEFF?---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$
```

Consequences (all **(observed)** from the regex + tests):

- File must start with `---` + newline, optionally preceded by a UTF-8 BOM.
- `---js` / `---javascript` / `---yaml` openers are **not** recognized, deliberately (avoids
  `eval`-based RCE). Such files parse as "no frontmatter at all" — the entire text becomes the
  body and the skill is skipped for missing keys. Tested at `src/frontmatter.test.ts:24-27`.
- Opening `---` must be immediately followed by a newline: a trailing space (`--- `) breaks it.
- The **first** subsequent line that is exactly `\r?\n---` closes the block (non-greedy match), so a
  literal `---` line inside a block scalar terminates the frontmatter early.
- LF and CRLF both work, and the match captures the input verbatim otherwise: BOM and line endings
  survive into `rawContent` (used for hashing). Tested `src/frontmatter.test.ts:9-22`, `:36-54`.
- An empty block (`---\n---\n`) does **not** match (the inner `[\s\S]*?\r?\n---` cannot be satisfied),
  so it behaves as "no frontmatter" rather than "empty data". **(observed, derived from regex;
  not unit-tested)**
- Text without frontmatter is returned unchanged with `data = {}`. Not an error at the parser level.

Encoding: file is read as UTF-8 (`src/skills.ts:72`). BOM is not stripped by Node. UTF-16 or invalid
bytes yield replacement characters and will not parse; there is no encoding detection or
normalization pass. **(assumed for invalid-byte behavior)**

### 2.2 Required keys

| Key | Type | Rules |
|---|---|---|
| `name` | string | Must be present, a YAML **string** (not number/bool), and non-empty after terminal-sanitization + trim. |
| `description` | string | Same: present, string type, non-empty after sanitization. |

Validation is literally three checks, in this order (`src/skills.ts:86-111`):

1. truthiness of `data.name` / `data.description` → else skip with
   `missing required frontmatter field(s): name, description`;
2. `typeof === 'string'` for both → else skip with a message quoting both observed types
   (so `name: 123` or `name: true` is a **skip**, not a coercion);
3. `sanitizeMetadata()` then non-empty → else skip with
   `empty required frontmatter field(s): …`.

There is **no** validation of name length, character set, kebab-case, or uniqueness at parse time.
`name: Bad Name` and `name: 日本語` are accepted; only the install folder is sanitized. **(observed)**
No `description` length cap is enforced anywhere (`docs/creating-skills.md:29-38` documents none
either). Common ecosystem limits (64 / 1024 chars) are **not** applied. **(observed absence)**

### 2.3 Optional and unknown keys

- Recognized by the CLI: `metadata` only (`src/skills.ts:116`). Everything else (`license`,
  `version`, `allowed-tools`, `compatibility`, vendor keys, …) is carried inside `rawContent` and
  ignored by parsing. **(observed)**
- Unknown keys are fully tolerated: no schema, no rejection, no warning. **(observed)**
- `metadata` is surfaced on the `Skill` object only when it is a YAML **mapping**; scalars/arrays
  are dropped (`isRecord` check). Stored unvalidated. (`src/skills.ts:24-26`, `:116`)
- Eve is the one place with an allowlist: on install it rewrites frontmatter to keep only
  `name`, `description`, `license`, `compatibility`, `version`, `metadata` — see §3.3.

### 2.4 `metadata.internal`

An internal skill is hidden from normal discovery. Recognition (`src/skills.ts:41-45`):

```yaml
metadata:
  internal: true      # accepted
  # internal: "true"  # accepted (spec-compliant string form)
  # internal: "1"     # accepted
```

- Only `true`, `'true'`, `'1'` are truthy. `'TRUE'`, `'yes'`, `1` (number), `'false'` are **not**.
  Tested in `src/add.test.ts:1170-1196`. **(observed)**
- `metadata` must be a mapping; `metadata: internal` yields nothing. **(observed)**
- Gating: a skill matching this flag is dropped during parse unless
  `INSTALL_INTERNAL_SKILLS` is `1` or `true`, *or* the caller passed `includeInternal`.
  Env check is exact-string, case-sensitive. (`src/skills.ts:32-35`, `:113-120`)
- Silent drop, not a warning: `parseSkillMd` returns `null` without emitting `SKILLS_SKILL_SKIPPED`.
  **(observed)**

### 2.5 When a skill without frontmatter is skipped vs. fatal

**Always skip, never fatal.** Every failure mode is a per-file warning plus `null`:

| Failure | Result |
|---|---|
| unreadable file | warning `failed to read file: …`, skip |
| YAML parse throws | warning `YAML parse error: …`, skip |
| missing/typed-wrong/empty `name`/`description` | warning, skip |
| internal and gated off | silent skip |
| no frontmatter at all | warning about missing `name, description`, skip |

Warnings are emitted through `warn()` as diagnostics
`⚠ Skipped <path> — <reason>`, with path and reason terminal-sanitized; outside
`withWarnings()` they go to `console.warn`. (`src/skills.ts:57-64`, `src/warnings.ts:9-22`,
`src/diagnostics.ts:139-142`) **(observed)**

The only fatal path reachable from discovery is a malformed **`skills-lock.json` in the source
repo** (see §6.5). Discovered skills list can therefore be empty without any error — callers turn
that into `SKILLS_NO_VALID_SKILLS` (`src/add.ts:2703-2709`). **(observed)**

YAML shape notes that matter for a Go port (`yaml.v3` semantics differ):

- Quoted scalars, folded/literal block scalars, nested mappings, anchors/aliases and comments all
  work — it is real YAML 1.2 via `yaml@^2.9.1`. **(observed dependency; assumed for exotic nodes)**
- A multi-line `description: >` is collapsed to one line by `sanitizeMetadata` later, not by YAML.
- Multiple YAML documents in one block (a second `---` inside the frontmatter) make
  `yaml.parse` throw ("Source contains multiple documents"), which surfaces as a YAML-parse skip.
  **(assumed: library behavior not executed here)**
- Duplicate keys: `yaml` throws on duplicates by default → parse skip. **(assumed)**

---

## 3. Frontmatter rewriting

Two rewriters exist and they are very different in spirit: `setFrontmatterName` (surgical) and
Eve's `stripIgnoredEveFrontmatter` (destructive).

### 3.1 `setFrontmatterName(raw, name)` — rename only

Used to make an installed copy answer to its installed name. (`src/frontmatter.ts:24-45`)

```
open  = "\uFEFF?---" + eol          # eol captured from the opening line
body  = everything up to the next  "\n---" line
close = that "\n---" + ("\n" or EOF)
nameLine = stringify({name}) trimmed  # -> `name: <value>`, YAML-quoted when needed
if body has a line matching /^name\s*:/:
    replace it AND every following line that starts with space/tab   (kills block-scalar continuations)
else:
    insert nameLine as the first body line
return open + lines.join(eol) + close + rest_of_file
```

Preservation / loss rules **(observed, tested `src/frontmatter.test.ts:57-94`)**:

- Everything outside the `name` value is byte-identical: other keys, comments, key order, body,
  trailing content, BOM, CRLF vs LF.
- A quoted `name: "Alpha Skill"` and a folded `name: >` block are replaced by a single plain
  `name: beta` line; their continuation lines are removed.
- Missing `name` is prepended as the **first** frontmatter key (order of the rest unchanged).
- Values YAML would coerce get quoted by the serializer, so `--rename 123` round-trips as the
  string `"123"` rather than the number (test at `:87-90`).
- No frontmatter → returns input unchanged (so the installed file has no name).
- Known blind spots (derived from the regexes, **(observed)**, not unit-tested):
  - a quoted key `"name": x` is not matched → a second `name` key is prepended;
  - an indented key (`  name: x`, e.g. under `metadata:`) is not matched — correct, but note that
    a nested `name` is therefore never touched;
  - mixed line endings inside the block are normalized to the opening line's EOL
    (`lines.join(eol)`), so a mostly-CRLF file with one LF line is rewritten uniformly;
  - only the first matching top-level `name` is replaced.

### 3.2 Where the rename rewrite is applied

`setInstalledSkillName(path, name)` (`src/installer.ts:496-505`) **always** uses
`setFrontmatterName`:

- `path` may be a skill directory (rewrites `<dir>/SKILL.md`) or a flat file (Eve's `<name>.md`).
- **Symlinks are skipped** — the link target is rewritten on its own. This prevents writing through
  a link into the user's source tree.
- Non-file targets are ignored; write happens only if the text actually changed.

Call sites (`src/add.ts:1907-1911` for disk/blob installs, `:1364-1368` for well-known installs):
both the canonical and the agent copy are rewritten (deduped when identical), and the rewrite is
**skipped when the install was skipped for "source is destination"** so the user's own copy is
never touched. **(observed)**

Consequences to preserve in Go:

- The installed `SKILL.md` no longer equals upstream: name differs, nothing else.
- The lock's `computedHash` is computed **after** the rewrite, by applying
  `setFrontmatterName` to the collected SKILL.md bytes before hashing (`src/add.ts:2431-2449`).
  A Go port must hash the same post-rewrite bytes or `check`/`--frozen-lockfile` will report drift.
- `update` re-applies the rename: `name` stays the installed name, `skillName` in the lock keeps the
  upstream name. Tested `tests/rename.test.ts:142-161`.

### 3.3 Eve normalization (agent-specific rewriting)

Before writing a skill for `eve`, the frontmatter is rebuilt from a whitelist
(`src/installer.ts:507-538`):

- Keeps `name` (string), `description` (string), `license` (string), `compatibility` (any),
  `version` (string|number), `metadata` (mapping).
- **All other keys are silently dropped**, key order becomes the code's order, and everything is
  re-serialized (comments, quoting style, anchors lost). Body is kept, minus one leading blank line.
- If no whitelisted key survives, the frontmatter block is removed entirely.
- Applies to both blob/copy installs (`:785-792`, `:806-813`) and `copyDirectory` (`:613-615`).
- The file is renamed `<sanitizeName(installName)>.md` instead of `SKILL.md`.

No other agent rewrites frontmatter. **(observed: grep for frontmatter writers finds only these)**

---

## 4. Name sanitization

### 4.1 `sanitizeName` — folder names

(`src/sanitize.ts:73-87`; re-exported from `src/installer.ts:37`)

```
lowercase
replace /[^a-z0-9._]+/g with "-"      # runs collapse into one hyphen
strip leading and trailing [.-]
truncate to 255 chars
if empty -> "unnamed-skill"
```

Examples (all from `tests/sanitize-name.test.ts`, **(observed)**):

| input | output |
|---|---|
| `MySkill` | `myskill` |
| `my   skill` | `my-skill` |
| `bun.sh`, `v2.0`, `my_skill` | unchanged (dots/underscores kept) |
| `skill@#$name` | `skill-name` |
| `../etc/passwd` | `etc-passwd` |
| `../../secret` | `secret` |
| `C:\Windows\System32` | `c-windows-system32` |
| `.hidden`, `..skill`, `...skill` | `hidden`, `skill`, `skill` |
| `skill..`, `--skill` | `skill`, `skill` |
| `émoji🎉skill` | `moji-skill` (non-ASCII dropped, runs collapsed) |
| `vercel/next.js` | `vercel-next.js` |
| `''`, `...`, `@#$%` | `unnamed-skill` |

Notes for Go:

- Non-ASCII letters are **removed**, not transliterated (`skill日本語` → `skill`).
- `.` survives, so `python3.12` and `docs.example.com` are legal folder names; combined with the
  leading-dot strip, `.` and `..` can never be produced.
- `:` is rewritten (`ce:review` → `ce-review`), which is why removal matching must re-sanitize
  (`src/remove.ts:38-79`).
- Truncation is a plain byte-slice of the already-ASCII string, so no rune-splitting issue.

Where it is applied **(observed)**:

| Site | Use |
|---|---|
| `installSkillForAgent` (`src/installer.ts:325`) | destination folder `.agents/skills/<name>` and agent dir |
| `getInstallPath` / `getCanonicalPath` / `isSkillInstalled` (`:649`, `:683`, `:707`) | path computation |
| `--rename` validation (`src/add.ts:1812-1818`) | value must be a **fixed point**: `sanitizeName(v) === v` |
| `remove` / `list` / `update` / `sync` / `local-lock` | canonical key comparison |
| Eve flat file name (`src/installer.ts:631-633`) | `<name>.md` |
| `use` temp directory (`src/use.ts:137`) | scratch dir |

`--rename` is rejected (exit 1) when not already safe, and the error suggests the sanitized form:

```
--rename must be a skill name: a skill name of lowercase letters, digits, ".", "_" and "-" (e.g. bad-name)
```

Tested `tests/rename.test.ts:223-229`, `src/add.ts:1812-1818`. This is stricter than frontmatter
`name`, which is never checked — an asymmetry a Go port must keep.

### 4.2 Collisions

Four distinct mechanisms, do not conflate them:

1. **Discovery dedupe** — first skill wins for a given exact `name` string; later ones are dropped
   without a warning. Optional `includeDuplicateNames` keeps both (used by update to detect
   ambiguity). (`src/skills.ts:287`, `:329`; test `tests/full-depth-discovery.test.ts:205-232`)
2. **Install overwrite** — destination directory is deleted and recreated before writing, so
   re-installing a different skill with the same sanitized name replaces it silently.
   (`src/installer.ts:197-205`)
3. **Category protection** — if the destination exists, has **no** `SKILL.md`, and has at least one
   child directory with `SKILL.md`, it is an agent category directory (`~/.hermes/skills/<cat>/<skill>`)
   and installing there is refused rather than wiped: `add` skips that target and prints one warning
   per conflict; the installer throws `SKILLS_CATEGORY_DIR_CONFLICT` if reached directly.
   (`src/installer.ts:176-187`; `src/add.ts:351-390`)
4. **Same source, two names** — `--rename` permits installing the same upstream skill as `alpha`
   and again as `beta`; lock keys, folders and lock `skillPath` (both `skills/alpha/SKILL.md`)
   coexist. Tested `tests/rename.test.ts:186-196`.
   Also: installing onto/into the source directory is detected (`pathsOverlap`) and reported as a
   successful no-op so `add ./skills --all` cannot delete the source. (`src/installer.ts:359-370`)

### 4.3 Terminal sanitization

Two functions, deliberately different. (`src/sanitize.ts:44-65`)

`stripTerminalEscapes`, applied in this exact order (order matters: OSC/DCS before CSI, else the
inner `ESC [` of an OSC payload would be eaten first and leave the payload's tail):

1. `OSC` `ESC ] … BEL | ESC \`
2. `DCS/PM/APC` `ESC P|^|_ … ESC \`
3. `CSI` `ESC [ params intermed final`
4. simple escapes `ESC` + any byte 0x20–0x7E (covers `ESC 7`, `ESC c`, `ESC M`)
5. C1 codes 0x80–0x9F (8-bit equivalents)
6. remaining control chars `0x00-0x08, 0x0B-0x1A, 0x1C-0x1F, 0x7F` — **tab and newline are kept**

`sanitizeMetadata = stripTerminalEscapes → collapse [\r\n]+ to " " → trim`.

Why: CWE-150. Untrusted `name`/`description`/remote-API strings must not be able to clear the
screen, move the cursor, retitle the window, or print fake "✓ Verified Safe" output. The worked
PoC and its expected result are in `tests/sanitize-terminal.test.ts:115-149` and `:166-176`.

Applied to: frontmatter `name`/`description` at parse time (`src/skills.ts:103-104`), paths and
skip reasons in warnings (`:60-62`), and display strings throughout (`update`, `list`, `check`).
Note `sanitizeMetadata` is **not** applied to `rawContent`, so hashing is unaffected.

---

## 5. Discovery on disk

Entry point: `discoverSkills(basePath, subpath?, options?)` (`src/skills.ts:189-340`).
`basePath` is a cloned/downloaded repo or a local path; `subpath` is a user-supplied subtree.

### 5.1 Preconditions

- `subpath` is validated with `isSubpathSafe`: `resolve(base/sub) === base` or starts with
  `base + sep`; otherwise throws `SKILLS_SUBPATH_OUTSIDE_REPO`. (`:169-174`, `:202-204`)
  Source-level parsing already rejects `..` (and backslash variants) earlier
  (`src/source-parser.ts` + `tests/subpath-traversal.test.ts:14-65`).
- `searchPath = subpath ? join(basePath, subpath) : basePath`.
- Destination: every discovered skill carries `path = dirname(SKILL.md)` (absolute).

### 5.2 Container list, in walk order

| # | Directory (relative to `searchPath`) | Walk depth |
|---|---|---|
| 0 | `searchPath` itself (root) | **1** |
| 1 | `skills/` | 3 |
| 2 | `skills/.curated/` | 3 |
| 3 | `skills/.experimental/` | 3 |
| 4 | `skills/.system/` | 3 |
| 5… | every `AGENT_PROJECT_SKILL_DIRS` entry (sorted, deduped, `skills` excluded) | 3 |
| … | each `skills.sh.json` `paths` entry | 3 |
| …last | each plugin-manifest-derived directory | **1** |

(`src/skills.ts:261-282`, `src/constants.ts:6`)

`AGENT_PROJECT_SKILL_DIRS` = the union of every agent's `skillsDir` plus legacy dirs
`.cline/skills`, `.codex/skills`, `.factory/skills`, `.github/skills`, `.kilo/skills`,
`.kilocode/skills`, `.opencode/skills`, `.pi/skills`; `skills` is filtered out and the result sorted.
(`src/agents.ts:838-857`) The effective list for v1.9.0 is the one printed in
`docs/creating-skills.md:60-118`.

The "deep" set is computed as `prioritySearchDirs.slice(1)` **before** plugin paths are appended,
which is why plugin-declared dirs stay at depth 1 (`src/skills.ts:273-282`).

### 5.3 Root `SKILL.md`

Handled before any container walk (`:243-258`):

1. If `<searchPath>/SKILL.md` exists, parse it.
2. If it parsed, push it, record its name, and **return immediately** unless `fullDepth`.
3. If it exists but failed to parse (bad frontmatter, internal-and-gated), the function continues
   scanning — the scan is not short-circuited.
4. The code intends to skip a root skill that is merely an installed copy tracked by a lock, but the
   check cannot match at the root (see §12).

Consequence: a repo with a valid root `SKILL.md` exposes exactly one skill, even if `skills/` is
full of them, unless `--full-depth`. Tested `tests/full-depth-discovery.test.ts:26-56,103-134`.

### 5.4 Walking a container (`walkSkillDirs`)

(`src/skills.ts:295-314`)

```
for entry in readdir(dir):           # only entry.isDirectory()
    child = dir/entry
    found = tryAddSkillAt(child)
    if found or depth >= maxDepth or entry.name in SKIP_DIRS: continue
    recurse(child, depth+1)

tryAddSkillAt(d):
    if not hasSkillMd(d): return false
    skill = parseSkillAt(d)          # deduped by resolved SKILL.md path
    if skill is null (parse/internal/dup-path): return true    # <-- claims the dir
    if name already seen (and !includeDuplicateNames): return true
    if isInstalledProjectSkill(skill): return true
    push; return true
```

Semantics:

- `depth` starts at 1 for the container's **children**, so maxDepth 1 = "children only",
  maxDepth 3 = `skills/<a>/<b>/<name>/SKILL.md` (three directory levels below `skills/`).
- **Shadowing**: finding a `SKILL.md` at a level stops descent below it, even when the skill was not
  added (duplicate name, unparsable, internal). `skills/foo/SKILL.md` hides `skills/foo/inner/…`.
  Tested `tests/nested-container-discovery.test.ts:68-76`, `:218-228`.
- `SKIP_DIRS = node_modules, .git, dist, build, __pycache__` are never descended into, and are also
  never treated as skills (`src/skills.ts:14`, `:307`). Checked at every level, including depth 2
  (`tests/nested-container-discovery.test.ts:78-85`).
- Entries are iterated in `readdir` order — **not sorted** — and children are visited before the
  next sibling. Which duplicate wins inside one directory is therefore filesystem-dependent.
  **(observed)** A deterministic Go port should sort, but that changes duplicate resolution.
- Unreadable/missing directories are swallowed (`catch {}`), so a bad `skills.sh.json` path simply
  contributes nothing.

Because the root is walked at depth 1, **any immediate child of the repo root containing
`SKILL.md` is a skill** — including `examples/foo/SKILL.md`. Only depth ≥ 2 below the root is
invisible (that is what `tests/nested-container-discovery.test.ts:147-155` actually asserts).
**(observed; the doc comment at `src/skills.ts:274-278` is misleading on this point)**

### 5.5 `--full-depth`

(`src/skills.ts:158-159`, `:252-255`, `:321-337`; CLI `src/cli.ts:179`, `src/add.ts:3658`)

1. Root skill no longer short-circuits.
2. Container walks are unchanged (still depth 3).
3. A **recursive fallback** runs unconditionally: `findSkillDirs(searchPath)`, depth-first from
   `searchPath` (depth 0) to `maxDepth = 5`, skipping the same `SKIP_DIRS`, adding the current dir
   first then children in `readdir` order. (`src/skills.ts:131-153`) This reaches
   `skills/l1/l2/l3/deep-skill` (tested `tests/nested-container-discovery.test.ts:157-169`).
4. Duplicates are still removed by name, so container/root copies **outrank** anything only the
   fallback finds. `includeDuplicateNames` disables that.
5. Blob fast path is skipped entirely when `fullDepth` is set (`src/add.ts:1689-1690`), and `use`
   likewise.
6. When an explicitly requested skill name is not found, `add` silently retries discovery once with
   `fullDepth: true` (`src/add.ts:1660-1671`).

### 5.6 Fallback without `--full-depth`

If the container phase produced **zero** skills, the same recursive `findSkillDirs` walk runs once
(`src/skills.ts:321-323`). This is why a repo with no `skills/` directory at all still works, and why
several plugin-manifest tests pass even when the manifest path is rejected (§9).

### 5.7 Deduplication and precedence

- Key = `skill.name` after `sanitizeMetadata`, compared **exactly** (case-sensitive). `Foo` and `foo`
  are two distinct skills. Slug normalization is used only for lock/name matching, not for dedupe.
  (`src/skills.ts:16-18`, `:251`, `:291`)
- Order of precedence for a duplicated name:
  `root SKILL.md` → container #1 … #n in the table above (each in `readdir` order) →
  skills.sh.json containers → plugin dirs → fallback tree.
- Duplicate drops are silent. The ambiguity is surfaced later by callers that opt into
  `includeDuplicateNames` (`src/update.ts:890-909`), or by `filterSkills` returning several matches.
- `filterSkills` (`src/skills.ts:349-363`) first matches display name case-insensitively; only if
  that yields nothing does it retry with `normalizeSkillName` (lowercase, whitespace/`_` → `-`).
  It returns matches in discovery order.

### 5.8 Leaving out installed copies

`isInstalledProjectSkill` (`src/skills.ts:221-233`) prevents a *consumer* repo from re-listing the
skills it installed:

1. Read `<basePath>/skills-lock.json`; the set of lock keys, normalized by
   `lowercase + spaces/underscores → '-'`.
2. The skill's directory (relative to `basePath`) must be inside one of
   `AGENT_PROJECT_SKILL_DIRS`.
3. Its normalized frontmatter name **or** normalized folder basename must be a lock key.

Only `.agents/skills`, `.claude/skills`, … are protected; the root `skills/` container is not
(because `skills` is filtered out of `AGENT_PROJECT_SKILL_DIRS`). Tested
`tests/nested-container-discovery.test.ts:113-145`.

Lock reading (`src/skills.ts:176-187`): missing file → empty set; invalid lock
(`SKILLS_INVALID_LOCAL_LOCK`: bad JSON, merge markers, missing `skills` map, unknown version) is
swallowed → empty set; **any other error is rethrown**, and `resolveSkills` rethrows upward, so an
unreadable source lock can abort the whole `add` (`src/add.ts:1756-1759`). **(observed)**

---

## 6. Discovery on the blob fast path (no clone)

For allowlisted GitHub repos, `findSkillMdPaths(tree, subpath?, extraContainers?)` mirrors the disk
walk over a Git tree listing (`src/blob.ts:354-469`). Differences that must be reproduced or
deliberately unified:

| Aspect | Disk | Blob |
|---|---|---|
| `SKILL.md` match | exact name | **case-insensitive** (`skill.md` accepted) |
| depth counting | dirs below container: 1–3 | path segments after container: ≤ 3 dirs + file |
| root prefix depth | 1 | 1 (direct `SKILL.md` in root added) |
| extra containers | depth 3, container itself never a skill | same (`:417-424`) |
| fallback | dir recursion, 5 levels | all `SKILL.md` with ≤ 6 path segments |
| ancestor shadowing | stop descending | `hasAncestorSkill` over the tree, case-insensitive |
| installed-copy skip | yes | **no** (no `skills-lock.json` handling) |
| duplicate names | dropped | **not dropped** — all parsed entries become skills |
| root skill payload | whole directory | `SKILL.md` only |
| frontmatter failure | warning + skip | silent skip |

(`src/blob.ts:370-469`, `:688-709`, `:739-768`)

Blob parse also requires string `name` + `description`, applies `sanitizeMetadata`, and honours
`includeInternal` (`:691-699`). If any snapshot download fails, or a snapshot does not match the tree,
the whole blob attempt returns `null` and the caller clones instead — never a partial install
(`:735-737`, `:770-775`).

Named-skills prefilter: `owner/repo@skill` narrows by folder-name slug first, then by frontmatter
name slug, then gives up and falls back to the clone path (`:654-668`, `:713-724`).

---

## 7. `skills.sh.json` — repo-declared containers

File: `<basePath>/skills.sh.json` (v1 also documents `$schema`, ignored). Only `paths` is read.
(`src/skills-sh-config.ts:8`, `:20-61`; `docs/creating-skills.md:140-153`)

```json
{
  "$schema": "https://skills.sh/schemas/skills.sh.schema.json",
  "paths": ["./tools/skills", "./more-skills/"]
}
```

Validation rules for each entry — entry is **dropped**, not fatal, when any fails:

1. must be a string;
2. must start with `./`;
3. must not contain `\` (rejected rather than reinterpreted, to defeat Windows traversal);
4. `posix.normalize` then strip trailing `/`;
5. must not normalize to `.`, `''`, `..`, must not start with `../`, must not be absolute;
6. exact duplicates removed, **order preserved**.

Invalid JSON, a non-object root, a non-array `paths`, or a missing field → `[]` (behaves as if the
file did not exist). Tested `tests/skills-sh-paths.test.ts:50-83`.

Downstream behavior:

- Each surviving path becomes a **container** with depth-3 walk, appended after the built-in dirs.
  (`src/skills.ts:271`, `:279`, `:316-319`)
- The container directory itself is never a skill: `<path>/SKILL.md` is ignored, only its children
  are searched. Tested `:138-146`; blob mirror at `src/blob.ts:417-424`.
- Entries that escape the repo are silently ignored at discovery time even though
  `parseSkillsShPaths` would normally have dropped them (defense in depth).
- Declared paths participate in name dedupe, with built-in locations winning (tested `:127-136`).
- Blob path: config is fetched from the tree only when the tree lists it, and parsed with the same
  function (`src/blob.ts:498-509`).

---

## 8. Plugin manifest discovery

Two Claude Code files, both read from `<searchPath>/.claude-plugin/`, independently (a repo may have
both). (`src/plugin-manifest.ts:51-112`)

### 8.1 `marketplace.json`

```json
{
  "metadata": { "pluginRoot": "./plugins" },
  "plugins": [
    { "name": "my-plugin", "source": "./my-plugin",
      "skills": ["./skills/review", "./custom/x"] },
    { "name": "remote", "source": { "source": "github", "repo": "o/r" } }
  ]
}
```

Rules **(observed)**:

- `metadata.pluginRoot` is optional. If present it must start with `./`; otherwise the **entire
  marketplace block is discarded** (not just that plugin).
- `plugins[]` is optional (`?? []`).
- `plugins[].source`:
  - absent → `pluginBase = basePath/pluginRoot`;
  - string → must start with `./`, else this plugin is skipped;
  - object (remote/`{source, repo}`) → plugin skipped, no network fetch attempt ever.
- `pluginBase = join(basePath, pluginRoot ?? '', source ?? '')`, then containment-checked against
  `basePath` (`..` or absolute escapes are dropped).
- `plugins[].name` is only used for grouping; not required for discovery.

### 8.2 `plugin.json`

```json
{ "name": "single-plugin", "skills": ["./skills/a", "./skills/b"] }
```

- `skills[]` entries are relative to `basePath`, must start with `./`, containment-checked.
- `name` only feeds grouping. A missing/empty `skills` array is fine — the conventional dir is
  still searched.

### 8.3 What the manifest contributes to the search list

For every accepted plugin (`addPluginSkillPaths`, `src/plugin-manifest.ts:56-75`):

1. for each `skills[]` entry: push `dirname(join(pluginBase, entry))` — the **parent** of the
   declared skill directory, contained-checked;
2. **always** push `join(pluginBase, 'skills')`.

Those directories are appended last and walked at **depth 1** (`src/skills.ts:282`, `:316-319`).

Important consequence: declaring `./custom-skills/x` makes the walker scan *all* of
`pluginBase/custom-skills/`, not just `x`. Declaring `./skills/review` mostly duplicates the
conventional `skills/` push. Because the declared entry's parent is computed, arbitrarily deep
declared paths work (`./a/b/c/skill` → parent `a/b/c` → depth-1 finds `skill`), which is what
`docs/creating-skills.md:138` means by "searched at their declared depth". **(observed)**

### 8.4 Grouping (display only)

`getPluginGroupings` returns `Map<absoluteSkillDir, pluginName>` (`src/plugin-manifest.ts:120-183`):

- marketplace: key = `resolve(join(pluginBase, entry))` — the **exact** declared skill directory;
- plugin.json: key = `resolve(join(basePath, entry))`;
- entries with no `./`, remote sources, non-contained paths, and plugins without `name` are skipped;
- `discoverSkills` attaches `skill.pluginName` only on exact path equality
  (`src/skills.ts:210-219`).

It never changes install paths or names. It only groups the interactive picker (`src/add.ts:2823-2843`)
and `list` output (`src/list.ts:242-243`), and is persisted in the lock (`src/skill-lock.ts:42`).

### 8.5 Broken manifests and precedence

- Missing file / invalid JSON / wrong shape → that block is silently skipped (`catch {}` at
  `src/plugin-manifest.ts:98-100`, `:107-109`, `:161-163`, `:178-180`). Tested
  `tests/plugin-manifest-discovery.test.ts:132-144`.
- A non-array `plugins` value is not validated; `for…of` over an object throws and the block is
  dropped by the catch, while a string would be iterated character-wise and simply contribute
  `pluginBase/skills`. **(assumed: not exercised by tests)**
- Malicious `source`/`skills`/`pluginRoot` values are dropped by the `./` + containment rules
  (tests `:380-451`, `:453-533`, `:535-585`).
- Precedence: standard containers and `skills.sh.json` are walked **before** manifest dirs, so on a
  name collision the standard copy wins. Skills reachable only through a manifest are added
  afterwards. Tests `:146-166`, `:337-378`.
- No manifest → no plugin dirs, and if nothing standard is found the recursive fallback still runs,
  so manifest-specific layouts often "work by accident".

---

## 9. Internal skills: `INSTALL_INTERNAL_SKILLS`

| Surface | Behavior |
|---|---|
| `add <src>` (picker / `--all` / `*`) | internal skills hidden from parse entirely |
| `add <src> --skill name` | `includeInternal = true` → internal skill visible and installable |
| `add <src> --skill '*'` | `*` is bulk, not explicit → hidden |
| `use <src>` (no `--skill`) | hidden; cannot be selected |
| `use <src> --skill name` | `includeInternal = selector !== undefined` → visible |
| `list`, `check`, `update`, `remove`, `sync` | no frontmatter gating; they operate on locks/paths |

Evidence: `src/skills.ts:32-35,113-120`; `src/add.ts:2671-2679` (and the well-known mirror at
`:978-981`); `src/use.ts:192-193`; blob `src/blob.ts:695-696`.

Env gating is `process.env.INSTALL_INTERNAL_SKILLS === '1' || === 'true'` — nothing else, including
`'TRUE'`, `'yes'`, `'0'`. Tested `src/add.test.ts:1200-1235`. **(observed)**

Because an internal skill's directory still returns `true` from `tryAddSkillAt`, a gated-off internal
skill **shadows** any nested skill beneath it (`src/skills.ts:284-293`). **(observed)**

`docs/creating-skills.md:36-47` documents `true`, `"true"` and `"1"`; it does not document that a
wildcard install skips them.

---

## 10. Relocation of an already-installed skill

There is no `rename` command. "Moving" a skill happens three ways:

### 10.1 Rename at install time (`add --rename <new>`)

- Applies to exactly one selected skill; more → error `--rename installs one skill` and **no lock is
  written** (tested `tests/rename.test.ts:215-221`).
- `validateRename` requires `sanitizeName(new) === new` (already-safe string), non-empty
  (`src/add.ts:1812-1818`).
- `renameSkill` sets `name = new`, `renamedFrom = upstream name` (unless renaming back to it, which
  clears `renamedFrom`), and for well-known sources also `wellKnown.installName`
  (`src/add.ts:1786-1796`).
- Results: folder `.agents/skills/<new>`, lock key `<new>`, lock `skillName: <upstream>`,
  lock `skillPath: <upstream SKILL.md>`, `SKILL.md` name rewritten, hash computed post-rewrite
  (tests `:86-116`).
- Both canonical and agent copies are rewritten; symlinks are not (`§3.2`). In symlink mode with
  several agents the single canonical file is rewritten once and all links follow (`:118-140`).
- `update` re-renames on every refresh, keeps `skillName`, and reports the update under the **new**
  name; `list` prints `(renamed from alpha)` / JSON `renamedFrom` (`:142-161`, `:198-204`).
- `remove <new>` works by the new name (`:206-213`).
- Restore from lock (`experimental_install`) reinstalls under the new name and does not rewrite the
  lock (`:163-184`).

### 10.2 Relocation detection during `update`

`resolveSkillLocations(lockedNames, lockSkills, discovered, { exactPathDisambiguates })`
(`src/skill-relocation.ts:32-87`) decides, per lock entry that has a `skillPath`:

```
normName = lowercase, [\s_]+ -> "-"
normPath = "\" -> "/", collapse "//"

candidates = discovered paths whose normName == normName(lock.skillName ?? key)
lockedPathExists = normPath(lock.skillPath) is among discovered paths

1. if lockedPathExists and exactPathDisambiguates -> resolved[key] = lockedPath
2. if candidates.length > 1                       -> ambiguous[key]   (no migration, no delete)
3. if lockedPathExists                            -> resolved[key] = lockedPath
4. if candidates.length == 1                      -> resolved[key] = that path   (relocation)
5. else                                           -> deleted[key]
```

- Ambiguity **fails closed**: the skill is neither migrated nor offered for deletion, so a human
  decides. The same-name-elsewhere case is ambiguous even when one path matches exactly, unless the
  caller vouches that path-targeted reinstall is possible. Tested
  `src/skill-relocation.test.ts:72-136`.
- `exactPathDisambiguates` is true for path-addressable sources (per-entry directory reinstall) and
  false for generic git sources that reinstall from the whole repo
  (`src/update.ts:899-910`, `:1239-1247`).
- Renamed skills are found by `lockSkills[key].skillName`, not by the installed key (tested
  `:156-165`).
- Lock entries **without** `skillPath` are skipped entirely — they are the "legacy" entries `update`
  refuses to touch and instead prints a reinstall hint for (`src/update.ts:1353-1369`).
- Only `update` uses this resolver; `add` and `experimental_install` work from explicit input.

### 10.3 Manual movement on disk

`remove`/`list` canonicalize both folder names and lock keys through `sanitizeName`, preferring the
lock key, so a folder renamed by hand (e.g. `ce:review` → `ce-review`) still resolves to the right
lock entry (`src/remove.ts:38-79`; `src/list.ts:117-127`). `findUntrackedSkills` reports installed
folders with no lock key and `remove --all` leaves them alone (`:81-88`).

---

## 11. Full discovery pseudocode (disk)

```
discoverSkills(base, subpath, opts):
  if subpath and not within(base, subpath): throw SUBPATH_OUTSIDE_REPO
  root = subpath ? base/subpath : base
  locked = normalized names from root/skills-lock.json   # missing/invalid -> {}
  groupings = plugin groupings for root
  skills = []; seen = {}; visitedPaths = {}

  if exists(root/SKILL.md):
      s = parse(root/SKILL.md)
      if s and not isInstalledCopy(s):
          add(s); if not opts.fullDepth: return skills

  containers = [root, root/skills, root/skills/.curated, root/skills/.experimental,
                root/skills/.system, ...agentDirs(root)]
  deep = containers[1:]
  containers += skillsShDirs(root)
  deep = deep ∪ skillsShDirs(root)
  containers += pluginDirs(root)          # depth 1

  for dir in containers:
      walk(dir, maxDepth = dir in deep ? 3 : 1)

  if skills empty or opts.fullDepth:
      for dir in recursiveDirs(root, maxDepth=5, skip=SKIP_DIRS):
          s = parse(dir/SKILL.md)
          if s and (opts.includeDuplicateNames or s.name not in seen)
             and not isInstalledCopy(s): add(s)

  return skills                       # order = discovery order, deduped by exact name
```

---

## 12. Surprising / undocumented

1. **Root `skills-lock.json` skip cannot fire.** The comment at `src/skills.ts:243-244` says a root
   `SKILL.md` that is an installed project skill is ignored, but `isInstalledProjectSkill` computes
   `relative(basePath, skill.path) === ''`, which never matches an `AGENT_PROJECT_SKILL_DIRS` entry
   (`:221-233`). A root skill in a consumer repo is always listed. **(observed)**
2. **`skills/` is never protected as an installed location.** Same function only protects agent
   dirs; `skills` is explicitly filtered out of `AGENT_PROJECT_SKILL_DIRS`
   (`src/agents.ts:850-857`). A repo that installs into `skills/` and commits a lock will re-list
   its own copies as source skills.
3. **Root container depth 1 finds more than intended.** `examples/foo/SKILL.md` *is* discovered; only
   two-level nesting under the root is hidden. Comments and docs say otherwise.
4. **Silent shadowing by unusable skills.** A `SKILL.md` that is malformed, internal-and-gated, or a
   duplicate name still stops the descent, hiding everything nested under it.
5. **`readdir` order decides duplicate winners.** No sorting anywhere in the walk. Reimplementing in
   Go with sorted output will change which of two same-named skills is kept.
6. **Blob and clone paths disagree.** Different `SKILL.md` casing rules, different depth arithmetic,
   blob does not dedupe names, blob does not honour `skills-lock.json`, and a root skill installs
   only `SKILL.md` from blob but the whole directory from clone (`src/blob.ts:376-378`, `:465-468`,
   `:739-768`).
7. **Manifest `skills[]` entries widen discovery.** A declared path adds its *parent* directory to
   the search list, so undeclared siblings in that directory are discovered too.
   (`src/plugin-manifest.ts:62-70`)
8. **The documented `pluginRoot` example does not validate.** `docs/creating-skills.md:126-136`
   shows `"pluginRoot": "./plugins"` with `"source": "my-plugin"`, but `source` must start with
   `./` (`src/plugin-manifest.ts:92`) — that plugin is skipped. The doc's example only appears to
   work because the recursive fallback later finds the skill.
9. **Frontmatter `name` is unvalidated while `--rename` is strict.** Any non-empty string is a legal
   skill name; the folder is sanitized and can collapse two distinct skills into one directory.
10. **`metadata.internal` is string-sensitive.** `"TRUE"`, `1`, `yes` are not internal; `"1"` is.
11. **A malformed `skills-lock.json` in the *source* repo aborts `add`.** Invalid-lock diagnostics
    are swallowed, but any other read error is rethrown through `discoverSkills` into `runAdd`.
12. **Eve install is lossy by design.** Unknown frontmatter keys disappear, `SKILL.md` becomes
    `<name>.md`, and the file is rewritten even when no rename happened.
13. **`.claude-plugin/` is not in `SKIP_DIRS`**, so a stray `SKILL.md` inside it is reachable by the
    fallback walk.
14. **A `skills.sh.json` container is not itself a skill**, even though a root `skills/SKILL.md`
    would be (`skills` is a built-in container and the root prefix accepts a direct `SKILL.md`).
15. **`--full-depth` does not deepen container walks.** It only removes the root short-circuit and
    adds the 5-level recursive fallback; catalog depths stay at 3.

---

## Open questions

1. Does `yaml@2` actually throw on multi-document frontmatter, and on duplicate keys, in the way
   `parseFrontmatter` assumes? The code has no `parseAllDocuments` fallback and no duplicate-key
   option. Not executed here. (`src/frontmatter.ts:14`)
2. What exactly happens for `plugins` being a non-array (string/object) in `marketplace.json`?
   `for…of` semantics differ per type and the `catch {}` hides it. (`src/plugin-manifest.ts:87`,
   `:98-100`)
3. Is the 255-char truncation in `sanitizeName` ever hit *before* path construction, and does a
   truncated name still satisfy `--rename`'s fixed-point rule while colliding with another skill?
   (`src/sanitize.ts:87`, `src/add.ts:1813`)
4. Should the Go port preserve `readdir` order (bug-compatible) or sort (deterministic)? The
   duplicate-winner behavior differs. (`src/skills.ts:297-309`)
5. Is `isInstalledProjectSkill`'s root-path gap (§12.1) intentional? The code and the comment
   disagree, and no test covers a root `SKILL.md` with a matching lock entry. (`src/skills.ts:243-256`)
6. Does any consumer rely on blob-vs-clone dedupe differences (§6)? `tests/blob-*` cover payload and
   hashes, not duplicate names.
7. `stripTerminalEscapes` keeps `\t`/`\n`; is any output path printing sanitized-but-not-collapsed
   strings where a newline could still forge a second line? `sanitizeMetadata` is used for
   name/description, but `stripTerminalEscapes` alone is used for the skip *reason*
   (`src/skills.ts:60-62`).
8. Whether `skills.sh.json` `paths` entries that are files (not dirs) are expected to be ignored
   silently — the walk swallows `readdir` failures, so yes in practice, but it is untested.

---

## Evidence index

**Frontmatter** — `src/frontmatter.ts:8-16` (parse regex), `:24-45` (rewrite);
`src/frontmatter.test.ts:9-33` (LF/CRLF/BOM/JS/no-frontmatter), `:36-54` (BOM via `parseSkillMd`),
`:57-94` (rewrite preservation, quoting, block scalar, prepend).

**Parse / validate** — `src/skills.ts:24-26` (`isRecord`), `:32-35` (env gate), `:41-45`
(`isInternalSkill`), `:47-55` (`hasSkillMd`), `:57-64` (skip warning), `:66-129` (`parseSkillMd`),
`:103-111` (sanitize + empty checks), `:113-120` (internal gate), `:122-128` (Skill shape).

**Sanitize** — `src/sanitize.ts:19-36` (regexes), `:44-52` (strip order), `:61-65`
(`sanitizeMetadata`), `:73-87` (`sanitizeName`); `tests/sanitize-name.test.ts:14-137`;
`tests/sanitize-terminal.test.ts:15-190`.

**Discovery** — `src/skills.ts:14` (`SKIP_DIRS`), `:16-18` (name normalization), `:131-153`
(recursive fallback, depth 5), `:169-174` (`isSubpathSafe`), `:176-187` (source lock),
`:189-340` (`discoverSkills`), `:221-233` (installed-copy skip), `:243-258` (root), `:261-282`
(containers, deep set, plugin dirs), `:284-314` (walk/shadow), `:316-337` (loop + fallback),
`:342-363` (`getSkillDisplayName`, `filterSkills`); `src/constants.ts:1-6`;
`src/agents.ts:838-857`; `tests/nested-container-discovery.test.ts:49-179`;
`tests/full-depth-discovery.test.ts:26-232`.

**Blob mirror** — `src/blob.ts:354-361` (priority prefixes), `:370-469` (`findSkillMdPaths`),
`:498-509` (skills.sh.json), `:688-709` (parse + internal), `:735-778` (snapshots, root trim).

**skills.sh.json** — `src/skills-sh-config.ts:8`, `:20-46`, `:53-61`;
`tests/skills-sh-paths.test.ts:50-83`, `:86-168`, `:170-200`, `:202-279`.

**Plugin manifests** — `src/plugin-manifest.ts:8-20` (containment + `./` rule), `:51-112`
(skill dirs), `:120-183` (groupings); `tests/plugin-manifest-discovery.test.ts:22-585`;
`tests/plugin-grouping.test.ts:1-72`.

**Install / rewrite** — `src/installer.ts:37` (re-export), `:176-187` (category guard), `:197-205`
(clean+create), `:323-357` (sanitized path + traversal guard), `:359-370` (source overlap),
`:496-505` (`setInstalledSkillName`), `:507-538` (Eve frontmatter), `:631-641` (Eve flat file),
`:785-813` (Eve writes), `:562-625` (copy semantics).

**Rename / relocation** — `src/add.ts:1786-1826` (`renameSkill`, `validateRename`, `applyRename`),
`:1907-1911` and `:1364-1368` (rewrite call sites), `:2431-2449` (post-rename hash), `:2671-2679`
(internal gating), `:3658`, `:3680`, `:3686-3690` (CLI flag); `src/skill-relocation.ts:32-87`;
`src/skill-relocation.test.ts:8-166`; `src/update.ts:390-400`, `:890-919`, `:1230-1250`,
`:1353-1369`; `tests/rename.test.ts:86-229`.

**Removal/list canonicalization** — `src/remove.ts:38-88`; `src/list.ts:117-127`;
`src/local-lock.ts:23`, `:99-109`, `:122-157`.

**Docs cross-check** — `docs/creating-skills.md:29-58` (required/optional fields, internal,
discovery + depth 3 + `--full-depth`), `:60-118` (container list), `:120-138` (plugin manifests),
`:140-153` (`skills.sh.json`, first-found wins, recursive fallback).
