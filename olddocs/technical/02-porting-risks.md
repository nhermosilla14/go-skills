# 02 — Porting risks: TypeScript `skills` CLI → Go

Target platforms for the port: Windows x86 / x86_64; Linux x86 / x86_64 / aarch64 / armhf;
macOS Intel / Apple Silicon.

All statements about the TypeScript implementation are tagged **observed** (read in
`/tmp/opencode/vercel-skills`) or **assumed** (inference, no direct evidence). Nothing here
was executed.

---

## 1. Behaviour that is easy to get subtly wrong

### 1.1 The two hashes (highest-risk area)

There are **two independent hash schemes** in the codebase, with **two different sort
orders**. Getting either wrong silently breaks update detection and frozen installs
without failing loudly.

**A. `computedHash` in `skills-lock.json` v2** — `src/skill-hash.ts` (**observed**):

1. Collect every file under the skill folder. Skip an entry named `metadata.json` at any
   depth, and directories named `.git`, `__pycache__`, `__pypackages__` at any depth.
2. Symlinks count as their target. A link whose realpath is broken, cyclic, or outside the
   skill root is **omitted** (and warns). Directory links are walked, not blindly copied.
3. Relative path, `/`-separated (backslashes converted).
4. Sort by **the UTF-8 bytes of the path** (`Buffer.compare`), explicitly *not* locale
   order. The README says the same and gives test vectors.
5. A file is "text" if it contains no `0x00`; text files get **every** `\r\n` replaced by
   `\n` (implemented as a latin1 round-trip). Binary files are untouched.
6. Frame each file as `path` `NUL` `decimal byte length` `NUL` `content`, fed into one
   SHA-256, in sorted order. Lowercase hex digest.

Porting notes:
- Go's `sort.Slice` with `bytes.Compare` on `[]byte` matches step 4; `sort.Strings` happens
  to match for valid UTF-8 because Go compares bytes, but state the intent explicitly.
- Do **not** normalise CRLF lazily per chunk; the check is "contains NUL" first, then
  "contains CRLF", then replace all. A file with a lone `\r` is unchanged.
- The length is **post-normalisation** bytes, decimal ASCII, no padding.
- Exclusions are name-based, not path-based, so a nested `deep/metadata.json` is excluded
  too (**observed** `isExcludedPath` splits on both separators).

**B. `skills-sh-tree-v2`** — `src/skills-sh-hash.ts` (**observed**): used only for
skills.sh pack pins, only text files (binary-extension deny-list), symlinks skipped
entirely, `.git`/`node_modules`/`.next`/`dist`/`build`/`__pycache__`/`.venv`/`venv`
excluded, sorted by **JavaScript string `<` comparison (UTF-16 code units)**, then
`"skills-sh-tree-v2\n<count>\n"` and per file `<pathByteLen>\n<path><contentByteLen>\n<content>`.
Its comment says the leaderboard must match exactly.

> Risk: a single Go implementation that "sorts paths" and is reused for both schemes will
> diverge on non-BMP filenames (UTF-16 code-unit order ≠ UTF-8 byte order) and on binary
> file inclusion. Implement two functions, and add the three README test vectors plus a
> non-BMP filename case as Go tests.

**C. Legacy v1 hash** — `computeLegacySkillFolderHash` (**observed**): `localeCompare`
sorting, no framing, no CRLF normalisation, excludes only `.git` and `node_modules`, and
only regular files. It exists solely to validate a v1 lock before migrating. Reproducing
`localeCompare` in Go is effectively impossible in general (ICU collation); a Go port either
drops v1 migration or needs an explicit decision. **Assumed**: dropping migration and
returning empty hashes for v1 entries is acceptable, since the code already treats "cannot
verify" as empty hash.

### 1.2 Lock-file byte-level format and merge behaviour

**Observed** `src/local-lock.ts`:

- `skills-lock.json` = `{ version: 2, skills: { <name>: entry } }`, keys sorted
  alphabetically, serialised with `JSON.stringify(x, null, 2)` plus a trailing `\n`.
- The sort is `Object.keys(...).sort()` → **UTF-16 code-unit order**. For ASCII skill names
  this equals byte order; for non-BMP names it does not. Go's `sort.Strings` is byte order,
  so a lock written by Go can differ byte-for-byte from one written by Node even with the
  same entries. Since the file is checked into VCS, that is a diff-noise and possible
  merge-conflict risk, not a correctness break.
- Fields are emitted in a fixed insertion order per entry (`source`, `sourceUrl`, `ref`,
  `commit`, `sourceType`, `skillPath`, `skillName`, `version`, `via`, `computedHash`,
  `subagents`, `wellKnownDigest`, `installMode`). Go maps are unordered → use an ordered
  struct with `omitempty` and verify with a golden-file test.
- **Atomic write**: temp file `<lock>.<pid>.<12 hex>.tmp` + rename, with 5 retries and
  backoff on Windows `EPERM`/`EBUSY` (**observed**). Comment says rename on POSIX mutates
  the parent dir, so it pre-checks `W_OK` on the lock file to preserve read-only errors.
  Go's `os.Rename` on Windows replaces the destination *only* if it is a file and lacks
  delete-sharing conflicts; retry logic must be carried over, and `os.Chmod`-based
  read-only detection differs from access(2) semantics.
- **Mutex**: sibling `<lock>.lock` created with `open(..., 'wx')`, 10–25 ms jittered polling,
  stale threshold 5 s (mtime), break-by-rename to `<lock>.<pid>.<hex>.stale`, re-`link()`
  if the moved file turned out to be live, 10 s acquire timeout, `SKILLS_LOCAL_LOCK_BUSY`.
  `link()` on Windows historically requires privileges for symlinks but hard links are fine
  (NTFS); on FAT/network shares it fails — the code swallows that failure (**observed**).
  Go must replicate the *semantics*: stale detection by mtime, single-winner claim, and
  re-link on loss. A naive `O_EXCL` + timeout loses the "don't delete a live lock" property.
- Invalid locks are fatal (`SKILLS_INVALID_LOCAL_LOCK`): merge-conflict markers, missing
  `skills` object, or an unknown-version lock raise instead of being treated as empty,
  because "empty + next write" would drop every entry (**observed** comment). The global
  lock is the opposite: `readSkillLock` silently returns empty on any error and **wipes**
  versions `< 3` (**observed**) — that asymmetry must be preserved, or behaviour changes.
- Local sources are stored portable: relative to the lock dir with `/` separators; when the
  relative path would cross Windows drives, an absolute forward-slashed path is stored
  (**observed**). Cross-drive behaviour is a real Windows edge case for a Go port.

### 1.3 YAML frontmatter

**Observed** `src/frontmatter.ts`:

