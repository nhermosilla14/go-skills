# 03 — Lock files and content hashing

Behavioral specification of the two lock files `skills` owns and of the content hash recorded in
the project lock, written so the TypeScript CLI can be reimplemented in Go byte-for-byte.

Source: `vercel-labs/skills` v1.9.0. Every claim is tagged **observed** (read from the code/tests in
this checkout) or **assumed** (inferred, not verified). Evidence references are `file:line`.

> Naming trap: the project lock is `skills-lock.json` (hyphen after `skills`), the global lock is
> `.skill-lock.json` (dot prefix, hyphen after `skill`). They are different files with different
> schemas and different versions. Most of this document is about the project lock because that is
> where the content hash lives.

---

## 1. Files owned, and their paths

| Lock | Constant | Project scope | Global scope |
| --- | --- | --- | --- |
| Project lock | `skills-lock.json` (`src/local-lock.ts:23`) | `<cwd>/skills-lock.json` | n/a |
| Global lock | `.skill-lock.json` (`src/skill-lock.ts:7`) | n/a | `$XDG_STATE_HOME/skills/.skill-lock.json`, else `~/.agents/.skill-lock.json` (`src/skill-lock.ts:76-82`) |
| Mutex (side file) | `skills-lock.json.lock` (`src/local-lock.ts:293`) | `<abs project lock>.lock` | none — the global lock has no mutex |
| Temp file | `<lock>.<pid>.<12 hex>.tmp` (`src/local-lock.ts:370`) | same directory as the project lock | none |
| Stale-lock claim | `<lock>.lock.<pid>.<12 hex>.stale` (`src/local-lock.ts:277`) | same directory | none |

Path construction is plain `path.join` with no per-OS branch (**observed**):

- Project lock: `join(cwd || process.cwd(), 'skills-lock.json')` — `src/local-lock.ts:102-104`.
  POSIX: `\tmp\...` → `/tmp/proj/skills-lock.json`; Windows: `C:\proj\skills-lock.json`;
  macOS: `/Users/x/proj/skills-lock.json`.
- Global lock: `$XDG_STATE_HOME` is honored on **every** OS when set (no `process.platform` check),
  otherwise `join(homedir(), '.agents', '.skill-lock.json')` — `src/skill-lock.ts:76-82`.
  On Windows `homedir()` is the `USERPROFILE` value, so the fallback is
  `%USERPROFILE%\.agents\.skill-lock.json`. Test helper always sets
  `XDG_STATE_HOME=$HOME/.local/state`, which is why most tests read
  `$HOME/.local/state/skills/.skill-lock.json` (`src/test-utils.ts:82`,
  `tests/remove-global-lockfile.test.ts:51`, `tests/lock-commit-pin.test.ts:106`).
- The installed skill folder that the hash covers is always
  `<cwd>/.agents/skills/<sanitizeName(name)>` for project scope
  (`src/local-lock.ts:107-109`, `src/installer.ts:116-119`, `:703-719`) and
  `~/.agents/skills/<sanitizeName(name)>` for global scope. `sanitizeName` lowercases, replaces
  runs of non-`[a-z0-9._]` with `-`, strips leading/trailing `.`/`-`, truncates at 255 bytes, and
  falls back to `unnamed-skill` (`src/sanitize.ts:73-88`).

### Discovery / search order

- **No upward directory walk for either lock** (**observed**). Every caller passes an explicit
  `cwd` (defaulting to `process.cwd()`), and `getLocalLockPath` joins it directly. A repo-wide
  search for parent traversal only finds the `node_modules` resolver in `src/sync.ts:177-183`,
  which is unrelated to lock files.
- `skills update`/`check` choose a *scope* (not a lock file) heuristically when neither `-g` nor
  `-p` is given: "project" if `<cwd>/skills-lock.json` exists **or** `<cwd>/.agents/skills/*/SKILL.md`
  exists, otherwise "global" (`src/update.ts:181-207`, `:229-231`). This is cwd-only, no walk.
- `skills list` defaults to project, `-g` switches to global (`src/list.ts:85-86`).
- There is no environment variable to relocate the project lock, and there is no per-project
  override of the global lock path (**observed**).

---

## 2. Project lock schema

### 2.1 Envelope

```json
{
  "version": 2,
  "skills": {
    "<installed name>": { "...": "LocalSkillLockEntry" }
  }
}
```

- `version` (number, required): `2` is the current value (`src/local-lock.ts:25`). `1` is accepted
  and upgraded in memory. Anything else **throws** `SKILLS_INVALID_LOCAL_LOCK`
  (`src/local-lock.ts:154-159`).
- `skills` (object, required, key → entry). The key is the *installed* name — the folder name and
  the `name` in the installed `SKILL.md` — not necessarily the upstream name
  (`src/local-lock.ts:56-61`).
- No other top-level key is part of the schema, and **unknown top-level keys are dropped on the
  next write**: the writer rebuilds `{ version, skills }` from scratch (`src/local-lock.ts:358`).
  (**observed**)
- Structural validation is shallow: `typeof value === 'object' && 'version' in value &&
  typeof version === 'number' && 'skills' in value && typeof skills === 'object' && skills !== null`
  (`src/local-lock.ts:249-259`). Individual entries are not validated; a malformed entry is
  carried through and may crash later code (**observed, no defensive parsing**).

### 2.2 Entry fields (project lock)