```text
^\uFEFF?---\r?\n ([\s\S]*?) \r?\n---\r?\n? ([\s\S]*)$
```
- Only `---` fences; `---js`/`---javascript` are intentionally unsupported (avoids
  `gray-matter`'s eval-based RCE).
- Leading UTF-8 BOM tolerated; `\r\n` or `\n` after each fence, but the **closing fence must
  be preceded by** `\r?\n`; a file ending `---` with no trailing newline still matches
  because `\r?\n` before the close is required but the trailing part is optional.
- Parsed with `yaml` v2 (YAML 1.2 core-ish schema). Two consequences:
  1. `name: 123` parses as a **number**, and `parseSkillMd` then rejects the skill because
     `name`/`description` must be `string` (**observed**). A Go YAML v3 decoder that
     unmarshals into `any` yields `int` too — so `name: 123` must stay invalid, not be
     stringified. Conversely `name: yes` is a string in YAML 1.2 but `true` in YAML 1.1;
     `gopkg.in/yaml.v3` is YAML 1.2-ish and resolves `yes` as a string, matching the Node
     library — verify explicitly, because yaml.v2 would differ.
  2. `description: >` / `|` multi-line values, quotes, anchors, and merge keys all go
     through the real parser.
- Multi-document files, duplicate keys (yaml v2 errors on duplicates by default? — **assumed**),
  tabs, and non-string keys are all edge cases where the two libraries can disagree.
- `setFrontmatterName` (used by `--rename`) is **byte-preserving** and line-based: it finds a
  line matching `^name\s*:` in the raw body, deletes that line **plus following indented
  continuation lines**, and inserts the serialised `name:` line. It re-joins with the
  original EOL style. A Go port must reproduce "delete continuation lines" and "insert
  first when absent" exactly, including CRLF preservation, or rename changes the hash
  (the README states the renamed skill's hash differs from upstream, so **any** formatting
  drift changes lock values).
- If there is no frontmatter, `setFrontmatterName` returns the input unchanged.

### 1.4 Symlink semantics

**Observed** `src/installer.ts`, `src/remove.ts`, `src/skill-hash.ts`:

- Default install: **copy** the skill into `.agents/skills/<name>` (canonical), then
  **symlink** each non-universal agent's dir entry to it. `--copy` copies into every agent
  dir instead. A third mode `link` symlinks the canonical dir at the source tree (used by
  `experimental_sync` for `node_modules` sources).
- POSIX link target is **relative**, computed from the **realpath of the link's parent**
  (because `~/.claude/skills` may itself be a symlink into a dotfiles repo).
- Windows link is a **junction** (`fs.symlink(target, path, 'junction')`) with an **absolute**
  target. Junctions are directory-only, same-volume-preferred, and need no elevation;
  symlinks need Developer Mode or admin. Go has no junction API: implement the reparse-point
  ioctl (`FSCTL_SET_REPARSE_POINT`, `IO_REPARSE_TAG_MOUNT_POINT`) via
  `golang.org/x/sys/windows`, or shell out to `cmd /c mklink /J`. Using `os.Symlink` on
  Windows silently requires the privilege and will fail for normal users — the codebase
  explicitly tells users to "enable Developer Mode" only as a fallback message
  (**observed** `add.ts`), so the port must not make the fallback the default path.
- Before linking, the code compares `realpath(target)` with `realpath(linkPath)` **and**
  parent-resolved variants, and returns success early if they are equal. This prevents
  `rm -rf` of the canonical directory when the agent dir is already a link to it. Node's
  Windows `realpath` resolves junctions; Go's `filepath.EvalSymlinks` also resolves reparse
  points, but behaviour differs for non-existent tails and for volume-GUID paths
  (**assumed**) — test with a junction and a `\\?\` path.
- `realpath` on a **non-existent** path throws in Node and is caught → falls back to the
  unresolved path. Go's `EvalSymlinks` also errors; same handling required.
- Existing link handling: if `lstat` says symlink, `rm` it; else `rm -r`. `ELOOP` gets a
  force-remove attempt. A Go port must not use `os.Stat` (follows links) where `Lstat`
  semantics are required, and must not recurse into a junction when deleting.
- Windows **case-insensitivity** is not handled: `isPathSafe` does a case-sensitive string
  `startsWith(base + sep)`. On NTFS, `C:\Foo` vs `c:\foo` compares unequal → false
  "unsafe path" rejections or, worse, missed containment when the attacker uses a
  different case that the filesystem treats as the same path. **Observed** logic;
  impact **assumed**. A Go port should either preserve the exact behaviour (bug-for-bug)
  or fix it deliberately and document the difference — not accidentally fix it.
- Symlink escape detection uses `target === root || target.startsWith(root + sep)`, again
  string-based, on `realpath` outputs. `filepath.Rel` + `..` checks are the idiomatic Go
  equivalent but are *not* byte-identical (trailing separators, volume names).

### 1.5 CRLF, encodings, and text/binary decisions

- The only CRLF normalisation is inside the skill hash (**observed**).
- `init` writes the template with `\n` via `writeFileSync` (no EOL translation on any
  platform) — Go must not use `\r\n` on Windows.
- `setInstalledSkillName` writes back the modified `SKILL.md` preserving its original EOLs
  (**observed** via the regex/join approach).
- "Text" is defined as "no NUL byte" in the hash, and as extension deny-list in the
  skills.sh hash. A Go port that uses `unicode/utf8.Valid` instead of the NUL test will
  diverge on latin-1 skill files.
- `metadata.json` is excluded by *exact name*; a Go `filepath.Match`-style filter must be
  exact, not prefix-based.

### 1.6 Terminal prompt rendering

**Observed** `src/prompts/search-multiselect.ts`, `src/find.ts`:

- Raw `readline` with a silent writable stream to suppress echo; `emitKeypressEvents`;
  keys handled: arrows, page up/down, space, enter, esc, ctrl-c, backspace, printable
  characters for the filter.
- Redraw erases the previous block by computing **physical rows**:
  `visualRowsForLine = max(1, ceil(approxStringWidth(strippedLine) / columns))`, with
  `columns` from `process.stdout.columns` or 80. Width uses a **hand-rolled wcwidth-ish
  table** (East Asian + emoji ranges, combining marks not handled). This is where a Go port
  typically drifts: runes vs bytes vs cells; cluster/ZWJ emoji count as N cells here.
- Truncation is per-rune via `truncateToWidth`, appending `…`.
- Cursor/line control (`\r\x1b[K`) is TTY-only and colour-independent.
- `find.ts` has a **second**, independent raw-readline picker (fzf-style) with debounced
  API search — two prompt implementations to port, not one.
- Windows: Node's readline works on ConPTY; Go will need to put the console into raw mode
  (`golang.org/x/term`), and `\x1b[K` works in Windows Terminal but has quirks in legacy
  `conhost` (**assumed**). Ctrl-C should surface as a cancel, not a signal death.
- Non-TTY: prompting is an error (`SKILLS_PROMPT_REQUIRES_TTY`) on the add path, and
  `-y`/`--agent`/`--skill` must make every path non-interactive. Any Go prompt library that
  blocks on `stdin` will hang CI.
- **No clipboard library is used anywhere** (**observed**). The "clipboard substitute" is
  `skills use … | claude` (prompt on stdout) and `--agent`, which spawns the agent with the
  whole prompt as **one argv element**:
  `spawn(cmd, [...args, prompt], { stdio: 'inherit' })` — a large `SKILL.md` can exceed
  ARG_MAX on Linux/macOS (~2 MB, but per-arg limits and env-size sharing) and the 32 KB
  command-line limit on Windows (**observed** code, limit **assumed**). A Go port that keeps
  this design inherits the risk; feeding the prompt on stdin would change agent behaviour.

### 1.7 Unicode sorting, width, and alignment

- Hash path sort: UTF-8 bytes (scheme A) vs UTF-16 code units (scheme B) — see 1.1.
- Lock key sort: UTF-16 code units.
- Display padding: `String.prototype.padEnd(n)` counts **UTF-16 code units**, so an emoji
  (2 units) gets 2 columns of padding, while a terminal renders 2 cells — accidentally
  close, but ZWJ sequences count as many units and render as 1 cluster. `list` output
  alignment will differ if Go pads by runes or by display width.
- `formatList(items, maxShow = 5)` truncates by count, not width; `+N more` suffix.
- Security-advisory table pads names with a `padEnd` helper that strips ANSI first, then
  pads by `.length` (**observed** `add.ts#padEnd`) — same UTF-16 caveat.
- Sorting of agents/groups: `Object.keys().sort()` and `localeCompare` appear in different
  places; pick the same order per site, not one global order.

### 1.8 ANSI colours

**Observed** `src/color.ts`:

```
enabled = !(NO_COLOR || --no-color) && (FORCE_COLOR || --color || CI || (isTTY && TERM != 'dumb'))
```
- This is picocolors *minus* the `process.platform === 'win32'` auto-enable. A Go port using
  a generic colour library will typically re-introduce auto-enable on Windows → escapes in
  redirected output in CI logs on Windows agents.
- `CI` alone turns colours **on**, which is the opposite of many libraries.
- Colour constants are 256-colour SGR (`38;5;102`, `38;5;145`, gradient 250→238) — must be
  emitted verbatim to keep visual parity; no colour-name mapping.
- `CLEAR_LINE` is `\r\x1b[K` only when `stdout.isTTY`.
- All untrusted text goes through `stripTerminalEscapes` before printing (CSI, OSC, DCS/PM/
  APC, C1, control chars except `\t`/`\n`). A Go port needs the same regex-equivalent
  scanner; `regexp` in Go has no backreferences but these patterns are fine. Missing this
  re-opens CWE-150 (a malicious skill name changing the terminal title or clearing the
  screen).
- `formatError` delegates to `nostics`' ANSI formatter: the exact layout (code, `why`, then
  a separate `fix` line) is user-visible. A Go port must decide whether to replicate the
  formatter byte-for-byte or accept cosmetic diffs (**assumed acceptable**, but tests and
  docs quote those strings).

### 1.9 Archive extraction safety

**Observed** `src/archive.ts`, `src/download-source.ts`, `src/providers/wellknown.ts`:

- ZIP is **hand-parsed** (no dependency): EOCD scan, optional ZIP64 locator/record, central
  directory, local headers, stored + deflate only (`inflateRawSync`). Rejects multi-disk,
  encrypted entries, unsupported methods, and — via `rejectedArchiveDiagnostics` — link
  entries, unsafe paths, too many files, too large extraction.
- Filename decoding: UTF-8 flag bit honored, otherwise CP437 table with an info-ZIP
  Unicode Path extra field (`0x7075`) override (**observed** `decodeFileName`).
  Go's `archive/zip` exposes `File.Name` already decoded with the same CP437/UTF-8 rule and
  a `NonUTF8` flag; the extra-field parsing is **not** exposed, so a faithful port needs
  manual extra-field scanning or accepts `archive/zip`'s behaviour (different for archives
  that set CP437 bytes plus a Unicode extra field).
- Entry path validation: backslash → `/`, reject absolute, reject `..` segments, then
  `join(extractDir, path)` re-checked with the string-prefix containment test.
- Limits: download 10 MiB (`SKILLS_DOWNLOAD_MAX_BYTES`), extract 25 MiB / 1000 files
  (`SKILLS_EXTRACT_MAX_*`); well-known uses 50 MiB / 1000; Notion 100 MiB / 5000. The byte
  counter increments **before** writing and aborts by throwing from the filter.
- TAR goes through the `tar` package with `strict: true`, `preservePaths: false`,
  `noChmod: true`, and a filter that accepts only `File`, `OldFile`, `ContiguousFile`,
  `Directory` — so symlinks, hardlinks, devices, FIFOs are silently skipped, not errors.
- `tryExtractArchive` sniffs `PK` (zip) or `1f 8b` (gzip); anything else is attempted as
  tar; a rejected archive is re-thrown, an unreadable one means "not a skill" →
  `SKILLS_DOWNLOAD_NOT_A_SKILL`. That distinction is user-visible.
- Go's `archive/zip` will happily report `Name` with `..`; the validation must be applied by
  the port. Go's `archive/tar` requires explicit type filtering. Neither library enforces
  the size/entry caps — those are caller logic.
- Extraction writes into a temp dir; a rejected archive deletes and recreates the extract
  dir. Symlinked extraction roots and `O_NOFOLLOW` are not used (**observed**) — a
  pre-existing symlink inside a temp dir is unlikely but worth noting.

### 1.10 Git authentication through subprocesses

**Observed** `src/git.ts`:

- Clone env is hardened: `GIT_TERMINAL_PROMPT=0`, `GIT_ALLOW_PROTOCOL=https:http:ssh:git:file`,
  `GIT_LFS_SKIP_SMUDGE=1`, `LC_ALL=C`, `filter.lfs.required=false` + empty smudge/clean/
  process filters, `core.longpaths=true`.
- `ext::` URLs are rejected up front (transport RCE vector).
- Auth-failure detection is **English substring matching**:
  `Authentication failed`, `could not read Username`, `Permission denied`,
  `Repository not found`, `requested URL returned error: 403`, plus GitHub SAML-SSO
  detection. `LC_ALL=C` is set *because* of this — a translated git would skip the `gh`
  and SSH retries.
- Missing-ref detection is also substring matching on three messages that vary by client
  and server ("Remote branch … not found", "couldn't find remote ref", "upload-pack: not
  our ref"). Comment says "Verified on git 2.53".
- Retry chain on GitHub HTTPS auth failure: `gh auth status -h <host>` (5 s timeout) → if
  `Git operations protocol: ssh`, clone the SSH URL via `gh repo clone` → else clone
  `git@host:owner/repo.git` with `GIT_SSH_COMMAND` defaulting to `ssh -o BatchMode=yes`.
- Commit-SHA refs: `--branch <sha>` first; if it fails with a missing-ref message and the ref
  is a full 40-hex SHA, re-`init` + `fetch --depth 1 origin <sha>` + `checkout FETCH_HEAD`.
- Submodule init failures are **non-fatal** (a `console.warn`, not a diagnostic).
- Tree/commit hashes come from `git -C <dir> rev-parse --verify --end-of-options <rev>`.
- `cleanupTempDir` refuses to delete anything outside `os.tmpdir()` (prefix check).

Go port implications: use `os/exec` with the same env, the same message matching (and the
same `LC_ALL=C`), and the same fallback ordering. Using a Go git library instead of the git
binary changes credential-helper handling, submodule behaviour, LFS filters, and error
strings — every one of those changes is user-visible.

### 1.11 Path semantics

- `isPathSafe` / `pathsOverlap` / `isWithinRoot` / `isContainedIn` are all
  `normalize(resolve(p))` + case-sensitive string prefix with `sep`. Node's `normalize`
  collapses `..`, resolves `.`, and **on Windows keeps a trailing separator for roots**;
  Go's `filepath.Clean` differs on `C:` and on UNC roots (**assumed**).
- Windows path handling is `\`-based; lock files and telemetry use `/`. Conversion points
  are `split('\\').join('/')` and `split(sep).join('/')`. Go must not use
  `filepath.ToSlash` blindly on inputs that contain literal backslashes in a POSIX path.
- Hash paths are always `/`-separated regardless of platform — a port that hashes
  `filepath.Rel` output on Windows without slash conversion changes every hash.
- `sanitizeName`: lowercase → `[^a-z0-9._]+` → `-` → trim leading/trailing `.` and `-` →
  cap 255 chars → fallback `unnamed-skill`. Note `.` and `_` are preserved *inside* names
  (`ce:review` → `ce-review`, `a.b` stays `a.b`). `toSkillSlug` is a **different**
  function (`[^a-z0-9-]` removed, not replaced) used for skills.sh URLs — do not merge them.
- `shortenPath` replaces the `homedir()` prefix with `~` and `cwd` with `.` by plain prefix
  match; no separator boundary check (**observed**), so `/home/foo` also matches
  `/home/foobar`.

### 1.12 Exit codes and process lifecycle

**Observed** `src/cli.ts`, `src/add.ts`, `src/use.ts`, `src/update.ts`, `src/install.ts`:

| Situation | Code |
| --- | --- |
| success | 0 |
| unknown command / bad option | 1 (`process.exitCode`) |
| prompt required, stdin not a TTY | 1 (`process.exit`) |
| prompt cancelled at a TTY | 0 (`process.exit`) |
| install failed / lock write failed | 1 |
| `check` with outdated or modified skills | 1 |
| frozen lockfile would change | 1 |
| `use` agent exited non-zero | agent's code |
| `use` CLI error | 1 (`process.exit`) |

- The main path deliberately **drains the event loop** instead of `process.exit()`, to avoid
  a Windows libuv `UV_HANDLE_CLOSING` assertion while telemetry sockets tear down
  (**observed** comment). A Go port has no such issue, but if it calls `os.Exit` early it
  will **skip telemetry flush and abort in-flight HTTP**, changing observable behaviour
  (telemetry loss) — and, more importantly, may truncate stdout when the consumer is a pipe
  (Go's `os.Exit` skips deferred buffered writes, and `fmt.Println` to a pipe is a direct
  write, so this is mostly safe).
- Because `process.exit()` is used by `emitJsonAndExit`, `exitInstallationCancelled`, and
  `use.ts#fail`, the `.finally(flushTelemetry)` handler does **not** run on those paths
  (**observed** code structure). So telemetry is already best-effort in JSON/cancel/use
  paths. A Go port can choose to be *more* reliable here; that is a behaviour difference,
  not a bug fix, and should be documented.
- `--version` prints just the version to stdout with no colour.

### 1.13 Telemetry wire format

**Observed** `src/telemetry.ts`:

- `GET https://www.skills.sh/tele/t?<params>` and `.../tele/audit?source=…&skills=a,b`.
- `URLSearchParams`: keys in insertion order (`v`, `ci`, `agent`, then event fields), spaces
  encoded as `+`, values `String()`-coerced. Go's `url.Values.Encode()` sorts keys and also
  uses `+` — ordering differs, which matters only if the server signs or logs raw URLs
  (**assumed** it does not).
- Booleans are sent as `'1'` (e.g. `global`, `interactive`, `ci`), counts as decimal strings.
- `skillFiles` is a **JSON string** nested inside the query string, e.g.
  `{"pdf":"skills/pdf/SKILL.md"}` — double-encoded, so Go must `json.Marshal` then
  URL-encode, not flatten.
- The audit request is skipped entirely when telemetry is disabled or the skill list is
  empty, times out at 3 s, and treats any non-2xx or parse failure as `null`.
- The audit response is consumed as `risk ∈ {safe, low, medium, high, critical, unknown}`
  plus partner ids `ath`, `socket`, `snyk` with `alerts`, `score`, `analyzedAt`. Unknown
  partner keys are tolerated (rendering only) — **assumed** from the mapping code.
- Privacy gate: identifiers are only attached for repos GitHub positively confirms public;
  `isRepoPrivate` returns `true | false | null` and `null` means "omit"
  (**observed** comments + docs). Getting this wrong leaks private repo names.
- No hashing/anonymisation client-side; no user id; no timestamps.
- Node's `fetch` follows redirects (up to 20) and honours proxy env vars only when Node is
  configured to; Go's `http.Client` follows 10 redirects and **does not** honour
  `HTTP_PROXY` unless `ProxyFromEnvironment` is used (which also ignores `NODE_USE_ENV_PROXY`
  semantics). e2e passes `NODE_USE_ENV_PROXY` through, implying some environments rely on
  Node's proxy support (**observed** env allow-list, effect **assumed**).

### 1.14 Discovery order and duplicate handling

**Observed** `src/skills.ts`:

- Search depth defaults to `DEFAULT_SKILL_CONTAINER_DEPTH = 3`; `findSkillDirs` recurses to
  maxDepth 5 for the "no SKILL.md at root" case; `--full-depth` changes the policy.
- Skip dirs: `node_modules`, `.git`, `dist`, `build`, `__pycache__`.
- Extra containers: plugin manifests (`marketplace.json`/`plugin.json`), `skills.sh.json`
  `paths`, legacy agent dirs (`LEGACY_AGENT_PROJECT_SKILL_DIRS`), each agent's `skillsDir`.
- Duplicate names across folders are allowed (first wins) unless the API is called with
  `duplicates: 'error'`; the CLI surfaces `SKILLS_DUPLICATE_SKILL_NAME` only in that mode.
- Invalid `SKILL.md` files are **warnings**, not errors, and are silently dropped in the API
  when no `onWarning` handler is supplied.

A Go port that changes discovery order or depth changes which skill "the first one found"
is — and therefore which content lands in the lock.

### 1.15 Timeouts and retries

**Observed**: clone 300 s (`SKILLS_CLONE_TIMEOUT_MS`), GitHub API 10 s, blob/well-known
fetch 10 s, download 30 s, `gh auth status` 5 s, `ntn` 30 s, audit 3 s, telemetry flush 5 s,
lock acquire 10 s, e2e per-command 170 s. Timeouts are wall-clock from process start, not
per-chunk; Go's `context.WithTimeout` plus `http.Client.Timeout` gives the same shape, but
`http.Client.Timeout` also covers body reads whereas Node's `AbortSignal` aborts mid-stream —
practically equivalent, semantically slightly different for large downloads.

---

## 2. Hard dependencies on Node-specific behaviour

| # | Node dependency | What a Go port must emulate or consciously drop |
| --- | --- | --- |
| 1 | **`node_modules` / `package.json` sync** (`src/sync.ts`) | Walks `dependencies`, `devDependencies`, `peerDependencies`, `optionalDependencies` + workspace packages (`pnpm-workspace.yaml`/`workspaces`), resolves each installed package dir, reads its `skills` field and any `SKILL.md` shipped in the tarball, and treats `node_modules` as the source of truth. Uses `fs/promises.glob` and `path.posix.matchesGlob` (`*`, `**`, `?`, classes) for `--include/--exclude`. Go has no `path.matchesGlob`; you need a glob implementation with matching semantics (including `**` matching zero segments) or you silently change filter results. Also `realpath`-based dedupe and "is this link ours" ownership heuristics (`linksIntoNodeModules`, `installedBySync`) depend on Node's `readlink` returning the raw stored target. **Decision required**: keep full sync (re-implement glob + workspace discovery) or drop `experimental_sync`. |
| 2 | **npm library API** (`dist/index.mjs`) | Nine named exports guarded by a `tsnapi` snapshot; callers may exist in the wild. Go has no runtime-compatible equivalent: you can ship a separate Go package with the same concepts, but you cannot provide `import { installSkills } from 'skills'` for JS consumers. If JS consumers must keep working, the npm package has to remain (e.g. keep the TS package and add a Go binary beside it). Also note: `installSkills` returns **frozen** result objects, supports `AbortSignal`, `onProgress`, `onWarning`, and throws diagnostics — semantics, not just signatures. |
| 3 | **Telemetry transport** | `fetch` + `AbortController` + flush-at-exit ordering. Go equivalent is straightforward but must preserve: disabled-by-env, one request per event, 5 s flush ceiling, abort after flush, and never fail the command. Server-side compatibility of param ordering/format is **assumed** stable. |
| 4 | **`process.stdout.write` monkey-patching for `--json`** | `runAdd` replaces `process.stdout.write` with a bound `stderr.write` for the duration of the run, restores it before printing, and installs a `process.once('exit')` safety net. Go cannot monkey-patch `os.Stdout` globally without care; the equivalent is an explicit output abstraction (writer injection) that must apply to *every* print site including `@clack` equivalents, or JSON mode will leak human text into stdout and break consumers. |
| 5 | **`AsyncLocalStorage` for warnings** (`src/warnings.ts`) | Lets the library capture warnings that the CLI would print, without threading a handler through every call. Go has `context.Context`: thread a warning sink in `ctx`, or use a package-level handler with a mutex (breaks nested/concurrent API calls). |
| 6 | **`node:util.parseArgs` token semantics** (`src/cli-args.ts`) | Grouped short flags (`-gy`), `--flag=value`, `-s=foo` → value `=foo` (then stripped), list options that consume following positionals until the next option, `--` terminator, unknown-option collection instead of failure. Go's `flag` package cannot do this; a hand-written parser must match, including the quirk that `--agent -y` reports "requires a value" rather than treating `-y` as the value. |
| 7 | **`os.homedir()` / `os.tmpdir()`** | Node on Windows prefers `USERPROFILE`, then `HOMEDRIVE`+`HOMEPATH`; `os.UserHomeDir` in Go uses `%USERPROFILE%` only. Node's `tmpdir()` honours `TMPDIR`/`TMP`/`TEMP`; Go's `os.TempDir()` does the same, but the *order* and the trailing-separator behaviour differ on Windows (**assumed**). Since the e2e env sets `TMP`/`TEMP`/`TMPDIR`, tests will catch the common case. |
| 8 | **`realpath` / `lstat` / `chmod` / `cp --dereference`** | `chmod(dest, mode & 0o777)` preserves exec bits and drops setuid; on Windows Node's chmod only toggles read-only. Go's `os.Chmod` on Windows is likewise read-only-only — parity is close, but `fs.cp` also preserves timestamps? (**assumed** not; the code never copies timestamps), and it creates parent dirs only when told to. |
| 9 | **`fs.promises.glob`** (sync) | Node 22's glob returns paths relative to `cwd`, honours `exclude`, and follows a specific `**` rule. Go needs `doublestar`-style semantics with the same `**` behaviour, or `sync --recursive` picks a different package set. |
| 10 | **`package.json` runtime read for `--version`** | The CLI reads `../package.json` relative to `dist/`, so a single-file Go binary has no such file. Embed the version at build time (`-ldflags -X`) and **also** keep the `0.0.0` fallback shape if you want error-path parity. |
| 11 | **Node compile cache** (`bin/cli.mjs`) | Pure startup optimisation; drop it. But note the shim is the **documented bin path** (`./bin/cli.mjs`) and is exercised by e2e, so the npm package layout must keep it if npm distribution continues. |
| 12 | **`@vercel/detect-agent`** | Detects agent sessions from ~19 env vars plus filesystem probes, and the CLI *changes UX* based on it (no banner, no prompts). A Go port needs its own copy of the env-var table **and** the extra Cursor refinement (`CURSOR_AGENT`, `CURSOR_EXTENSION_HOST_ROLE=agent-exec`), or `skills` will start prompting inside agent sessions. |
| 13 | **YAML library identity** | `yaml` v2 (YAML 1.2 core schema defaults, duplicate-key errors, merge keys) vs `gopkg.in/yaml.v3` (YAML 1.1-ish resolution for some scalars, different error messages). Frontmatter acceptance and error text will differ at the edges; the hash is unaffected (it hashes raw bytes), but *which skills are considered valid* is affected. |

---

## 3. Cross-compilation and platform pitfalls

### 3.1 32-bit x86 (Windows and Linux)

- `int` is 32-bit: the caps in the code go up to 100 MiB and 16/50 MiB — safe. But any
  arithmetic like `offset + length > buffer.length` (**observed** in `archive.ts`) must be
  done in `int64`/`uint64` in Go, because ZIP64 fields are `uint64` and a 32-bit `int`
  conversion truncates. The TS code guards with `Number.isSafeInteger` before use.
- `zlib`-based inflate: Go's `compress/flate` is pure Go and works on all targets; no cgo
  needed. Keep `CGO_ENABLED=0` so binaries are static and portable.
- Windows x86 Go binaries run on x64 Windows (WOW64); the reverse is not true. If you ship
  one binary per arch, make sure the npm/installer shim selects by `process.arch`/`GOARCH`
  or the OS loader fails with a confusing error.

### 3.2 Linux aarch64 and armhf

- `GOARCH=arm GOARM=6/7`: avoid 64-bit atomics on unaligned fields (use `atomic.Int64`
  which guarantees alignment) if you add counters; the current design has none, so this is
  only relevant if you add telemetry batching.
- Some armhf userlands lack `io_uring`/`openat2`; use plain `os` calls.
- Timeouts use `context`; Go's timer resolution on armhf is fine at these scales (seconds).
- `syscall.Stat_t` field widths differ (`Mtim` on darwin vs `Mtim`/`Mtimensec` on linux,
  `Mtimespec` on some) — if you implement stale-lock detection via `syscall.Stat_t`, the
  build tags differ per OS. Prefer `os.FileInfo.ModTime()`.

### 3.3 macOS Intel / Apple Silicon

- Two architectures: either build a universal binary (`lipo -create`) or ship two
  binaries + a shim. If the Go binary replaces the npm bin, npm's `bin` shim is a shell
  script/`.cmd`, and the `os`/`cpu` mapping is usually done with `optionalDependencies`
  (**assumed** distribution approach; nothing in this repo does it today).
- **Signing/notarisation is not visible anywhere in this repo** (**observed**: no
  `codesign`, `notarytool`, entitlements, or macOS signing step in `.github/workflows/`).
  A Go binary delivered to macOS users must be signed (Developer ID) and notarised, or
  Gatekeeper will block it; quarantine (`com.apple.quarantine`) applies to downloaded
  archives, not to files created by `npm`, so npm-delivered binaries may escape quarantine
  but are still subject to team/hardened-runtime rules on managed Macs (**assumed**).
  Whatever you choose, the release workflow must be extended; the current one only
  publishes the JS tarball.
- `realpath` on macOS resolves `/private/var` vs `/var`; `/tmp` is a symlink to
  `/private/tmp`. This is exactly why `cleanupTempDir` validates against `os.tmpdir()` after
  `normalize(resolve(...))` — note it does **not** use `realpath`, so `/tmp/x` passes while
  `/private/tmp/x` would fail the prefix check (**observed** logic, behaviour **assumed**).
  A Go port that "fixes" this by calling `EvalSymlinks` first changes the guard's semantics
  (and could start refusing legitimate temp dirs, or start allowing others).

### 3.4 Windows

- **Symlink/junction privilege** — see 1.4. Junctions need no privilege but are
  directory-only, must target a local volume, and cannot target a path with a trailing
  separator. `os.Symlink` needs Developer Mode/admin. Plan for junction-by-default plus a
  copy fallback, and surface the same "enable Developer Mode" hint only when a real
  symlink is required.
- **MAX_PATH (260)** — the current code sets `core.longpaths=true` for git and relies on
  Node's `\\?\`-aware fs layer. Go's `os` package automatically applies `\\?\` for long
  paths in most APIs (since 1.20), but `filepath.Abs`/`Clean` do not, and `os.Rename` across
  long paths can still fail. Deep skill trees (`skills/<name>/assets/...`) plus deep project
  dirs is a realistic failure mode; test with a >260-char path.
- **Case-insensitive filesystem vs case-sensitive string checks** — see 1.4. All containment
  checks are string-based.
- **Reserved names and characters** — `sanitizeName` can produce `con`, `nul`, `aux`,
  `com1`, `a.`/`a ` (well, trailing dots/hyphens are stripped, but `a.b` is kept). Creating
  `.agents/skills/con` fails on Windows. The TS code does not special-case these
  (**observed** absence) — it will surface a raw `ENOENT`/`EPERM`. Decide whether the Go
  port preserves or fixes this.
- **File locking / sharing** — the `.lock` mutex and the temp-rename both rely on Windows
  sharing semantics; `os.Rename` fails with `EPERM` while an antivirus or editor holds the
  destination. Keep the 5-attempt backoff.
- **`chmod`** — no-op except read-only; don't expect mode preservation to round-trip.
- **ANSI** — picocolors' win32 auto-enable is deliberately removed; keep it removed.
- **Console input** — `readline.emitKeypressEvents` maps Windows console input; Go must use
  `golang.org/x/term` raw mode and handle `\x00`/`\xe0` extended-key prefixes for arrows, or
  the picker breaks in `conhost` (**assumed** implementation detail of the port).
- **`gh`/`git`/`ntn` discovery** — `execFile` resolves through `PATHEXT`; `os/exec` does
  too on Windows, but the commands are `.cmd` wrappers for `gh` (npm shims). `gh` installed
  via npm is `gh.cmd`; `execFile('gh')` without `shell: true` works on Windows only because
  Node's `spawn` appends `PATHEXT` (**assumed**). Go's `exec.LookPath` does the same.
  `ntn` on Windows is likely a `.cmd`/`.exe` — verify.
- **stdout/stderr encoding** — Node writes UTF-8 bytes; Windows console decoding depends on
  the code page unless in UTF-8 mode. Go writes UTF-8 bytes too, so parity holds, but the
  box-drawing logo and `◆`/`│` glyphs will mojibake in a legacy code page for both
  implementations — acceptable parity, not a regression.

### 3.5 Static-analysis / build hygiene for the port

- Keep `CGO_ENABLED=0` for all eight targets so the binary has no libc dependency
  (glibc/musl on Linux, and no Xcode CLT requirement on macOS).
- `-trimpath` for reproducible builds; `-ldflags "-s -w -X main.version=…"`.
- Reproduce the "single source of truth for ANSI" rule (`color.test.ts` forbids raw
  escapes elsewhere): in Go, keep SGR constants in one package and add a `go vet`-style
  check or a lint rule, otherwise escape literals creep in and the `NO_COLOR` guarantee
  rots.

---

## 4. Upstream pain points and known defects visible in the repo

### 4.1 Observed (code comments, tests, docs)

| # | Item | Evidence |
| --- | --- | --- |
| 1 | git-lfs absent breaks checkout ("git-lfs: command not found", "remote end hung up"); mitigated by disabling the LFS filter and skipping smudge | `src/git.ts` comment, downstream report `heygen-com/hyperframes#407` |
| 2 | Translated git (`LANG=de_DE`) skips the SHA/auth fallbacks because messages are matched in English; mitigated with `LC_ALL=C` | `src/git.ts`, issue `#2505` |
| 3 | Windows `MAX_PATH` failures on checkout; mitigated with `core.longpaths=true` | `src/git.ts`, issue `vercel-labs/skills#2613` |
| 4 | Anonymous GitHub API rate limiting and private-repo 404 masked as not-found; mitigated by lazy token/`gh api` auth retries | `src/blob.ts`, issues `#523`, `#1318` |
| 5 | Agent "category directory" (e.g. `~/.hermes/skills/<category>/<skill>`) being destroyed by an install; mitigated with `SKILLS_CATEGORY_DIR_CONFLICT` and pre-flight checks on every target | `src/installer.ts`, `src/add.ts`, tests, issue `#1723` |
| 6 | `remove` deleting user-authored skills and global-lock-tracked skills; mitigated by "only unmodified copies we installed" checks and `SKILLS_REMOVE_GLOBALLY_TRACKED` | `src/remove.ts`, issues `#604`, `#2560`, `#1771`, `#2469` |
| 7 | Removing from one agent breaking another agent's links | `src/remove.ts`, issue `#287` |
| 8 | Removing the last agent link silently making a skill un-updatable | `src/remove.ts`, issue `#1718` |
| 9 | Proxy/mirror URLs embedding `github.com` in the path being rewritten to real GitHub | `src/source-parser.ts`, issue `#2079` |
| 10 | `--skill=<name>` not filtering | `src/cli-args.test.ts`, issue `#2039` |
| 11 | Removing a skill whose directory is itself a symlink | `src/remove.test.ts`, issue `#2679` |
| 12 | Windows libuv `UV_HANDLE_CLOSING` assertion when force-exiting with a telemetry socket in flight; mitigated by draining the event loop | `src/cli.ts` comment |
| 13 | Global lock is silently wiped when its version is older; users must reinstall | `src/skill-lock.ts`, `AGENTS.md` ("If reading an older lock file version, it's wiped") |
| 14 | `parseArgs` in non-strict mode consumes the next option as a value (`--agent -y`) and `-s=foo` yields `=foo`; both are worked around by hand | `src/cli-args.ts` |
| 15 | `find` multi-term queries hide relevant skills because the API requires every term; mitigated by term-drop fallback | `src/find.ts`, pending changeset `find-partial-matches` |
| 16 | Two divergent hash schemes with different sort orders, one of which must match a server implementation | `src/skill-hash.ts`, `src/skills-sh-hash.ts` comments ("keep in sync with the leaderboard's …") |
| 17 | `@vercel/detect-agent` treats any `CURSOR_TRACE_ID` as a Cursor agent, including the integrated terminal; narrowed by requiring stronger signals | `src/detect-agent.ts` |
| 18 | `skills use` passes the whole prompt as one argv element; also leaves its temp materialisation behind (the prompt points at it) | `src/use.ts` |
| 19 | `notion-test.ts` still carries a "-test" filename and prints `[notion] ntn …` to stderr unconditionally | `src/notion-test.ts` |
| 20 | Environment-variable documentation lists 6 vars while the code reads ~25 | `docs/troubleshooting.md` vs `src/*` |
| 21 | Disabled/errored telemetry is silently swallowed by design (no log, no metric) | `src/telemetry.ts` |

### 4.2 Assumed (plausible, not proven by tests or comments)

| # | Item | Reasoning |
| --- | --- | --- |
| A1 | Unbounded `Promise.all` over directory entries can hit `EMFILE` on large skill trees | `copyDirectory`, `collectFiles`, `computeLegacySkillFolderHash`, `detectInstalledAgents` have no concurrency cap; Node's default fd limits are modest |
| A2 | `isPathSafe` is case-sensitive on Windows and can both miss and over-reject containment | plain `startsWith` on `normalize(resolve(...))`; no `toLowerCase`, no `fs.realpath` in the check |
| A3 | `shrinkPath` prefix replacement can mis-shorten `/home/foo` vs `/home/foobar` | no `sep` boundary check in `shortenPath` |
| A4 | The lock mutex has a 5 s stale threshold, so a slow `fsync`/AV scan can cause two writers | stale-by-mtime only, no owner liveness check |
| A5 | Windows reserved device names (`con`, `nul`, `aux`) can be produced by `sanitizeName` and then fail to create | no reserved-name filtering observed |
| A6 | `String.padEnd` based table alignment misaligns on emoji/ZWJ content | UTF-16 counting, confirmed by reading `padEnd` call sites |
| A7 | `skills use --agent` can exceed OS argument limits for large skills | prompt is a single argv element; limits are OS-level |
| A8 | A translated or non-standard git (e.g. busybox, JGit-backed) changes the fallback behaviour | substring matching on three English messages |
| A9 | macOS notarisation is absent and would block a signed-binary distribution | no signing step in workflows |
| A10 | `experimental_sync`'s ownership heuristics can delete a user-authored skill that happens to live under `node_modules` and matches a lock entry | heuristics keyed on path shape + lock entry, not on content |

---

## 5. Risk table

Likelihood/Impact: H/M/L. "Mitigation" assumes a Go rewrite that must remain
interoperable with existing users' lock files and repos.

| # | Risk | Likelihood | Impact | Mitigation |
| --- | --- | --- | --- | --- |
| 1 | Skill content hash diverges (CRLF rule, NUL-test, path sort, exclusion set, UTF-8 framing) → every existing `skills-lock.json` is reported "modified", `--frozen-lockfile` fails, `update` reinstalls unnecessarily | H | H | Port `skill-hash.ts` literally; add the README's three test vectors plus CRLF, binary-with-NUL, nested `metadata.json`, symlink-in-skill, and non-BMP filename cases; fuzz against the TS implementation's outputs before cutover |
| 2 | `skills-sh-tree-v2` hash diverges → pack pins never match, silently falling back to the pack's copy (looks like "works", but installs the wrong content) | M | H | Implement separately from (1); golden tests from real skills.sh packs; document the "mismatch is safe" fallback so it is not mistaken for success |
| 3 | Windows symlink/junction handling differs (privilege, absolute target, `realpath` equivalence) → installs fail, or removal deletes the canonical directory | H | H | Junction via reparse-point ioctl or `mklink /J`; replicate the realpath-equality early return; CI test on `windows-latest` with both a junction and a Developer-Mode symlink; never `os.RemoveAll` a path reached through a junction without an `Lstat` guard |
| 4 | Lock file byte format differs (key order, field order, trailing newline, local-source rel/abs rule) → noisy diffs, merge conflicts, and `--frozen-lockfile` churn for every user | H | M | Golden-file tests with CRLF/LF and non-ASCII names; compare against a lock written by the TS CLI; keep UTF-16 key ordering only if bit-parity matters, and document the choice |
| 5 | YAML frontmatter parsed differently (`name: 123`, YAML 1.1 vs 1.2 scalars, duplicate keys, BOM, CRLF, multi-line `name`) → skills silently skipped or renamed differently | M | H | Use one YAML library, write a conformance table against `yaml` v2 for the fixture corpus; keep `setFrontmatterName` byte-preserving including continuation-line deletion and EOL style |
| 6 | git auth fallbacks break (English-only message matching, `LC_ALL=C` forgotten, `gh`/SSH ordering changed, SHA-fetch path dropped) → private repos stop installing for exactly the users who depend on the feature | M | H | Keep subprocess git with the identical env and fallback chain; test against a local bare repo with an auth-required path and a full-SHA ref; never replace with a pure-Go git implementation |
| 7 | Archive extraction becomes unsafe or too strict (missing `..`/absolute/link rejection, ZIP64, CP437 + Unicode-extra filenames, missing size/entry caps) | M | H | Port `archive.ts` validators 1:1; keep the caps and their env overrides; add the existing ZIP fixture matrix (encrypted, multi-disk, zip64, unicode extra, link) to Go tests; treat "rejected archive" vs "not an archive" as distinct errors |
| 8 | Terminal prompt breaks or hangs (raw mode, ConPTY, `columns` fallback, width table, non-TTY hang in CI) | M | M | Use `golang.org/x/term`; reproduce the wcwidth table and row math; hard-require TTY for prompts and route every non-interactive flag; add a pipe-stdin test asserting a clean error and exit 1 |
| 9 | ANSI/colour regressions (win32 auto-enable, `CI` enabling colour, `NO_COLOR`, untrusted text not stripped) | M | M | Centralise SGR constants, forbid raw escapes by lint; port `colorEnabled` verbatim; keep `stripTerminalEscapes` on every untrusted string; golden CLI output tests with `NO_COLOR=1` and `CI=1` |
| 10 | `node_modules` sync cannot be reproduced (glob semantics, workspaces, ownership heuristics) → `experimental_sync` is dropped or mis-deletes | M | H | Decide explicitly: implement glob + workspace discovery with `doublestar`-style `**` and port `installedBySync`/`linksIntoNodeModules` guards, or ship `sync` as unsupported with a clear diagnostic and keep the TS package for it |
| 11 | JS `--json` consumers break because stdout is polluted or the array is emitted twice / not at all | M | M | Introduce an explicit out/err writer pair in every command; assert "exactly one JSON value on stdout" for success, usage error, install failure, and panic paths |
| 12 | Sync/update/install now run in parallel differently → duplicate installs, torn lock writes, or lost entries | L | H | Keep the `.lock` mutex file and the bounded fan-out (4, `SKILLS_CLONE_CONCURRENCY`); port `mapConcurrent` semantics including result ordering; add a two-process race test |
| 13 | Exit codes drift (cancel=0 vs 1, `check`=1, frozen=1, agent exit code propagation) → CI scripts misclassify results | M | M | Tabulate the exit-code matrix as a test; run it as a table-driven test over the built binary |
| 14 | Telemetry/audit accidentally leaks private repo identifiers, or is impossible to disable | L | H | Port the `true/false/null` privacy tri-state and the `null` ⇒ omit rule; keep `DISABLE_TELEMETRY`/`DO_NOT_TRACK` as hard gates; add a test asserting no identifiers are sent for the private/unknown case |
| 15 | Version/reporting and public API mismatch after the port (`--version`, npm `exports`, nine library exports) → tooling and downstream agents break | M | M | Keep the npm package if JS consumers exist (documented decision); embed the Go version via `-ldflags`; reproduce `--version` output exactly |
| 16 | 32-bit/arm builds fail at runtime (int truncation in ZIP64 offsets, 64-bit atomics, unaligned access) | L | H | Use `int64`/`uint64` for archive arithmetic; `CGO_ENABLED=0`; build all eight targets in CI and run a smoke test (help, init, add from a local path, hash a fixture) under QEMU for arm64/arm |
| 17 | macOS distribution blocked (unsigned/unnotarised binary, or universal-binary packaging bug) | M | H | Add codesign + notarisation to the release pipeline; produce `lipo` universal or arch-specific artifacts with an arch-selecting shim; test on both `macos-13` (Intel) and `macos-14` (arm64) runners |
| 18 | Temp-dir cleanup guard behaves differently (`/tmp` vs `/private/tmp`, symlinked temp roots) → either refuses legitimate cleanup or deletes outside temp | L | M | Replicate the `normalize(resolve)` prefix check as written, then add a deliberate, documented improvement (EvalSymlinks) rather than an accidental one; test on macOS |

---

## Open questions

1. **Distribution**: does the Go binary replace the npm package, ship alongside it, or is the
   npm package retired? This decides whether items 2 (library API), 11 (`--json`), and 15
   are hard requirements or moot.
2. **Lock-file bit-parity**: must a Go-written `skills-lock.json` be byte-identical to a
   Node-written one (UTF-16 key ordering, field order, exact JSON spacing)? If teams mix
   both CLIs, this becomes a hard requirement rather than cosmetic.
3. **V1 lock migration**: is `localeCompare`-based legacy hashing required, or can v1 locks
   be treated as "unverifiable" (empty hash) as the code already does when disk content
   does not match?
4. **`experimental_sync`**: keep, port fully, or drop? It is the single largest Node-specific
   surface (`node_modules`, workspaces, glob semantics).
5. **Telemetry**: does the Go port keep the same endpoints and wire format, or is it
   expected to be disabled/removed for a first-party Go tool? The audit feature is part of
   user-visible output (`security` block in `--json`, advisory table in the human UI).
6. **`skills use` prompt delivery**: is piping on stdout (and passing the prompt as argv with
   `--agent`) contractual for downstream agent integrations, or may the port use stdin?
7. **Windows symlink policy**: junction by default plus copy fallback (current behaviour), or
   require Developer Mode and always use real symlinks? This affects whether `--copy`
   remains meaningful.
8. **Reserved-name and case-insensitivity fixes**: should the port fix Windows reserved
   device names and case-sensitive containment checks, or stay bug-for-bug compatible?
9. **Signing/notarisation**: is there an Apple Developer ID available for the project, and
   who owns the release workflow change?
10. **Test oracle**: is running the TS CLI in a container as a differential oracle (same
    inputs → same locks, same stdout) part of the acceptance criteria, and on which
    platforms?
11. **macOS Intel support horizon**: is `GOARCH=amd64` for macOS still required, or is
    Apple Silicon + Rosetta acceptable (which removes one build target and one notarisation
    artefact)?
12. **Prompt UX parity**: must the custom multiselect (groups, locked section, detail pane,
    width-aware redraw) be reproduced, or is a simpler prompt acceptable for the Go port?