| Field | Type | Required | When written | Meaning |
| --- | --- | --- | --- | --- |
| `source` | string | yes | every write | Normalized source: `owner/repo` for GitHub shorthand/HTTPS GitHub, the full URL for non-GitHub hosts and SSH (`src/add.ts:124-142`), an absolute-then-portable path for `sourceType: "local"`, the package name for `node_modules` (`src/sync.ts:957`), the URL for downloads (`src/add.ts:1614-1619`) |
| `sourceUrl` | string | no | GitHub/GitLab generic Git sources, downloads, well-known | Original URL, kept so a generic Git source can be re-fetched (`src/add.ts:144-146`, `:1975`, `:1616`) |
| `ref` | string | no | when the install had a ref (`owner/repo@v1.2.3`, branch, tag) | Branch/tag the skill was installed from; written as `ref: parsed.ref` so `undefined` disappears in JSON (`src/add.ts:1976`) |
| `commit` | string | no | Git sources when the commit could be read | Full commit SHA the install came from. Restores fetch this commit; `update` advances it; `update` also back-fills it on older entries (`src/local-lock.ts:41-46`, `src/install.ts:128-132`, `src/update.ts:1271-1273`) |
| `sourceType` | string | yes | every write | One of `github`, `gitlab`, `git`, `local`, `well-known`, `download`, `node_modules` (`src/types.ts:121-129`, `src/sync.ts:958`) |
| `skillPath` | string | no | remote sources and downloads | Path of `SKILL.md` inside the source (`skills/pdf/SKILL.md`) or inside the download/archive (`src/add.ts:1618`, `src/sync.ts:959`). Omitted for `local` |
| `skillName` | string | no | `add --rename` only | Upstream name when installed under a different name; the lock key is the new name (`src/local-lock.ts:57-61`, `src/add.ts:1804-1806`) |
| `version` | string | no | `node_modules` only | Version of the npm package that shipped the skill (`src/sync.ts:960`) |
| `via` | string | no | sync / `installFromSource` with a declarer | Package whose `package.json` `skills` field requested the skill; `"."` is the project (`src/local-lock.ts:63-65`, `src/add.ts:2265`) |
| `computedHash` | string | yes (may be `""`) | every write | SHA-256 of the installed folder, see §4. `""` means "unverifiable, migrated from v1" (`src/local-lock.ts:66-72`) |
| `subagents` | string[] | no | Eve installs into subagents | Eve subagent dirs the skill went to; `""` = root agent (`agent/skills`) (`src/local-lock.ts:73-79`, `src/add.ts:3346-3352`) |
| `wellKnownDigest` | string | no | well-known sources | Provider digest (`sha256:<hex>` fallback computed over sorted `path\0content\0`) (`src/providers/wellknown.ts:846-858`) |
| `installMode` | `"copy"` | no | only when the user chose copy and it is not the default | Makes `update`/restore re-install as copies (`src/local-lock.ts:81-82`, `src/add.ts:3093-3096`) |

Extra/unknown **entry** fields: they survive `update` (which spreads the old entry,
`src/update.ts:1289-1295`) but are **not** preserved by a plain `add`, which writes a freshly built
entry (`src/add.ts:1966-1982`, `src/local-lock.ts:417`). (**observed**)

### 2.3 Example v2 document (project)

Realistic shape after `skills add owner/repo --skill pdf --rename acme-pdf` plus a synced
`node_modules` skill and a local skill (**assembled from the code paths; shape observed, values
illustrative**):

```json
{
  "version": 2,
  "skills": {
    "acme-pdf": {
      "source": "owner/repo",
      "ref": "v1.4.0",
      "commit": "3f9c1c2b8a1e5d4c7b6a9081726354f4e3d2c1b0",
      "sourceType": "github",
      "skillPath": "skills/pdf/SKILL.md",
      "skillName": "pdf",
      "computedHash": "b22c4d22aa319d2924312fa18fcbfedc88fa89c74dbd8f298c683e1caf7b7f24"
    },
    "agent-browser": {
      "source": "agent-browser",
      "sourceType": "node_modules",
      "skillPath": "skills/agent-browser/SKILL.md",
      "version": "0.4.2",
      "via": ".",
      "computedHash": "93c36952d8843b33e68ae970ebabcc03e1687c9bb0dd1ecaf357060318952f55"
    },
    "my-local": {
      "source": "./src/my-local",
      "sourceType": "local",
      "computedHash": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    },
    "site-skill": {
      "source": "https://downloads.example.com/skills/alpha.md",
      "sourceUrl": "https://downloads.example.com/skills/alpha.md",
      "sourceType": "download",
      "skillPath": "SKILL.md",
      "computedHash": "93c36952d8843b33e68ae970ebabcc03e1687c9bb0dd1ecaf357060318952f55"
    }
  }
}
```

Key order inside an entry created by `add` is: `source`, `sourceUrl?`, `ref`, `commit?`,
`sourceType`, `skillPath?`, `computedHash`, then `skillName?`, `subagents?`, `installMode?`
(spread last) — `src/add.ts:1973-1981`, `:3363-3369`. `JSON.stringify` drops `undefined` values,
so absent optionals simply do not appear.

### 2.4 Example v1 document (legacy project lock)

`version: 1` had the same envelope and entry shape minus `commit`, `version`, `via`, `subagents`,
`wellKnownDigest`, `installMode`, and with a **different `computedHash` algorithm** (`src/local-lock.ts:198-228`):

```json
{
  "version": 1,
  "skills": {
    "alpha": {
      "source": "./src-a",
      "sourceType": "local",
      "computedHash": "5f2d…(locale-sorted, unframed hash)"
    },
    "beta": {
      "source": "owner/repo",
      "ref": "main",
      "sourceType": "github",
      "skillPath": "skills/beta/SKILL.md",
      "computedHash": "9c1a…"
    }
  }
}
```

v1 hash definition (**observed**, `src/local-lock.ts:203-228`): regular files only, recursing into
directories but skipping any directory literally named `.git` or `node_modules`, relative paths
`/`-separated, sorted with `localeCompare` (locale-dependent!), then for each file feed
`relativePath` bytes followed immediately by `content` bytes (no separators, no length prefix),
SHA-256, lowercase hex. `metadata.json`, `__pycache__`, and `__pypackages__` were **not** excluded
in v1 (unlike v2).

---

## 3. Global lock schema

### 3.1 Envelope

```json
{
  "version": 3,
  "skills": { "<name>": { "...": "SkillLockEntry" } },
  "dismissed": { "findSkillsPrompt": true },
  "lastSelectedAgents": ["claude-code", "codex"]
}
```

- `version` (number, required): **3** (`src/skill-lock.ts:8`, comment: bumped from 2 for
  folder-hash support).
- `skills` (object, required).
- `dismissed` (object, optional): only `findSkillsPrompt?: boolean` is known
  (`src/skill-lock.ts:52-55`). Created as `{}` by the empty-lock factory (`:259-265`).
- `lastSelectedAgents` (string[], optional): last agent selection, read by restore to pick link
  targets (`src/skill-lock.ts:67-68`, `src/install.ts:33-38`).
- Unknown top-level keys **are preserved** here, because read returns the parsed object and write
  serializes that same object (`src/skill-lock.ts:94-107`, `:118-127`). (**observed** — differs
  from the project lock.)

### 3.2 Entry fields (global lock)

| Field | Type | Meaning |
| --- | --- | --- |
| `source` | string | Normalized source (`owner/repo`, URL, local path) |
| `sourceType` | string | Same vocabulary as the project lock |
| `sourceUrl` | string | Original URL |
| `ref` | string? | Branch/tag |
| `commit` | string? | Commit SHA (`src/skill-lock.ts:22-23`) |
| `skillPath` | string? | `SKILL.md` path in the source |
| `skillName` | string? | Upstream name for renamed installs |
| `skillFolderHash` | string (required in the type) | **Not a content hash of installed bytes.** GitHub sources: the 40-hex Git *tree* SHA of the skill folder, fetched from the GitHub Trees API (`src/skill-lock.ts:31-36`, `:171-181`, `src/add.ts:2341-2346`). Local/download/clone-without-tree sources: the 64-hex `computeSkillFolderHash` value (`src/add.ts:2347-2356`, `:2312`). Well-known: `""` (`src/add.ts:1993`). May also be `""` when no hash could be computed (`src/add.ts:2338`, `:2350`) |
| `installedAt` | string (ISO 8601) | First install; preserved across updates (`src/skill-lock.ts:37-38`, `:197`) |
| `updatedAt` | string (ISO 8601) | Every write via `addSkillToLock` |
| `pluginName` | string? | Plugin the skill belongs to |
| `sourceBaseUrl` | string? | Well-known base URL |
| `wellKnownDigest` | string? | Provider digest |
| `installMode` | `"copy"`? | Reinstall as copies |

There is **no** `computedHash`, no `subagents`, no `version`, and no `via` in the global lock
(**observed**, interface `src/skill-lock.ts:13-47`).

### 3.3 Example v3 document (global)

```json
{
  "version": 3,
  "skills": {
    "alpha": {
      "source": "owner/repo",
      "sourceType": "github",
      "sourceUrl": "https://github.com/owner/repo",
      "ref": "main",
      "commit": "3f9c1c2b8a1e5d4c7b6a9081726354f4e3d2c1b0",
      "skillPath": "skills/alpha/SKILL.md",
      "skillFolderHash": "8e4d0f5a1c2b3d4e5f60718293a4b5c6d7e8f901",
      "installedAt": "2026-01-01T00:00:00.000Z",
      "updatedAt": "2026-02-01T00:00:00.000Z"
    },
    "acme-pdf": {
      "source": "anthropics/skills",
      "sourceType": "github",
      "skillName": "pdf",
      "skillFolderHash": "d41d8cd98f00b204e9800998ecf8427e00000000",
      "installedAt": "2026-01-02T00:00:00.000Z",
      "updatedAt": "2026-01-02T00:00:00.000Z"
    }
  },
  "dismissed": {},
  "lastSelectedAgents": ["claude-code"]
}
```

---

## 4. Content hash (`computedHash`) — implementation-grade

Implemented in `src/skill-hash.ts`. The README publishes the same algorithm under
"Lock File Content Hash" (`README.md:87-112`). Change it only with a lock version bump
(`src/skill-hash.ts:5-9`).

### 4.1 Step 1 — collect files

Input: the skill **folder** (project: `.agents/skills/<sanitizeName>`, or the source folder in the
clone/blob when recording; global: the clone folder).

1. Resolve the folder with `realpath`; the resolved path is the containment root
   (`src/skill-hash.ts:74`).
2. Walk recursively with `readdir(withFileTypes)` (`:80-113`):
   - Skip an entry whose name is **exactly** `metadata.json` (any depth, file or dir).
   - Skip an entry whose name is **exactly** `.git`, `__pycache__`, or `__pypackages__` **when it is
     a directory**. A *file* named `.git` is hashed; a *directory* named `node_modules` is **not**
     excluded and is walked (it is only excluded by the legacy v1 hash). Comparisons are
     case-sensitive. (`src/skill-hash.ts:11-25`, `:94`)
   - For a symlink: resolve it with `realpath`. If it is broken/looping → skip silently. If the
     resolved target is not the root itself and does not start with `root + path.sep` → skip
     (a link pointing outside the skill folder contributes nothing). Otherwise treat it as the
     target type; a symlinked directory still gets the directory exclusion check on its own name
     (`:98-105`).
   - Non-directory, non-regular, non-symlink entries (sockets, FIFOs, devices) are skipped
     (`:103-105`).
   - Symlink cycles inside the folder are broken by an ancestor set of real paths carried down the
     branch; a directory whose real path equals an ancestor is not re-entered (`:87-89`). The set is
     per-branch, so the same in-folder directory reached through two different links is hashed
     twice under two paths.
3. File content is read as raw bytes, no mode, no mtime, no size cap. Empty directories contribute
   nothing. Hard links are hashed per path.

Resulting relative path: `prefix + entry.name`, where `prefix` is `""` for the root and
`parentPath + "/"` below it — so paths are always `/`-separated on every OS (`:96`, `:107`).

### 4.2 Step 2 — normalize line endings (per file)

- Determine "binary" as: the byte buffer contains at least one `0x00` (`:59`).
- If the file contains no `0x00` **and** contains the byte pair `0x0D 0x0A`, replace **every**
  `\r\n` with `\n`. The replacement is done through a `latin1` round-trip, i.e. byte-preserving for
  arbitrary non-UTF-8 text; a lone `\r` is left untouched, and `\r\r\n` becomes `\r\n`
  (`:58-61`).
- Binary files (any NUL) are never modified, so CRLF inside them is significant (test:
  `tests/skill-hash.test.ts:58-64`).
- A UTF-16 text file contains NUL bytes and is therefore treated as binary.

### 4.3 Step 3 — path normalization and sort

- The in-memory API (`hashSkillFiles`) converts every `\` in the supplied path to `/`; a path that
  already uses `/` is unchanged. No other normalization: `./a`, `a//b`, and trailing slashes are
  preserved as given (`src/skill-hash.ts:39`). The folder walk never produces such paths.
- Paths are encoded as UTF-8 and sorted by `Buffer.compare`, i.e. **unsigned byte-wise
  lexicographic order of the UTF-8 encoding** — *not* locale order and *not* UTF-16 order
  (`:44`). Consequence: `B.md` (0x42) sorts before `b.md` (0x62) before `ä.md` (0xC3 0xA4).
- Sort is over paths only; duplicate paths are not deduplicated (callers do not produce them).

### 4.4 Step 4 — framing and digest

One SHA-256 over the concatenation, in sorted order, for each file:

```
UTF8(path) | 0x00 | ASCII(decimal byte length of normalized content) | 0x00 | normalized content
```

- There is **no** separator, terminator, or count between files and no header.
- The length is the decimal ASCII byte length **after** CRLF normalization (`:50`).
- Digest is lowercase hex (`:54`).
- Empty file list → SHA-256 of the empty input.

Byte layout of one record, for clarity:

```
 53 4b 49 4c 4c 2e 6d 64  00  32  00  61 0a
 S  K  I  L  L  .  m  d  NUL '2' NUL 'a' LF
```

### 4.5 Published and additional test vectors

Published in `README.md:100-104`, asserted in `tests/skill-hash.test.ts:15-28`:

| Files | `computedHash` |
| --- | --- |
| (none) | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `SKILL.md` = `# hi\n` | `93c36952d8843b33e68ae970ebabcc03e1687c9bb0dd1ecaf357060318952f55` |
| `SKILL.md` = `a\n`, `docs/b.md` = `b\n` | `b22c4d22aa319d2924312fa18fcbfedc88fa89c74dbd8f298c683e1caf7b7f24` |

The README notes the third equals
`printf 'SKILL.md\0002\000a\ndocs/b.md\0002\000b\n' | sha256sum`. Additional vectors I derived from
the same framing and verified with `sha256sum` (**observed by computation**, useful as Go unit
tests):

| Files | `computedHash` |
| --- | --- |
| `SKILL.md` = `a\n` | `bcefee752f081fde2ae3e8e0659ce73b0a2872fd1727b8ef88df9aaf9b4e2084` |
| `ab` = `c` | `7a4656db1a35d8c77966a2f857d214fd789a65d0f77ea56a4fdae2f8554c305a` |
| `a` = `bc` | `e1cf9898d1e966a32f8611f05aa624936397e7ac6b6ccd73eaa62dab582c747f` |
| `a` = `x`, `b` = `` (empty) | `9c6f927025257a77cd58faaa38615c7d5f8397e9360c59beae2a79709b6c4a23` |
| `a` = `xb` | `87898619c5d0f3bfb6e8917ff8d8477e56456331d07ec0a674183ca9aad95a93` |
| `B.md`=`2`, `b.md`=`1` (byte order) | `473fe1bf717fecac53c699774d475c352247993e66be7d3bb2ec70c17f9cae6e` |
| `b.md`=`1`, `B.md`=`2` (wrong order) | `84299c09634f4e5714729845a6c6a8f6901f39f292336ef00169dabc72be0f56` |

The two framing pairs above prove paths and bodies cannot be shifted into each other, which the
tests also assert without pinning a digest (`tests/skill-hash.test.ts:40-50`).

Other assertions worth porting as tests:

- CRLF and LF text hash identically (`tests/skill-hash.test.ts:52-56`); a real file rewritten with
  CRLF still reports `tracked` (`tests/skill-hash.test.ts:206-207`) and `skills check` exits 0
  (`tests/check.test.ts:74-84`).
- Binary CRLF differs from binary LF (`tests/skill-hash.test.ts:58-64`).
- Windows separators in the in-memory API are accepted (`:66-70`).
- Sort is order-independent input-wise and byte-wise, tested with `B.md`/`b.md`/`ä.md`
  (`:30-38`).
- A filesystem folder hashes exactly like the same files in memory, including `metadata.json`,
  `.git/`, `node_modules/x/index.js`, an in-folder symlink, and an escaping symlink
  (`:90-114`); the installed copy hashes the same as the source folder (`:98-114`).
- Escaping, broken, and cyclic links do not change the hash (`:130-149`).
- Renaming a file changes the hash, and a name change plus an offsetting content change still
  differs (`tests/local-lock.test.ts:573-608`).
- Excluded names (`.git/`, `__pycache__/`, `metadata.json`) do not change the hash
  (`tests/local-lock.test.ts:630-651`).

### 4.6 Empty skills

- Zero files → the all-zero-input digest above. A skill folder with only excluded entries hashes
  like an empty folder.
- The lock does not use the empty-string digest as a sentinel; `""` in `computedHash` means
  "no trustworthy hash", never "empty folder". `""` is only produced by the v1 migration
  (§5), and is treated as "modified" by `getLockedSkillState`
  (`src/local-lock.ts:242-246`).

### 4.7 Renamed skills

The hash is always of the **installed** bytes. `add --rename` rewrites the `name:` line in
`SKILL.md` before/while installing, so the recorded `computedHash` differs from the upstream
folder's hash (README:110). `sourceSkillHash` implements this: take the blob files (or walk the
folder), filter excluded paths, replace the content of the entry whose path is exactly `SKILL.md`
with the frontmatter `name` rewritten to the installed name, then hash
(`src/add.ts:2431-2449`; the rewriter is `src/frontmatter.ts:24-45` — it preserves the file's own
EOL style and appends/prepends a `name:` line if missing). Consequence: restore/`list`/`check`
verify against the installed copy, and the lock key plus `skillName` describe the mapping.

---

## 5. v1 → v2 upgrade semantics (project lock)

Performed inside `readLocalLock` (`src/local-lock.ts:122-181`):

1. Read/parse. `ENOENT` → empty v2 lock (`{version: 2, skills: {}}`). Other read errors propagate.
   Unparseable JSON → `SKILLS_INVALID_LOCAL_LOCK` with the parse message. Wrong shape → same code
   with `expected an object with "version" and "skills"`. Version not `1` and not `2` → same code
   with `unsupported lock version N (expected 2)`.
2. Resolve `sourceType: "local"` sources that are relative against the lock directory
   (`isAbsolute` test, then `resolve`) — done *before* migration, in place (`:161-165`).
3. If `version === 1`, for every entry compute the v2 hash:
   - Skip (→ `""`) when the v1 hash is empty **or** `<install dir>/SKILL.md` does not exist.
   - Compute the legacy v1 hash of the installed folder; if it does not equal the stored v1 hash
     (files edited since), → `""`.
   - Otherwise compute the v2 hash of the installed folder and use it.
   - Any error → `""`. (`:188-196`)
4. Set `version = 2` in memory. If `persistMigration` is true, write the migrated lock
   (`:167-178`).

Persisting is opt-in per call: only `experimental_install`/`install` pass
`persistMigration: !frozen && !dryRun` (`src/install.ts:59-60`). But any *write* triggered later
(`addSkillToLocalLock`, `removeSkillFromLocalLock`, `writeLocalLock`) serializes the whole
in-memory lock, so a plain `add`/`remove`/`update`/`sync` also persists the migrated version and
the migrated hashes. A read-only command (`list`, `check`, `--dry-run`, frozen install) leaves the
file at v1. (**observed**)

Empty hashes at install time:

- Non-frozen `experimental_install`: `getLockedSkillState` → `missing` (no folder) or `modified`
  (empty hash) → the skill is re-fetched, reinstalled, and a new hash recorded
  (`src/install.ts:117-124`, `src/add.ts:2219`, `tests/install-from-lock.test.ts:83-95`).
- Frozen (`--frozen-lockfile`, default in CI): `lockChangeReason` returns
  `<name> has no content hash in skills-lock.json (migrated from version 1, and the installed skill was missing or did not match)`
  and the command fails with exit 1, installing nothing and leaving the lock byte-identical
  (`src/local-lock.ts:433-435`, `tests/install-from-lock.test.ts:328-345`).

Frozen-lock comparison otherwise (`src/local-lock.ts:426-443`):

| Condition | Message |
| --- | --- |
| name absent | `<name> is not in skills-lock.json` |
| `computedHash === ""` | `<name> has no content hash in skills-lock.json (migrated from version 1, and the installed skill was missing or did not match)` |
| hash mismatch | `<name> has changed since skills-lock.json was written` |
| npm `version` mismatch | `<name> comes from version <new version or "unknown">, skills-lock.json has <locked version or "unknown">` |

The aggregate error starts with
`Cannot install with --frozen-lockfile because skills-lock.json is not up to date:` then one
two-space-indented line per change, then `Run again with --no-frozen-lockfile to update it.`
(`:445-451`).

Global lock "migration" is destructive, not an upgrade: `readSkillLock` returns a fresh v3 empty
lock when `version` is not a number, or `skills` is missing, **or `version < 3`** — the old file is
not migrated, just ignored, and the next write replaces it (`src/skill-lock.ts:96-107`). Any parse
or read error (including a torn write) also yields an empty lock (`:108-111`). There is no
`SKILLS_INVALID_LOCAL_LOCK` equivalent for the global lock: it never throws.

---

## 6. Write semantics

### 6.1 Project lock

- **Formatting**: `JSON.stringify(doc, null, 2)` plus a trailing `"\n"`; UTF-8; LF only
  (`src/local-lock.ts:359`, asserted `tests/local-lock.test.ts:139-175`).
- **Key order**: top level `version` then `skills`. `skills` keys are sorted with JavaScript's
  default `Array.prototype.sort()` — UTF-16 code-unit order, *not* the UTF-8 byte order used by the
  hash and not a locale collation (`:344-356`). Field order inside an entry is the object's
  insertion order (§2.3). (**observed**)
- **Portable local sources**: on every write, an entry with `sourceType: "local"` gets its
  (absolute) `source` rewritten relative to the lock directory, `/`-separated, prefixed `./`
  unless it is `..`/`../…`; the lock directory itself becomes `"."`; a different Windows drive
  stays absolute with `/` separators (`:350-353`, `:390-403`).
- **Merge vs replace**: `writeLocalLock(doc)` replaces the whole document with exactly what it is
  given — it does **not** merge. `addSkillToLocalLock` and `removeSkillFromLocalLock` do
  read-modify-write *inside* the mutex; `addSkillToLocalLock` replaces the whole entry for that key
  (no deep merge) and keeps all other keys (`:408-420`, `:456-470`).
- **Atomicity**: temp file `<lock>.<pid>.<12 hex>.tmp` in the same directory, `writeFile`, then
  `rename` over the lock; on `win32` up to 5 attempts on `EPERM`/`EBUSY` with 15/30/45/60 ms
  backoff; the temp file is unlinked on failure (`:322-336`, `:370-377`).
- **Permission preservation**: before writing, `access(lockPath, W_OK)`; a failure other than
  `ENOENT` is rethrown so a read-only lock reports a write error instead of silently replacing the
  directory entry via rename (POSIX) (`:361-368`). Tested:
  `tests/add-lock-write-failure.test.ts:34-42`.
- **Mutual exclusion**: an exclusive companion file `<abs lock>.lock` created with `open(..., 'wx')`.
  Acquire timeout 10 s; stale threshold 5 s on mtime; on `EEXIST` and stale, one waiter *claims* the
  stale lock by renaming it to `<mutex>.<pid>.<12 hex>.stale`, re-checks mtime, and if a new live
  lock appeared in between, links it back before unlinking its claim; waiters sleep 10–25 ms
  (`:264-320`, `:276-286`). Timeout error: `SKILLS_LOCAL_LOCK_BUSY` with the text
  `Timed out waiting for <path>: another skills process is updating skills-lock.json. If none is running, delete the file, then retry.`
  (`src/diagnostics.ts:218-221`).
- **Re-entrancy**: the mutex is *not* re-entrant, so the internal write helper is used from inside
  the lock and the public `writeLocalLock` acquires it separately. A Go port must keep that split.
- **Concurrency proven by tests**: 4 concurrent in-process adds, 4 concurrent OS processes, 4
  concurrent `installSkills` calls all retain every entry and leave only `skills-lock.json` in the
  directory; a 60 s-old `.lock` is broken in < 2 s (`tests/local-lock.test.ts:283-399`).
  A prune path reads the lock, then deletes its entry through `removeSkillFromLocalLock` (re-read
  under the lock) so an entry another process added in the meantime survives
  (`src/sync.ts:478-483`, `tests/sync-lock-race.test.ts:68-74`).
- **Reads** do not take the mutex; they rely on the atomic rename, so they see the old or the new
  document, never a partial one.

### 6.2 Global lock

- **Formatting**: `JSON.stringify(lock, null, 2)`, **no trailing newline** (`src/skill-lock.ts:125`).
  Directory is created recursively first (`:122`).
- **Ordering**: no sorting of `skills` at all — insertion order of the in-memory object, i.e. the
  order in the file for existing keys plus new keys appended (`:186-202`).
- **Merge vs replace**: `addSkillToLock` does read-modify-write of the single key, replacing that
  entry and preserving `installedAt`; `dismissPrompt`/`saveSelectedAgents` mutate one field
  (`:186-202`, `:278-301`). Every write serializes the entire parsed object, so unknown keys
  survive.
- **Atomicity**: **none**. Plain `writeFile` over the live path, no temp file, no rename, no
  mutex. A crash or a concurrent writer can truncate or lose whole sets of entries; a torn file is
  then read as an empty lock and the next write replaces it. (**observed in code; the failure mode
  is an assumption but follows directly**)
- **Failures are reported, not swallowed**: callers wrap each `addSkillToLock` and print
  `Installed <name> but could not record it in <path>: <reason>` and set exit code 1
  (`src/add.ts:2231-2237`, `:3319-3322`; tests `tests/add-lock-write-failure.test.ts:44-56`).
  The install itself stays on disk and is therefore untracked.

---

## 7. What each command does with the locks

| Command | Project lock (`skills-lock.json`) | Global lock (`.skill-lock.json`) |
| --- | --- | --- |
| `add` (project) | Preflight `readLocalLock()` before touching disk (throws on invalid), then per skill `computeSkillFolderHash`/blob snapshot hash and `addSkillToLocalLock`; write failure → error + exit 1 (`src/add.ts:1216-1217`, `:3299-3374`) | untouched |
| `add -g` | untouched | `readSkillLock` then `addSkillToLock` per skill with `skillFolderHash` (Git tree SHA for GitHub, content hash otherwise) (`src/add.ts:1407-1420`, `:3310-3324`; `src/add.ts:2292-2374`) |
| `remove` | `readLocalLock` (invalid → refuses, nothing deleted), `removeSkillFromLocalLock` only when no other agent still uses the copy; `--all`/`--skill '*'` limited to lock keys (`src/remove.ts:141-153`, `:498-520`) | `readSkillLock` for `--all`, `getSkillFromLock`/`removeSkillFromLock`; untracked folders are skipped with a warning (`src/remove.ts:364-372`; `tests/remove-global-lockfile.test.ts`) |
| `update` (project) | Reads lock, hashes the clone (`sourceSkillHash`), reinstalls when `skillPath`, `computedHash`, or on-disk presence changed, writes entry with new `skillPath`/`computedHash`/`commit`; back-fills a missing `commit` even when up to date — except in `check` mode (`src/update.ts:1254-1296`, `:1271`) | Reads lock, compares `skillFolderHash` (40-hex → Git tree SHA from clone, else folder hash), rewrites `skillFolderHash` + `commit` after reinstall (`src/update.ts:914-934`, `:1004-1017`) |
| `check` | Same read/hash pass with `reportOutdated`, so **no** commit pinning and no writes; also prints `✎ <name> has local changes` for entries whose disk hash ≠ `computedHash` and exits 1 (`src/update.ts:1381-1388`, `:1328-1351`) | Reads only; reports outdated / skipped |
| `list` | Read-only; invalid lock → `Warning: [SKILLS_INVALID_LOCAL_LOCK] …` on stderr and empty source info, exit still 0; compares disk hash only when `computedHash !== undefined`; statuses `tracked`/`modified`/`untracked`/`missing` (`src/list.ts:71-80`, `:113-151`) | `getAllLockedSkills` read-only; no hash check |
| `experimental_install` / `install` | `readLocalLock(cwd, {persistMigration: !frozen && !dryRun})`; `getLockedSkillState` decides skip/restore; `frozen` → `installFromSource({frozenLock})` verifies every fetched skill and writes nothing; else `recordProjectLock` → `addSkillToLocalLock` per restored skill; each entry's `installMode` decides copies (`src/install.ts:57-60`, `:111-154`, `:215-234`; `src/add.ts:2176-2271`) | Read only for `lastSelectedAgents` (`src/install.ts:33-38`) |
| `experimental_sync` | `readLocalLock`, prune stale lock entries under the mutex, frozen pre-check via `lockChangeReason(hash, version)`, then write `{source, sourceType:'node_modules', skillPath, version?, via?, computedHash}` per packaged skill and record remote entries through `installFromSource` (`src/sync.ts:690-706`, `:835-845`, `:950-970`, `:984-992`) | Read/write only for `lastSelectedAgents` (`src/sync.ts:580`, `:598`) |
| programmatic API (`installSkills`, `addSkills`, `removeSkills`) | Same helpers; `lockfile: false` skips recording (`docs/api.md:120-122`; `src/api/install.ts:205-208`, `src/api/remove.ts:118-121`) | Same |

`node_modules` entries are never restored by `experimental_install` directly — they are handed to
`experimental_sync` (`src/install.ts:111-115`, `:257-275`).

---

## 8. Global scope and non-`.agents` layouts

- The global lock records the same installs as the project lock but with a **different hash
  contract** (§3.2): GitHub-backed skills are tracked by the Git tree SHA of the *upstream* folder,
  so `check`/`update` need no local walk; a tree SHA is recognizable as exactly 40 lowercase hex
  (`src/update.ts:920`). Non-GitHub sources (local paths, downloads, generic Git without a tree)
  store the 64-hex content hash, and `update` recomputes it with the folder walk
  (`src/update.ts:921-923`). Both are stored in the same field, so a Go port must branch on
  `sourceType` and on the length/hex shape.
- Global installs never write the project lock, and project installs never write the global lock
  (`e2e/github.e2e.ts:158-176`). The global lock is also where `dismissed.findSkillsPrompt` and
  `lastSelectedAgents` live (`src/add.ts:572-692`, `:3596-3640`).
- `remove -g --all` deletes only lock-tracked folders, never hand-written ones in the same
  `~/.agents/skills` directory (`src/remove.ts:147-153`; `tests/remove-global-lockfile.test.ts`).
  After removing the last skill, the global lock remains with `"skills": {}`.
- A project-scoped `remove` refuses when the project skills dir resolves to the same real directory
  as the global one (cwd is home) and the name is tracked only globally, to avoid deleting a
  globally tracked skill and orphaning its global entry (`src/remove.ts:217-227`, `:364-372`,
  diagnostics `SKILLS_REMOVE_GLOBALLY_TRACKED`).
- Non-`.agents` agent directories (OpenClaw's `skills`, `data/skills`, `agent/skills`; Eve's
  `agent/skills` and `agent/subagents/<name>/skills`) are agent *link/copy* targets, never lock
  files. Eve is special: it may be copied rather than linked, and project installs can record
  `subagents: [""|"name", …]` so update restores placement (`src/local-lock.ts:73-79`,
  `src/add.ts:3346-3368`). For Eve, `getCanonicalPath` can point inside `agent/skills`
  (`src/installer.ts:703-712`), which is why `list`/`check` pass an explicit directory to
  `getLockedSkillState` instead of relying on `.agents/skills/<name>`
  (`src/list.ts:136-141`, `src/update.ts:1341-1343`).
- `old`/root-level skills: a repo whose `SKILL.md` sits at the repository root is now hashed over
  the whole directory, not just `SKILL.md` — regression test `tests/root-level-lock-hash.test.ts`
  proves a change in `scripts/` changes `computedHash`. The GitHub **blob** fast path is the
  exception: for a root-level skill it installs and hashes only `SKILL.md`
  (`src/blob.ts:744-752`).

---

## 9. Surprises and undocumented behavior

1. **Two different hash algorithms share one field name-ish.** `computedHash` (project) is always
   the 64-hex SHA-256 of §4. `skillFolderHash` (global) is either a 40-hex Git tree SHA or that
   same 64-hex content hash or `""` (`src/add.ts:2338-2366`, `:2312`, `:1993`). A naive port that
   compares a 40-hex and a 64-hex value will always report "update available".
2. **The README documents v2 and `computedHash`; the global lock is v3 and is documented only in
   `AGENTS.md:118-122`.** `AGENTS.md` still says an older lock is "wiped"; that is true only for
   the global lock — the project lock is upgraded in memory.
3. **`node_modules` is not excluded from the v2 hash** even though it was excluded from the v1
   hash. Only `.git`, `__pycache__`, `__pypackages__` directories and `metadata.json` are
   (`src/skill-hash.ts:11-12` vs `src/local-lock.ts:211`). A `node_modules` directory *inside* a
   skill folder is hashed.
4. **`metadata.json` is excluded by name at any depth**, so a legitimate `docs/metadata.json` is
   also invisible to the hash; likewise a directory named `metadata.json`
   (`isExcluded` ignores `isDirectory` for that name, `src/skill-hash.ts:15-19`).
5. **Sorting mismatch**: the hash sorts by UTF-8 bytes, the written `skills` object sorts with
   JS default (UTF-16 code units). They can disagree for astral-plane names; do not "fix" one to
   match the other.
6. **Global lock writes are not atomic and not locked**, unlike the project lock, and a torn read
   silently becomes an empty lock that the next write persists. Losing global tracking for every
   skill is a plausible outcome of two concurrent `skills add -g`.
7. **`readLocalLock` throws where the global read never does.** Any command that writes the project
   lock refuses on a conflicted lock (merge markers) and leaves disk untouched, but `list` degrades
   to a warning (`tests/invalid-local-lock.test.ts`, `src/list.ts:71-80`).
8. **`add` has no `--frozen-lockfile`** of its own; the frozen check lives in
   `installFromSource({frozenLock})`, used by restore and sync (`src/add.ts:2183-2217`).
9. **A missing `SKILL.md` makes a skill `missing` even when a hash matches**, and an existing
   `SKILL.md` with an empty hash is `modified`, not `tracked`
   (`src/local-lock.ts:235-247`).
10. **`installedAt` is only touched by the global lock**; the project lock is deliberately
    timestamp-free to minimise merge conflicts (`src/local-lock.ts:28-33`,
    `tests/local-lock.test.ts:654-692`). Two branches adding different skills produce
    non-overlapping keys.
11. **`__proto__`/`constructor` are legal skill names** and must be treated as ordinary own keys:
    load with `Object.hasOwn`-style checks, write with data-property semantics
    (`tests/local-lock.test.ts:454-483`).
12. **Local sources are stored relative but resolved absolute on read**; the in-memory object after
    `readLocalLock` holds absolute paths, and only the writer converts back
    (`src/local-lock.ts:161-165`, `:390-403`).
13. **A different Windows drive yields an absolute `source` with forward slashes** in the lock
    (`src/local-lock.ts:394-397`) — a lock written on `D:` is not portable as a relative path.
14. **`experimental_install` rewrites a v1 lock to v2 before it knows whether the restore will
    succeed** (non-frozen, non-dry-run). `--force` skips the "already installed / matches the hash"
    decision and reinstalls everything, then rewrites `computedHash` from the fresh install; it does
    not bypass a frozen check (`src/install.ts:57-60`, `:117-124`, `:232`).
15. **A frozen restore of a v1-migrated well-known entry with `computedHash: ""` installs the files
    and then fails** the post-install verification, because the frozen pre-check skips well-known
    skills but the post-check does not (`src/add.ts:2194-2204` vs `:2212-2217`). Reported, lock
    untouched. (**observed by reading, not covered by tests**)

---

## 10. Open questions

1. Is the global lock's non-atomic write intentional, or an accepted risk? Nothing in the code or
   docs says. A Go port must decide whether to add a temp-file+rename (behavior-compatible, more
   robust) or mirror the hazard exactly.
2. Should the project lock writer ever be expected to preserve unknown top-level keys? Current
   behavior drops them; no test asserts either way. (`src/local-lock.ts:358`)
3. Does anything rely on `computedHash` for `sourceType: "local"` when the source is outside the
   project? `isUnmanagedProjectDir` uses `pathsOverlap(entry.source, path)` plus the hash to decide
   whether a remove may delete a real directory (`src/remove.ts:381-392`); the exact intended
   policy for nested/overlapping source paths is not spelled out.
4. What is the intended ordering contract for `skills` keys — sorted file output is asserted
   (`tests/local-lock.test.ts:139-175`) but the sort comparator (UTF-16 vs UTF-8) is never stated;
   a Go port using `sort.Strings` (byte order) would differ only for non-BMP names.
5. Is `skillFolderHash: ""` for well-known skills (`src/add.ts:1993`) meant to disable update
   checks, or is it a placeholder? `update` routes well-known entries by
   `sourceBaseUrl` + `wellKnownDigest` before the `skillFolderHash` check (`src/update.ts:777-782`),
   which suggests "placeholder", but nothing states it.
6. Should `install --frozen-lockfile` reject a v1 lock with an empty hash *before* installing
   well-known skills (see surprise 15)? Current code does not.

---

## 11. Evidence index (tricky parts)

| Claim | Evidence |
| --- | --- |
| Project lock path, no upward walk | `src/local-lock.ts:102-104`; callers pass cwd: `src/install.ts:55-60`, `src/update.ts:337`, `src/list.ts:74` |
| Global lock path / XDG | `src/skill-lock.ts:72-82`; `src/test-utils.ts:76-94`; `tests/xdg-config-paths.test.ts:86-103` |
| Local lock versions accepted / rejected | `src/local-lock.ts:154-159`; `tests/local-lock.test.ts:127-135` |
| Global lock v3 wipe | `src/skill-lock.ts:96-111`; `src/update.test.ts:50-66` (v3 fixture) |
| Hash file exclusions | `src/skill-hash.ts:11-25`; `tests/local-lock.test.ts:630-651` |
| Symlink policy and cycle guard | `src/skill-hash.ts:87-105`; `tests/skill-hash.test.ts:116-149` |
| CRLF / NUL binary rule | `src/skill-hash.ts:57-61`; `tests/skill-hash.test.ts:52-64`; `tests/check.test.ts:74-84` |
| Framing bytes | `src/skill-hash.ts:37-55`; `README.md:95-106`; `tests/skill-hash.test.ts:40-50` |
| Byte-wise path sort | `src/skill-hash.ts:39-44`; `tests/skill-hash.test.ts:30-38` |
| v1 legacy hash (locale sort, no framing, `.git`/`node_modules` only) | `src/local-lock.ts:198-228` |
| v1 migration decision table | `src/local-lock.ts:167-196`; `tests/skill-hash.test.ts:152-198`; `tests/install-from-lock.test.ts:67-95` |
| Empty hash → modified / frozen failure | `src/local-lock.ts:242-246`, `:433-435`; `tests/install-from-lock.test.ts:328-345` |
| Frozen message text | `src/local-lock.ts:426-451` |
| Local write formatting, sorting, trailing newline | `src/local-lock.ts:338-359`; `tests/local-lock.test.ts:139-175` |
| Local atomic write + Windows retry + W_OK precheck | `src/local-lock.ts:322-378`; `tests/add-lock-write-failure.test.ts` |
| Mutex protocol, timeouts, stale break | `src/local-lock.ts:261-320`; `tests/local-lock.test.ts:283-399` |
| Concurrent sync prune under lock | `src/sync.ts:478-483`; `tests/sync-lock-race.test.ts:68-74` |
| Global write non-atomic / no newline / key preservation | `src/skill-lock.ts:118-127` |
| Global entry fields and tree SHA | `src/skill-lock.ts:13-47`, `:171-181`; `src/blob.ts:329-349`; `e2e/github.e2e.ts:169-176` |
| Global hash may be content hash (local/download) | `src/add.ts:2311-2312`, `:2347-2356`; `tests/download-lock.test.ts:171-192` |
| 40-hex vs 64-hex branch on update | `src/update.ts:920-923` |
| Renamed skill hashing (rewrite `name:`) | `src/add.ts:2425-2449`; `src/frontmatter.ts:24-45`; `README.md:110` |
| `skillName` semantics | `src/local-lock.ts:56-61`; `src/add.ts:1804-1806`; `tests/update-lock-only-walk.test.ts` (locked paths) |
| Commit pinning and back-fill | `src/install.ts:126-132`; `src/update.ts:1270-1273`; `tests/lock-commit-pin.test.ts:60-115` |
| `installMode` copy round-trip | `src/local-lock.ts:81-82`; `src/install.ts:140-142`, `:294-318`; `tests/install-from-lock.test.ts:270-280` |
| Download entries and `skillPath` | `src/add.ts:1601-1625`; `docs/commands.md:113`; `tests/download-lock.test.ts:87-131` |
| Invalid project lock behavior per command | `tests/invalid-local-lock.test.ts:51-113`; `src/list.ts:71-80` |
| `remove --all` lock ownership | `src/remove.ts:141-153`; `tests/remove-global-lockfile.test.ts` |
| Root-level hash covers whole dir | `tests/root-level-lock-hash.test.ts:103-137`; blob root exception `src/blob.ts:744-752` |
| Prototype-named keys | `src/local-lock.ts:417`, `:456-470`; `tests/local-lock.test.ts:454-483` |
| `sanitizeName` for folders vs raw lock keys | `src/sanitize.ts:73-88`; `src/local-lock.ts:107-109`; `src/installer.ts:676-719` |
