# 02 — Sources, refs, and fetching

Behavioral specification of how `skills` (vercel-labs/skills v1.9.0) turns a single
source argument into a directory (or an in-memory file set) containing one or more
skills.

Scope: `src/source-parser.ts`, `src/download-source.ts`, `src/archive.ts`, `src/git.ts`,
`src/github-host.ts`, `src/blob.ts`, `src/providers/*`, `src/notion-test.ts`,
`src/constants.ts`, plus the dispatch code in `src/add.ts` and the tests that cover
fetching. Installation, hashing/verification of installed files, and agent targets are
out of scope (see the sibling docs).

Every claim is tagged **observed** (read in code, tests, or docs) or **assumed**
(inferred, not verified end-to-end). Assumptions are also collected in *Open questions*.

---

## 1. Where this layer sits

**observed** — the pipeline for `skills add <source>`:

```
argv[0] ──► notion intercept (bare "notion", notion.so/notion.com page URL)
        ──► parseSource()            → ParsedSource
        ──► GitHub repo lookup       → privacy flag + canonical owner/repo (async, parallel)
        ──► fetch well-known?        → provider.wellKnownProvider.discoverSkills()
        ──► resolveSkills(parsed)    → BlobResult | clone | download → discoverSkills()
        ──► install, lock, telemetry
```

`runAdd` accepts exactly **one** source (`args[0]`, `src/add.ts:2458-2459`). Multiple
sources only exist inside `fetchSources()` (`src/add.ts:2075-2088`), used by
`update` / `experimental_install` to fetch several lock entries concurrently.

Module responsibilities (**observed**):

| Module | Role |
| --- | --- |
| `source-parser.ts` | String → `ParsedSource`. No network, no git. Also GitHub API repo lookup, subpath sanitizing, alias table. |
| `add.ts` (`resolveSkills`, `fetchSource`, `fetchSources`) | Decides *how* to materialize a `ParsedSource`: blob snapshot, git clone, or HTTP download. |
| `blob.ts` | GitHub Trees/commits API + skills.sh snapshot API fast path. |
| `git.ts` | `git clone` wrapper, `gh` fallback, SSH fallback, commit resolution, temp-dir cleanup. |
| `download-source.ts` | Direct HTTP download → SKILL.md file or extracted archive. |
| `archive.ts` | Hand-written ZIP reader (ZIP64, CRC, CP437/UTF-8 names). |
| `providers/wellknown.ts` | RFC 8615 `.well-known` discovery provider (v0.1.0 and v0.2.0). |
| `providers/registry.ts` | Provider registry — **no callers in v1.9.0** (see §16). |
| `notion-test.ts` | `ntn` CLI integration for Notion pages and packs. |
| `concurrency.ts` | `mapConcurrent` (default 4 in flight). |

---

## 2. The `ParsedSource` contract

**observed** — `src/types.ts:121-129`:

```ts
type ParsedSource = {
  type: 'github' | 'gitlab' | 'git' | 'local' | 'well-known' | 'download';
  url: string;        // normalized clone URL, resolved local path, or download URL
  subpath?: string;
  localPath?: string; // same value as url, local only
  ref?: string;       // branch, tag, or full 40-hex SHA
  skillFilter?: string; // owner/repo@name
}
```

Notes that matter for a port (**observed**):

* `github` vs `git` is *not* about the host family alone: a shorthand resolved through a
  non-`github.com` `GH_HOST` becomes `git` (`src/source-parser.ts:688-689`), and an explicit
  GitHub Enterprise URL becomes `git` (`:554-578`). Only `github` sources get the blob fast
  path and the GitHub API repo lookup.
* `gitlab` is used for `gitlab.com` and for any host with GitLab's `/-/tree/` URL shape.
* `download` is used only for *hosted artifact* URLs (§4.11). Generic `https://…/x.zip`
  is `well-known`, not `download`, and is resolved by the download path as a fallback.
* `subpath` is always stored with `/` separators and is **not** normalized beyond
  `..`-segment rejection (§4.13).

---

## 3. Parse order (precedence)

**observed** — `parseSource()` returns at the first match, in this order
(`src/source-parser.ts:498-741`):

1. **Local path** — `isAbsolute`, `./`, `../`, `.`, `..`, or `^[A-Za-z]:[/\\]`
   (`:325-335`). Resolved against `options.cwd ?? process.cwd()`; returns
   `{type:'local', url: resolved}` **without checking existence** (`:503-511`).
   Existence is checked later by the caller.
2. **Fragment split** — see §3.1. Happens *before* aliases and prefixes.
3. **Source aliases** — exact-string map applied to the fragment-stripped input
   (`:343-346`, `:520-524`): `coinbase/agentWallet` → `coinbase/agentic-wallet-skills`,
   `vercel-labs/vercel-skills` → `vercel-labs/agent-skills`.
4. `github:<rest>` → recurse on `<rest>` with the parsed fragment re-appended (`:526-531`).
5. `gitlab:<rest>` → recurse on `https://gitlab.com/<rest>` (`:533-543`).
6. **Hosted artifact URL** (`raw.githubusercontent.com`, `codeload.github.com`,
   `objects.githubusercontent.com`, `github.com/{o}/{r}/{archive|raw|releases/download|releases/latest/download}/…`,
   `gitlab.com/…/-/{archive|raw}/…`) → `download` (`:465-492`, `:545-552`).
7. **Explicit GitHub Enterprise URL** when `GH_HOST != github.com` → `git` (`:554-578`).
8. **`git@host:owner/repo[/tree/ref[/subpath]]`** on a GitHub host → `git` (`:580-592`).
9. **`github.com` host** (scheme optional, `www.` allowed, query ignored) → `github`
   (`:594-620`).
10. **GitLab `/-/tree/ref/path`** on any host except `github.com` → `gitlab` (`:622-638`).
11. **GitLab `/-/tree/ref`** on any host except `github.com` → `gitlab` (`:640-651`).
12. **`gitlab.com/{group…}/{repo}`** → `gitlab` (`:653-667`).
13. **Proxy/mirror path** embedding `github.com` or `gitlab.com` → `git` on the proxy
    host (`:669-674`).
14. **Azure Repos** path containing `/_git/{repo}` on any host → `git` (`:676-681`).
15. **Shorthand `owner/repo@name`** → `github`/`git` (`:691-711`).
16. **Shorthand `owner/repo[/subpath]`** → `github`/`git` (`:713-723`).
17. **Arbitrary `http(s)://`** (not github.com / gitlab.com / raw.githubusercontent.com,
    not ending in `.git`) → `well-known` (`:725-733`, `:748-771`).
18. **Fallback** → `git` with the input as `url` (`:735-740`). This catches `ssh://…`,
    `https://host/x.git`, and anything else that reached here (including `npm:…`).

### 3.1 Fragment handling (`#ref` and `#ref@skill`)

**observed** — `parseFragmentRef` (`:414-444`):

* Split at the first `#`. Empty fragment → unchanged input.
* The fragment is treated as a git ref **only if** `looksLikeGitSource(inputWithoutFragment)`
  (`:362-412`): `github:`/`gitlab:`/`git@` prefixes, `ssh://…*.git`, `http(s)` URLs whose
  path is a GitHub/GitLab repo or tree shape, a mirrored repo path, an Azure `/_git/` path,
  `http(s)…*.git`, or a bare `owner/repo[...]` shape.
  Otherwise the `#…` stays part of the URL (`:425-427`). Consequence: a fragment on a
  generic docs URL is *not* a ref; a fragment on GitHub *blob* URLs is also not a ref
  (`/blob/…` fails the repo/tree shape test) — covered by
  `src/source-parser.test.ts:144-150`.
* Inside the fragment, the first `@` splits `ref` from `skillFilter`; each side is
  percent-decoded, failures keep the raw text (`:429-443`). `#main@code%20review` →
  ref `main`, filter `code review`.
* Fragment values are re-appended percent-encoded when recursing into `github:`/`gitlab:`
  (`:458-463`), so `github:owner/repo#release%40v1@review` round-trips
  (`src/source-parser.test.ts:9-31`).

### 3.2 `@` in shorthand: skill name vs version pin

**observed** — `:446-456`, `:691-711`:

* `isVersionPin(value)` is true for `v?N(.N)+` with optional `-prerelease` /
  `+build`, or a full 40-hex SHA.
* Pin applies **only if** the input had no `#ref` and no `#…@filter`
  (`!fragmentRef && !fragmentSkillFilter`). Then `owner/repo@v1.2.3` →
  `ref: 'v1.2.3'`, no `skillFilter`.
* Otherwise `@name` is a `skillFilter`. Deliberate non-pins: `v2`, `2`, `v1.x`,
  `find-skills` (`tests/source-parser.test.ts:400-406`). A single number is treated as
  ambiguous and stays a skill name (`:449-451`).
* No validation against the repo: a typo like `owner/repo@v9.9.9` produces a ref and
  fails later at clone time.

---

## 4. Accepted source forms

All rows below are **observed** (tests in `src/source-parser.test.ts`,
`tests/source-parser.test.ts`, plus the code paths cited in §3).

### 4.1 Local paths

| Input | Result |
| --- | --- |
| `./my-skills`, `../x`, `.`, `..` | `local`, `url`/`localPath` = resolved absolute path |
| `/home/u/skills` | `local` |
| `C:\Users\u\skills`, `D:/x` | `local` (Windows drive regex) |

No existence check at parse time; `add` later errors with
`Local path does not exist: <path>` (`src/add.ts:2681-2690`, diagnostics `:117-119`).
A local path can be a skill directory (SKILL.md directly inside → one skill, §6) or a
catalog directory (many skills).

### 4.2 GitHub shorthand

| Input | type | url | ref | subpath | skillFilter |
| --- | --- | --- | --- | --- | --- |
| `owner/repo` | github | `https://github.com/owner/repo.git` | – | – | – |
| `owner/repo/` | github | same | – | – | – |
| `owner/repo/skills/x` | github | same | – | `skills/x` | – |
| `owner/repo@my-skill` | github | same | – | – | `my-skill` |
| `owner/repo@v1.3.1` | github | same | `v1.3.1` | – | – |
| `owner/repo@<40-hex>` | github | same | SHA | – | – |
| `owner/repo#branch` | github | same | `branch` | – | – |
| `owner/repo/skills/x#feature/y` | github | same | `feature/y` | `skills/x` | – |
| `owner/repo#branch@skill` | github | same | `branch` | – | `skill` |

With `GH_HOST=github.example.com` the same inputs become `type: 'git'` and
`https://github.example.com/owner/repo.git` (`src/source-parser.test.ts:180-198`).
`GH_HOST` is validated (`src/github-host.ts:10-32`): a value with userinfo, a port, a
path, query, or fragment is rejected and `github.com` is used instead; only the hostname
survives.

### 4.3 Prefix shorthands

* `github:<rest>` — strips the prefix and re-parses `<rest>`; supports subpath, `@skill`,
  and fragments (`:526-531`; `src/source-parser.test.ts:698-733`).
* `gitlab:<rest>` — becomes `https://gitlab.com/<rest>` (`:533-543`), so subgroups work.

### 4.4 GitHub URLs

Accepted shapes (scheme optional, `www.` allowed, host matched case-insensitively,
`?query` dropped, trailing `/` tolerated — `:236-244`, `:596-620`):

* `https://github.com/o/r`
* `https://github.com/o/r.git`
* `https://github.com/o/r/tree/<ref>`
* `https://github.com/o/r/tree/<ref>/<subpath>`
* `https://github.com/o/r/blob/<ref>/<file>[#L10]` → repo only, **no ref**
* `github.com/o/r?tab=readme-ov-file` → repo only

**Ambiguity (documented in tests):** in a tree URL the *first* segment after `tree/` is
the ref, everything else is the subpath. `…/tree/feature/my-feature` →
ref `feature`, subpath `my-feature` (`tests/source-parser.test.ts:61-70`). Slash-containing
branch names therefore cannot be expressed in a tree URL; `#ref` can carry them.

Any extra path on a non-tree URL (`github.com/o/r/anything`) is **discarded**, not treated
as a subpath (`:610-619`).

### 4.5 GitHub Enterprise

* If `GH_HOST` is set and a URL's host is that host (and not `github.com`), the result is
  `type: 'git'`, `url: <protocol>//<host>/<owner>/<repo>.git`, with ref/subpath taken from
  `tree/<ref>/<subpath>` or from the `#fragment` (`:554-578`). A `skillFilter` from a
  fragment is **dropped** on this path (observed: the returned object omits it).
* An explicit `https://github.com/...` URL is never rewritten to `GH_HOST`
  (`src/source-parser.test.ts:213-220`).
* Enterprise sources deliberately skip the blob fast path and the GitHub.com API repo
  lookup (`getOwnerRepo` still works, but `tryBlobInstall` is only reached for
  `type === 'github'`).

### 4.6 `git@host:…` (scp-like SSH)

* `git@github.com:o/r.git` → `git`, ref/subpath unaffected.
* `git@<github-host>:o/r[/tree/<ref>[/<subpath>]]` → ref and subpath extracted; the
  `.git` suffix is normalized in (`:580-592`).
* `git@other-host:group/repo/tree/x.git` → left verbatim as a `git` URL
  (`tests/source-parser.test.ts:498-503`); the `/tree/` path is *not* interpreted.

### 4.7 `ssh://` and generic git

* `ssh://git@host:7999/o/r.git` → `git`.
* `https://git.example.com/o/r.git` → `git`.
* Anything that ends in `.git` or has a colon and is not matched earlier → `git` with the
  raw string as URL.
* `#ref` works for all of the above (`tests/source-parser.test.ts:517-529`).

### 4.8 GitLab

* `https://gitlab.com/o/r` → `gitlab`, `https://gitlab.com/o/r.git`.
* `https://gitlab.com/group/sub/repo` (nested subgroups) → `gitlab`, full path preserved.
* `…/-/tree/<ref>` and `…/-/tree/<ref>/<subpath>` → ref + subpath; `.git` stripped from
  the repo path; works on **any** host (`https://git.corp.com:8443/group/sub/proj/-/tree/main/src`)
  except that a `github.com` host is excluded (`:622-651`,
  `src/source-parser.test.ts:33-77`).
* `~user/project` personal namespaces are accepted (`:70-76`).
* A URL ending in `.git` inside the `gitlab.com` branch keeps its `.git`
  (`tests/source-parser.test.ts:97-121`).

### 4.9 Azure Repos

Marker: a path segment `_git` with a non-empty segment after it, on **any** host
(`dev.azure.com`, `*.visualstudio.com`, Azure DevOps Server with `/tfs/` or a collection
path) (`:180-230`).

* Clone URL = `protocol//host` + segments up to `_git` + `_git` + repo (each segment
  percent-re-encoded, `.git` suffix stripped). Query, fragment, and trailing slash are
  dropped.
* `?version=GB<branch>` and `?version=GT<tag>` become `ref`; `?version=GC<sha>` is
  **ignored** (no ref) because `git clone --branch` cannot check out a SHA (`:161-173`).
* `?path=/skills/x` becomes `subpath` (leading slashes stripped, `..` rejected).
* `#fragment` is the ref fallback when `version` yields nothing (`:219`).

### 4.10 Proxy / mirror URLs

When the *path* embeds `github.com` or `gitlab.com` (optionally with a nested
`https://`), the clone URL stays on the proxy host (`:251-320`):

| Input | Result url | ref | subpath |
| --- | --- | --- | --- |
| `https://gh-proxy.org/github.com/o/r` | `https://gh-proxy.org/github.com/o/r.git` | – | – |
| `https://gh-proxy.org/github.com/o/r/tree/main/skills/x` | `…/o/r.git` | `main` | `skills/x` |
| `https://gh-proxy.org/https://github.com/o/r/` | `…/o/r.git` | – | – |
| `https://mirror.example.com:8443/gitlab.com/group/sub/repo.git` | unchanged | – | – |

The proxy repo path is matched greedily for GitHub (`/owner/repo`) and non-greedily for
GitLab (subgroups). `#ref@skill` works. Query and fragment are cleared from the clone URL.
A proxied *archive* URL (`https://gh-proxy.org/https://github.com/o/r/archive/main.zip`) is
**not** a mirror and falls through to `well-known` (`tests/source-parser.test.ts:295-298`).

### 4.11 Hosted artifacts → `download`

`type: 'download'` is returned for (`:465-492`):

* any URL on `raw.githubusercontent.com`, `codeload.github.com`,
  `objects.githubusercontent.com`;
* `github.com/o/r/archive/…`, `github.com/o/r/raw/…`,
  `github.com/o/r/releases/download/…`, `github.com/o/r/releases/latest/download/…`;
* `gitlab.com/…/-/archive/…`, `gitlab.com/…/-/raw/…`.

URL kept exactly as given. Downloads bypass well-known discovery entirely
(`src/add.ts:2634`, `:1677-1688`).

### 4.12 Arbitrary HTTP(S) → `well-known`, then download

Anything `http(s)://` on another host that does not end in `.git` becomes `well-known`
(`:725-733`, `:748-771`). At runtime `add` first asks the well-known provider; if it
returns no skills, it downloads the same URL as a SKILL.md or archive
(`src/add.ts:2649-2660`, `:1677-1688`). So `.zip`/`.tar.gz` URLs on arbitrary hosts are
parsed as `well-known` even though they end up as downloads.

Excluded from well-known parsing: `github.com`, `gitlab.com`, `raw.githubusercontent.com`,
and anything ending in `.git` (`:757-765`).

### 4.13 Subpath safety

**observed** — `sanitizeSubpath` (`:145-158`) normalizes `\` → `/` *only for the check*,
throws `Unsafe subpath: "<subpath>" contains path traversal segments. Subpaths must not
contain ".." components.` if any segment equals `..`, and then returns the **original**
string (backslashes preserved). Dots that are not a `..` segment are allowed
(`.hidden`, `..skill`, `skill..`) — `tests/subpath-traversal.test.ts:33-39`.

Second line of defence at discovery time: `isSubpathSafe` compares resolved paths and
throws `Invalid subpath: "<subpath>" resolves outside the repository directory…`
(`src/skills.ts:169-174`, `:201-204`; `SKILLS_SUBPATH_OUTSIDE_REPO`).

Traversal is rejected in every form that carries a subpath: GitHub/GitLab tree URLs,
`git@…/tree/…`, shorthand, Azure `?path=`, mirrored repo paths
(`tests/subpath-traversal.test.ts:67-125`).

### 4.14 Notion

**observed** — handled *before* `parseSource` (`src/add.ts:2608-2628`):

* `notion` (case-insensitive, whole argument) → interactive/default pack selection.
* A Notion page URL: host matches `(^|\.)notion\.(so|com)$`, last non-empty path segment
  ends with a 32-hex id or a canonical UUID (regex `(?:^|-)([0-9a-f]{32}|8-4-4-4-12)$`),
  decoded before matching. The id is normalized to dashed UUID form
  (`src/notion-test.ts:322-360`).
* Everything else on notion.so that does not match falls through to normal parsing
  (i.e. `well-known`).

If `ntn` is missing → `SKILLS_NOTION_CLI_MISSING`; other failures map to the
`SKILLS_NOTION_*` family (`src/diagnostics.ts:257-313`). API version string is
`2026-03-11`; endpoints `/v1/ai/plugins`, `/v1/ai/plugins/<id>`, `/v1/ai/skills/<pageId>`,
`/v1/users/me` (`src/notion-test.ts:15`, `:246-320`). Downloads use
`downloadSource` with **larger** limits: 50 MiB download / 100 MiB extracted / 5000 files
(`src/notion-test.ts:21-23`, `:171-178`). Those options are only the *fallback* for the
env vars (§10.1), so an exported `SKILLS_DOWNLOAD_MAX_BYTES` (etc.) still wins.

Lock URLs: a page → `https://www.notion.so/<32-hex>`, a pack → `notion://packs/<encoded id>`
because the `ntn` download URL expires (`src/notion-test.ts:362-381`;
`docs/commands.md:113`).

### 4.15 `npm:` — never reaches this layer

**observed** — `npm:<package>` entries in a `skills` field are parsed by
`src/skills-field.ts:32`, and resolved by `sync.ts` against installed packages
(`node_modules`); they never become a `ParsedSource`
(`src/sync.ts:236-330`, `src/skills-field.ts:41-94`). If an `npm:` string were passed to
`parseSource`, it contains `:` so shorthand is skipped, it is not HTTP, and it falls to the
`git` fallback with `url: "npm:foo"`, which then fails at clone time. A port that accepts
`npm:` at the CLI would therefore diverge from v1.9.0.

---

## 5. Refs, tags, and commits

**observed**

* Nothing is resolved to a commit at parse time. `ref` is passed verbatim to
  `git clone --depth 1 --branch <ref>` (`src/git.ts:307`).
* Precedence when several ref sources exist:
  1. `ref` embedded in the URL path (`/tree/<ref>`, mirrored `/tree/<ref>`);
  2. `version=GB|GT…` for Azure, then the `#fragment`;
  3. `@version` shorthand pin;
  4. none → the remote's default branch.
* `git clone --branch` resolves branches **and** tags but not bare SHAs. A ref that is a
  full 40-hex SHA triggers a second attempt *only* when the first failed with a
  "missing ref" message (§11.3). Branch/tag detection is never inferred from the string
  (a branch literally named `deadbeef` works via `--branch`) — `src/git.ts:20-33`.
* Commit recording:
  * blob fast path: `fetchTreeCommit` asks
    `repos/<o>/<r>/commits?per_page=1[&sha=<branch>]` and accepts the SHA only when it
    matches the tree that was used (`sha === tree.sha` or
    `commit.tree.sha === tree.sha`), so a moved branch yields **no** commit
    (`src/blob.ts:294-323`; `tests/blob-fetch-tree-auth.test.ts:293-329`).
  * clone path: `git rev-parse --verify --end-of-options HEAD^{commit}`
    (`src/git.ts:401-428`).
* Restore pins the commit: `install` builds the source as `…/<folder>#<commit>` and keeps
  the *original* ref in the lock (`src/install.ts:140-147`), so the clone again goes
  through the `--branch <sha>` → SHA-fetch fallback. Requires server support for fetching
  a commit by SHA (README:108-111).
* `update` moves `commit` forward; `update`/`install` never rewrite `ref` to a SHA
  (`tests/lock-commit-pin.test.ts:60-116`).
* Effective default-branch probing order for the blob fast path when no ref is given:
  `HEAD`, `main`, `master` (`src/blob.ts:280`). `HEAD` wins when the API answers it.

---

## 6. Discovery: many skills vs a path that *is* a skill

**observed** — `discoverSkills(basePath, subpath, options)` (`src/skills.ts:189-340`):

1. `searchPath = subpath ? join(basePath, subpath) : basePath`.
2. If `searchPath/SKILL.md` exists and parses → **exactly one skill**, returned
   immediately, unless `fullDepth` is set or the skill is a "installed project skill"
   recorded in the source's own `skills-lock.json` (`:242-258`, `:180-187`,
   `:221-233`). This is the "URL points directly at one skill" case.
3. Otherwise walk priority containers: `searchPath`, `searchPath/skills`,
   `skills/.curated`, `skills/.experimental`, `skills/.system`, every
   `AGENT_PROJECT_SKILL_DIRS` entry, plus containers declared in `skills.sh.json`
   (`:261-282`).
4. Depth: the repo root and plugin-manifest dirs are walked **1 level**; every other
   container is walked up to `DEFAULT_SKILL_CONTAINER_DEPTH = 3` levels
   (`src/constants.ts:6`, `src/skills.ts:279`, `:316-319`). Walking stops below a
   directory that already has a `SKILL.md`. Ignored dirs: `node_modules`, `.git`, `dist`,
   `build`, `__pycache__`.
5. Deduplicate by skill `name` (first wins) unless `includeDuplicateNames`.
6. If nothing was found (or `fullDepth`), recurse the whole tree to depth 5
   (`findSkillDirs`, `src/skills.ts:131-153`, `:321-337`).
7. `add` retries with `fullDepth: true` when a requested `--skill` name was not found
   (`src/add.ts:1660-1671`).

`skills.sh.json` `paths` (repo-declared extra containers) are normalized:
must start with `./`, must not escape the repo, must not be the root; results are
`tools/skills`, `a/b` style paths (`src/skills-sh-config.ts`, exercised by
`tests/skills-sh-paths.test.ts:50-84`). A `SKILL.md` directly inside a declared container
is **not** a skill (`tests/skills-sh-paths.test.ts:138-146`).

Frontmatter requirements for a directory to count as a skill: `name` and `description`
present, strings, non-empty after sanitizing, plus optional `metadata.internal`
(`src/skills.ts:86-127`). Invalid candidates are skipped with a warning, not a failure.

The blob fast path reimplements the same priority logic over the API tree
(`src/blob.ts:353-469`): root `SKILL.md`, one-level skills, and 2–3-level skills under
containers, with a depth-6 fallback. The two implementations are kept in sync by
convention only (comment at `src/blob.ts:390-395`).

---

## 7. How a source is materialized (dispatch)

**observed** — `resolveSkills` (`src/add.ts:1641-1770`):

| `parsed.type` | Behavior |
| --- | --- |
| `local` | `discoverSkills(localPath, subpath, …)`. Missing path has already exited with `SKILLS_LOCAL_PATH_NOT_FOUND`. |
| `well-known` | Inside `resolveSkills`: `downloadSource(url)`. The well-known attempt happens one level up, in `add` and `fetchSource`, before `resolveSkills` is called; a `well-known` source that reaches `resolveSkills` has already failed discovery. Notion lock URLs (`type: 'download'`) go through `downloadNotionUrl` first. |
| `download` | `downloadSource(url)` → temp dir → `discoverSkills`. |
| `github` and **not** `fullDepth`, owner/repo eligible | `tryBlobInstall`; on null → `cloneRepo`; commit = blob commit or `HEAD`. |
| everything else (`gitlab`, `git`, `github` + `--full-depth`, ineligible owner) | `cloneRepo` then `discoverSkills`. |

Well-known is attempted **before** `resolveSkills` in the `add` flow
(`src/add.ts:2651-2660`), and the early `--list` path stops there
(`src/add.ts:1012-1025`).

Eligibility for the blob fast path (`src/add.ts:1689-1712`, `src/blob.ts:55-60`,
`:623-645`):

* `parsed.type === 'github'`; **no** explicit `ref` (a pinned ref returns `null`
  immediately); owner exactly `owner/repo` (so GitLab subgroups never qualify because
  `parseOwnerRepo` requires exactly one slash);
* owner (lowercased) in `['vercel', 'vercel-labs', 'heygen-com', 'remotion-dev']`, or the
  repo is in `BLOB_ALLOWED_REPOS` (`zapier/connectors`, self-hosted download URL);
* not `--full-depth`.

Parallelism: `fetchSources` maps with at most 4 concurrent fetches
(`src/concurrency.ts`), preserving input order; one failing source does not stop others
(`tests/parallel-source-fetch.test.ts:170-184`).

---

## 8. Blob fast path (`src/blob.ts`)

**observed**

Purpose: install from pre-built snapshots on skills.sh instead of cloning. Steps
(`:605-779`):

1. `GET {api}/repos/<o>/<r>/git/trees/<ref|HEAD|main|master>?recursive=1`
   (`:275-292`). API base is `https://api.github.com`, or
   `https://<GH_HOST>/api/v3` for Enterprise (`:125-127`).
2. If the tree contains `skills.sh.json` at the search root, fetch it from
   `raw.githubusercontent.com` and read `paths` (`:498-509`).
3. `findSkillMdPaths(tree, subpath, extraContainers)` (`:370-469`).
4. Fetch each `SKILL.md` from
   `https://raw.githubusercontent.com/<o>/<r>/<branch>/<path>` to read frontmatter and
   derive the slug (`:477-492`).
5. Slug = lowercase, whitespace/underscore → `-`, drop non `[a-z0-9-]`, collapse and trim
   `-` (`:72-79`). Must match the server's implementation exactly.
6. `GET {SKILLS_DOWNLOAD_URL|https://skills.sh}/api/download/<owner>/<repo>/<slug>`
   returning `{ files: [{path, contents}], hash }` (`:32-35`, `:515-533`). Self-hosted
   repos substitute their own URL (`:55-60`, `:523-524`).
7. All-or-nothing: any failed download → `null` → clone (`:735-737`).
8. Integrity vs the tree: held-out files are ignored, but every installable blob in the
   skill folder must be present and its Git blob SHA (`sha1("blob <len>\0"+content)`) must
   match the tree; otherwise `null` (`:566-595`, `:770-775`).
9. Root-level `SKILL.md` keeps **only** `SKILL.md`, never the rest of the snapshot
   (`:744-752`; guards against dumping an entire repo into one skill).
10. `snapshotHash` is recomputed locally with the same hashing the clone path uses
    (`:544-551`).
11. Commit via `fetchTreeCommit` when it matches the tree.

Everything is in-memory: `BlobSkill.path` is `''` and the installer consumes `files`.

---

## 9. Well-known discovery provider (`src/providers/wellknown.ts`)

**observed**

### 9.1 Endpoints and probe order

* Preferred path: `/.well-known/agent-skills/index.json`.
* Legacy fallback: `/.well-known/skills/index.json` (`:119-120`).
* For each path, a **path-relative** URL is tried first, then the origin root
  (`:185-205`). With a base URL that has a path, the effective order is:
  1. `<base>/.well-known/agent-skills/index.json`
  2. `<origin>/.well-known/agent-skills/index.json`
  3. `<base>/.well-known/skills/index.json`
  4. `<origin>/.well-known/skills/index.json`

  A bare origin produces only 2 candidates.
* **All** candidates are fetched (no early break) — up to 4 HTTP requests even when the
  first succeeds (`:215-239`). `fetchIndex()` then returns `candidates[0]`, i.e. the
  highest-priority successful index (`:164-166`).
* One `AbortSignal.timeout(10_000)` is created for the whole candidate loop, so a slow
  first request can abort the rest (`:13`, `:183`).
* `update` passes `updateCheck: true`, adding the header `X-Skills-Update-Check: 1`
  (`:219`; `tests/wellknown-update.test.ts:237`).

### 9.2 Index schemas

**v0.1.0 / legacy** (no `$schema` field) (`:19-33`, `:285-303`):

```json
{ "skills": [ { "name": "x", "description": "…", "files": ["SKILL.md", "ref/a.md"] } ] }
```

* Names: 1–64 chars, `[a-z0-9-]`, no leading/trailing `-`, no `--` (`:312-319`).
* `files` must be a non-empty array, each path non-empty, not absolute, not starting with
  `\`, containing **no** `..` anywhere and no NUL; at least one entry must be `SKILL.md`
  (case-insensitive) (`:321-347`).
* Validation is all-or-nothing: one bad entry rejects the whole index (`:288-290`).
* No integrity data at all.

**v0.2.0** — requires `$schema === "https://schemas.agentskills.io/discovery/0.2.0/schema.json"`
(`:10`, `:257-279`):

```json
{ "$schema": "…/0.2.0/schema.json",
  "skills": [ { "name": "x", "type": "skill-md" | "archive",
                "description": "…", "url": "relative or absolute",
                "digest": "sha256:<64 hex>" } ] }
```

* `description` required, ≤ 1024 chars; `url` must resolve as absolute or relative;
  `digest` must match `^sha256:[a-f0-9]{64}$` (`:349-370`).
* Bad entries are **skipped individually** (v2), unlike v1; an index whose entries all
  fail normalizes to "no index" (`:261-278`).
* Any `$schema` value other than the exact v0.2.0 URL rejects the index (`:281-283`).
* Relative `url` resolves against the *index* URL (`:264`).

### 9.3 Fetching a skill

v0.2.0 (`:504-567`):

* GET the artifact (no explicit timeout).
* Digest = `sha256:` + SHA-256 of the exact response bytes; mismatch throws
  `SKILLS_WELLKNOWN_DIGEST_MISMATCH` — **never** downgraded to v1 or to a plain download
  (`tests/wellknown-integrity.test.ts:49-95`).
* `skill-md`: decode, require frontmatter `name` + `description`.
* `archive`: ZIP or tar.gz; must contain a root `SKILL.md`; frontmatter from it.
* Container detection is lenient — content-type, URL suffix, or magic bytes
  (`PK`, `1f 8b`) (`:701-718`).

v0.1.0 (`:448-502`):

* `SKILL.md` is fetched first and parsed for metadata; the `files` list is *not* downloaded
  yet (`discoverSkills` returns a lazy `load()`).
* `installName` = the index `name`; `name` = frontmatter `name`.
* Any listed supporting file that fails (non-2xx, network, body read) throws
  `SKILLS_WELLKNOWN_FILE_FETCH_FAILED` with skill, path, and HTTP status; the skill cannot
  be installed without all listed files (`src/diagnostics.ts:130-135`;
  `tests/direct-download-add.test.ts:186-201`).
* Only the selected skills' files are downloaded, so one broken unselected skill does not
  block an install (`tests/direct-download-add.test.ts:203-227`).

### 9.4 Archive rules inside the provider

* ZIP: `readZipArchive` with 50 MiB unpacked / 1000 entries (`:11-12`, `:685-692`).
* tar.gz: hand-written parser; rejects symlink (`0x32`) and hardlink (`0x31`) type flags
  with `Archive links are not supported`; skips directories and other types; requires
  `SKILL.md`; per-file path rules reject `\`, absolute paths, drive letters, `.` and `..`
  segments; totals capped at 50 MiB / 1000 files (`:720-790`).
* Unknown format → `Unsupported archive format` (`:698`).

### 9.5 Scope semantics (surprising, and load-bearing)

**observed** — `discoverSkills` (`:609-665`):

* If the input URL has a non-empty path (`https://host/s/alpha`), the **root** candidates
  found in the same call are filtered out. A scoped URL is never widened to the host's
  root catalog.
* If the scoped candidates produced no skills but a root index existed, it throws
  `SKILLS_WELLKNOWN_SCOPE_NOT_FOUND` (message explicitly says it refuses to install every
  skill the host publishes) — `src/diagnostics.ts:120-125`;
  `tests/wellknown-provider.test.ts:410-452`.
* A scoped index that exists but lists zero skills *also* throws that error
  (`tests/wellknown-provider.test.ts:432-452`).
* A host with no index at all on a scoped path returns `[]` (no throw)
  (`tests/wellknown-provider.test.ts:481-489`).
* `add` turns both the digest-mismatch and scope errors into a printed error and
  `process.exit(1)` (`src/add.ts:983-994`).

### 9.6 Provider registry

**observed** — `ProviderRegistry` is a normal registry (duplicate id → error, first match
wins, insertion order) (`src/providers/registry.ts:4-52`), but **nothing in the CLI calls
`findProvider`, `getProviders`, or `registerProvider`** outside the provider module itself.
`add`/`update` call the `wellKnownProvider` singleton directly
(`src/add.ts:978`, `:1833`, `update.ts:449-489`). `WellKnownProvider.match()` excludes
`github.com`, `gitlab.com`, and `huggingface.co` (`:127-146`), but that exclusion has no
effect on the `add` path because `match()` is never consulted. A Go port needs at most one
provider plus a hard-wired dispatch; the registry is not behaviorally observable.

Telemetry source identifier = hostname with a leading `www.` removed
(`:826-837`).

---

## 10. Direct downloads (`src/download-source.ts`)

**observed** — `downloadSource(url, options)`:

1. `mkdtemp(<tmp>/skills-download-…)`, files `source.download` and `extract/` (`:286-288`).
2. `fetch(url, {redirect:'follow', signal: AbortSignal.timeout(30_000)})`; non-2xx →
   `Download failed with HTTP <status>` (`:106-118`).
3. Size guard: `content-length` over the limit fails fast; the body is also counted
   chunk-by-chunk so a missing/lying header still fails (`:120-144`).
4. Empty file → `Downloaded URL is empty`.
5. If the file's frontmatter has string `name` and `description` → it *is* a skill:
   copy to `<tmp>/skill/SKILL.md`, `kind: 'skill-md'` (`:146-154`, `:298-303`).
6. Otherwise treat it as an archive: magic-byte sniff (`PK` → zip, `1f 8b` → tar.gz),
   anything else → try tar (`:241-271`). If extraction fails for a non-rejection reason,
   the extract dir is wiped and the result is `Downloaded URL is not a valid SKILL.md file
   or supported archive` (`:306-311`).
7. If the extraction produced exactly one top-level visible directory (ignoring
   `__MACOSX`), that becomes `rootDir`; otherwise `extract/` itself (`:273-279`,
   `:305-309`). This is how `repo-main/skills/x` archives collapse.
8. Failures remove the temp dir; success leaves it for the caller to clean up
   (`:312-315`).

### 10.1 Limits and env overrides

| Limit | Default | Env override | Option |
| --- | --- | --- | --- |
| Download bytes | 10 MiB | `SKILLS_DOWNLOAD_MAX_BYTES` | `downloadMaxBytes` |
| Extracted bytes | 25 MiB | `SKILLS_EXTRACT_MAX_BYTES` | `extractMaxBytes` |
| Extracted files | 1000 | `SKILLS_EXTRACT_MAX_FILES` | `extractMaxFiles` |

Precedence (**observed**, `:51-66`): **env var > option > default**. Values that are not
positive integers are ignored. Consequence: a caller cannot *lower* a limit below an
exported env var, and Notion's larger limits are silently overridden if the env vars are
set lower.

### 10.2 Extraction and path safety

* ZIP is read fully into memory, then each entry written with a `isPathSafe` check under
  `extract/`; violations throw `Archive contains unsafe path: <path>` (`:156-175`).
* tar uses `tar.x` with `strict: true`, `preservePaths: false`, `noChmod: true` and a
  filter (`:191-239`) that:
  * normalizes `\` → `/`, strips a leading `./`, rejects absolute paths and
    `^[A-Za-z]:/` drive paths, rejects any `..` segment (`:74-80`);
  * counts **every** entry (including directories) toward the file limit
    (`:87-104`; `tests/download-source.test.ts:197-221`);
  * accepts only `File`, `OldFile`, `ContiguousFile`, and `Directory`; **symlinks and
    hardlinks are silently skipped**, not rejected (`:177-189`, `:217-221`).
* Rejection-class errors (unsafe path, link entry, encrypted entry, too many files, too
  large) propagate to the user with their own message; structural errors are treated as
  "not an archive" (`:82-85`, `:263-270`).

### 10.3 ZIP reader (`src/archive.ts`)

Hand-rolled, no dependency (**observed**):

* Finds the end-of-central-directory record by scanning backwards from EOF, validating
  the comment length (`:41-52`).
* ZIP64 supported (EOCD64 record + locator, including entry extra field `0x0001`);
  multi-disk archives rejected (`:65-146`, `:208-268`).
* Entry names: UTF-8 when flag bit `0x800`, else the Unicode Path extra field `0x7075`
  when its CRC matches, else CP437 decode (`:187-206`).
* Path rules: no NUL, no absolute path, no drive letter, no `..` segment; separators
  normalized; directory entries end with `/` (`:148-160`).
* Rejected: encrypted entries (flag `0x1`) → `Encrypted zip entries are not supported`;
  entry types other than regular file / directory (symlinks!) → `Archive links are not
  supported` (`:338-345`).
* Compression: stored (`0`) and deflate (`8`) only; anything else →
  `Unsupported zip compression method: <n>` (`:387-395`).
* Every entry is verified: declared sizes, local-header vs central-directory agreement,
  inflated length, CRC-32 (`:354-403`).
* Extracted-byte total is checked against the limit while iterating; the central
  directory must not overlap the trailer or file data (`:284-289`, `:348-351`,
  `:379-383`, `:406-408`).

---

## 11. Git clone (`src/git.ts`)

**observed**

### 11.1 Invocation and environment

* `git` is spawned by name from `PATH` (simple-git; no discovery, no fallback binary).
  The `gh` CLI is likewise spawned by name (`execFile('gh', …)`).
* Working dir: `mkdtemp(<tmp>/skills-…)` (`:306`).
* Flags: `['--depth','1']`, plus `['--branch', ref]` when a ref exists (`:307`).
* Env forced on every git invocation (`:151-165`):
  `GIT_TERMINAL_PROMPT=0`, `GIT_ALLOW_PROTOCOL=https:http:ssh:git:file`,
  `GIT_LFS_SKIP_SMUDGE=1`, `LC_ALL=C` (so English error strings match; a translated git
  would silently disable the SHA and auth fallbacks). Everything else is inherited,
  including credential helpers, `GIT_SSH_COMMAND`, proxies, `GIT_CONFIG_*`.
* Config forced on the command line (`:191-197`): `filter.lfs.required=false`,
  `filter.lfs.smudge=`, `filter.lfs.clean=`, `filter.lfs.process=`,
  `core.longpaths=true` (Windows MAX_PATH). This makes clones succeed whether or not
  git-lfs is installed; LFS payloads stay as pointer files, which is acceptable because
  skills are text (`tests/git-lfs-clone.test.ts`, `:168-197`).
* `ext::` URLs are refused outright: `Unsupported Git transport: ext` (`:301-304`).
  Inherited `GIT_ALLOW_PROTOCOL` cannot re-enable `ext`, `fd`, or custom protocols
  because the env is replaced (`tests/git-transport-allowlist.test.ts:33-59`).
* Timeout: 300 s default per command, `SKILLS_CLONE_TIMEOUT_MS` override (`:10-17`).
  A timeout produces `Clone timed out after <n>s.` plus remediation hints naming the env
  var (`src/diagnostics.ts:388-396`).
* Submodules: `git submodule update --init --recursive --depth 1`, best-effort; failure
  only prints `Warning: failed to initialize Git submodules; installed skills may be
  incomplete.` (`:252-262`).

### 11.2 Attempt order

1. `git clone --depth 1 [--branch ref] <url> <tmp>` with the user's ambient credentials.
2. On failure, if the ref is a 40-hex SHA **and** the message matches a missing-ref
   pattern → wipe the temp dir, `git init`, `git remote add origin <url>`,
   `git fetch --depth 1 origin <sha>`, `git checkout FETCH_HEAD` (`:66-78`, `:318-331`).
3. If the failure looks like authentication **and** the URL is an HTTPS GitHub URL:
   1. `gh auth status -h <host>` (5 s timeout) — used only to learn whether Git operations
      use SSH; a failure skips this branch entirely (`:264-279`).
   2. `gh repo clone <slug|sshUrl> <tmp> -- --depth=1 [--branch ref]` (`:281-290`).
   3. If that fails: SSH clone with `GIT_SSH_COMMAND` (existing value or
      `ssh -o BatchMode=yes`), including the same SHA fallback (`:355-373`).
4. Otherwise / after all fallbacks: remove the temp dir and throw a diagnostic —
   SAML SSO variant when the message matches SSO phrases, else a generic auth error
   (`:293-299`, `:379-386`).

Note that `gh`'s stored credential is used only *through the `gh` binary*; the CLI never
runs `gh auth token`, never reads `hosts.yml`, and never exports a token
(`docs/commands.md:61`; asserted in `tests/private-repo-add-security.test.ts:114-133`).

### 11.3 Recognized messages (must be reproduced verbatim for parity)

Missing ref (`:48-54`):

* `Remote branch … not found in upstream origin`
* `couldn't find remote ref`
* `upload-pack: not our ref`

Auth failure (`:140-149`):

* `Authentication failed`
* `could not read Username`
* `Permission denied`
* `Repository not found`
* `requested URL returned error: 403`
* SSO: `saml sso`, `enforced sso`, `enabled or enforced saml`,
  `re-authorize the oauth application`

### 11.4 Tree/commit helpers

* `getGitTreeHash(repo, skillPath)` → `rev-parse HEAD:<folder>` or `HEAD^{tree}` for a
  root skill; matches GitHub's folder SHA (`:389-399`).
* `getHeadCommit` → `HEAD^{commit}`, lowercased 40-hex or `null` (`:401-428`). Uses
  `GIT_OPTIONAL_LOCKS=0` and `--end-of-options`.
* `cleanupTempDir` refuses paths outside `os.tmpdir()`
  (`SKILLS_CLEANUP_OUTSIDE_TEMP_DIR`, `:430-440`).

---

## 12. Authentication, in full

**observed**

### 12.1 Direct GitHub API calls (blob fast path, folder hashes)

Order (`src/blob.ts:204-267`):

1. **Unauthenticated** request. This is the normal path (60 req/h per IP).
2. Retry with credentials only for: `403` **with** `x-ratelimit-remaining: 0`
   (rate limit) or `401`/`404` (a private repo hides behind 404). A bare `403` with
   remaining quota (permission denied) is **not** retried.
3. Explicit token: `GITHUB_TOKEN`, then `GH_TOKEN` (`src/skill-lock.ts:136-157`), sent as
   `Authorization: Bearer <token>`; headers also include
   `Accept: application/vnd.github.v3+json` and `User-Agent: skills-cli`
   (`src/blob.ts:128-134`).
4. If no token (or the token call returned nothing): `gh api <endpoint> --method GET
   --hostname <GH_HOST|github.com>` with `GH_PROMPT_DISABLED=1`, 10 s timeout, 16 MiB
   buffer, `windowsHide` (`:172-202`). The stored credential never leaves `gh`.
5. If both fail → the fast path returns `null` and the caller clones.

Session memo: once an IP-level rate limit is seen, later calls skip the unauthenticated
pass entirely (`_rateLimitedThisSession`, `:99-108`, `:234-238`, `:262-264`). A private-repo
404 must **not** set that flag (`tests/blob-fetch-tree-auth.test.ts:193-215`).

`fetchGitHubRepoInfo` — used for privacy gating, canonical rename, and telemetry — is a
plain **unauthenticated** `GET https://api.github.com/repos/<o>/<r>` (no token, no `gh`
fallback, no Enterprise base), returning `null` on any failure
(`src/source-parser.ts:89-107`). A port should keep that: privacy unknown ⇒ treated as
private.

### 12.2 Git clone

* Credentials come from the user's ambient Git configuration (credential helper, SSH
  agent/keys, `netrc`, `GIT_ASKPASS`, …). The CLI never injects them.
* `gh` is a *fallback* only for GitHub HTTPS, and only after the ambient clone fails.
* SSH is a *fallback* for GitHub HTTPS when both of the above fail.
* No interactive prompt is ever allowed (`GIT_TERMINAL_PROMPT=0`,
  `BatchMode=yes`), so an unauthenticated private repo fails fast instead of hanging.
* SSH URLs, `ssh://`, and other hosts get exactly one attempt (step 1); no `gh` fallback,
  because `parseGitHubRepoUrl` requires a GitHub host (`:87-119`).

### 12.3 What is never read, logged, or persisted

* `gh auth token`, `gh config get`, GitHub CLI credential files — never touched
  (`tests/private-repo-add-security.test.ts:127-132`).
* Tokens are never printed, never passed as clone arguments, never written to a lock file.
* Download URLs containing a query string or userinfo are **not** written to the lock
  (`SKILLS_DOWNLOAD_NOT_LOCKED`, `src/add.ts:1566-1578`, `:1621-1624`) and are redacted
  (`origin + pathname`) in messages (`:1627-1635`).
* Repo/skill identifiers are only sent to telemetry when the repo is *known* public;
  unknown visibility is treated as private (`src/add.ts:2410-2425`;
  `tests/private-repo-add-security.test.ts:147-159`).

### 12.4 When nothing is authenticated

Public GitHub: unauthenticated API + anonymous clone work. Private anything: the clone
fails, and the user gets `Authentication failed for <url>` with SSH/`gh auth status` hints
(`src/diagnostics.ts:397-408`), or the SAML SSO variant (`:409-416`). Exit code 1.

---

## 13. Temp dirs, concurrency, cleanup

**observed**

| Producer | Prefix | Cleaned by |
| --- | --- | --- |
| `cloneRepo` | `<tmp>/skills-…` | caller (`cleanupTempDir`), or on failure inside `cloneRepo` |
| `downloadSource` | `<tmp>/skills-download-…` | caller (`DownloadedSource.tempDir`), or on failure inside `downloadSource` |
| Notion pack staging | `<tmp>/skills-notion-…` | caller (`PreparedNotionPackSource.tempDir`) |

* `os.tmpdir()` is used directly; there is no env override for the temp root.
* `cleanup()` in `add.ts` tolerates `null`; `cleanupTempDir` validates the path is under
  tmpdir before `rm -rf`.
* Concurrency: `mapConcurrent` default 4, `SKILLS_CLONE_CONCURRENCY` override
  (`src/concurrency.ts:1-24`); a non-positive or malformed value falls back to 4.
* Clone and download temp dirs survive a successful resolve and are removed after install
  (`releaseSource` → `cleanup(resolved.tempDir)`).

---

## 14. Errors and exit behavior a port must reproduce

**observed**

### 14.1 Message text (user-visible `why` strings, `src/diagnostics.ts`)

| Code | Text (abridged) |
| --- | --- |
| `SKILLS_UNSAFE_SUBPATH` | `Unsafe subpath: "<s>" contains path traversal segments. Subpaths must not contain ".." components.` |
| `SKILLS_SUBPATH_OUTSIDE_REPO` | `Invalid subpath: "<s>" resolves outside the repository directory. …` |
| `SKILLS_LOCAL_PATH_NOT_FOUND` | `Local path does not exist: <path>` |
| `SKILLS_NO_VALID_SKILLS` | `No valid skills found. Skills require a SKILL.md with name and description.` |
| `SKILLS_NO_MATCHING_SKILLS` | `No matching skills found for: <names>` + `Available skills:` list |
| `SKILLS_WELLKNOWN_SCOPE_NOT_FOUND` | `No skills found for the scoped path '<p>' on <root>. Not falling back to the root skills index because that would install every skill the host publishes. …` |
| `SKILLS_WELLKNOWN_DIGEST_MISMATCH` | `Digest mismatch for skill '<n>': expected <e>, received <a>.` |
| `SKILLS_WELLKNOWN_FILE_FETCH_FAILED` | `Failed to download "<path>" for skill "<name>" (HTTP <s>). The skill cannot be installed without all listed files.` |
| `SKILLS_WELLKNOWN_INDEX_NOT_FOUND` | `No skills found at this URL. Make sure the server has a /.well-known/agent-skills/index.json or /.well-known/skills/index.json file.` (used by `skills use`) |
| `SKILLS_DOWNLOAD_HTTP_ERROR` | `Download failed with HTTP <status>` |
| `SKILLS_DOWNLOAD_TOO_LARGE` | `Download is larger than <n> bytes. Set SKILLS_DOWNLOAD_MAX_BYTES to override.` |
| `SKILLS_DOWNLOAD_NO_BODY` / `_EMPTY` / `_NOT_A_SKILL` | `Download response has no body` / `Downloaded URL is empty` / `Downloaded URL is not a valid SKILL.md file or supported archive` |
| `SKILLS_DOWNLOAD_NOT_LOCKED` | `Not recording <skills> in the lock file: <url> has a query string or credentials, which may be secret` |
| `SKILLS_ARCHIVE_UNSAFE_PATH` | `Archive contains unsafe path: <p>` |
| `SKILLS_ARCHIVE_ENCRYPTED_ENTRY` | `Encrypted zip entries are not supported` |
| `SKILLS_ARCHIVE_LINK_ENTRY` | `Archive links are not supported` |
| `SKILLS_ARCHIVE_TOO_MANY_FILES` | `Archive contains too many files (<n>). Maximum is <max>. Set <ENV> to override.` |
| `SKILLS_ARCHIVE_TOO_LARGE` | `Archive extracts to more than <n> bytes. Set <ENV> to override.` |
| `SKILLS_ARCHIVE_UNSUPPORTED_COMPRESSION` | `Unsupported zip compression method: <n>` |
| `SKILLS_ARCHIVE_UNSUPPORTED_FORMAT` | `Unsupported archive format` |
| `SKILLS_ARCHIVE_MISSING_SKILL_MD` | `Archive missing root SKILL.md` |
| `SKILLS_UNSUPPORTED_GIT_TRANSPORT` | `Unsupported Git transport: ext` |
| `SKILLS_GIT_CLONE_TIMEOUT` | `Clone timed out after <n>s.` + hints (`SKILLS_CLONE_TIMEOUT_MS`, ssh-add, gh auth status) |
| `SKILLS_GIT_AUTH_FAILED` | `Authentication failed for <url>.` + SSH/`gh auth status` hints |
| `SKILLS_GIT_SAML_SSO_BLOCKED` | `GitHub blocked HTTPS access to <url> because the organization enforces SAML SSO. …` |
| `SKILLS_GIT_CLONE_FAILED` | `Failed to clone <url>: <raw git detail>` |
| `SKILLS_SOURCE_FETCH_FAILED` | `Could not fetch <source>: <detail>` (programmatic API) |
| `SKILLS_NOTION_CLI_MISSING` | `ntn` (Notion CLI) not installed |

Structural archive errors (`SKILLS_ARCHIVE_INVALID`, `_MULTI_DISK_ZIP`) are explicitly
**not** shown to users; they mean "this is not an archive, try something else"
(`src/diagnostics.ts:166-172`).

### 14.2 Exit behavior

* `add` prints a `Source: <url>[ @ <ref>][ (<subpath>)][ @<skill>]` line before fetching
  (`src/add.ts:2635-2639`) — useful as a parsing smoke signal.
* Well-known digest mismatch or scope failure → error printed, `process.exit(1)`, **no**
  download fallback (`src/add.ts:983-994`). This contradicts the generic "well-known failed
  ⇒ try download" rule and must be preserved.
* No skills discovered → `SKILLS_NO_VALID_SKILLS`, red outro, exit 1
  (`src/add.ts:2703-2709`).
* Uncaught diagnostics bubble to `cli.ts`, which prints `Error: <formatted>` to stderr and
  sets `process.exitCode = 1` (no `process.exit`, to drain the event loop cleanly on
  Windows — `src/cli.ts:445-458`).
* Restore/install of several sources: failed sources are reported per source, the rest
  install, and the process ends with exit code 1
  (`tests/parallel-source-fetch.test.ts:170-184`).
* Lock write failure: skills are installed, `SKILLS_LOCK_WRITE_FAILED` is reported, exit 1
  (`tests/direct-download-add.test.ts:347-405`).
* `--json` requires `--yes` and is rejected for `--list` and for well-known sources;
  each case exits 1 with a message (`src/add.ts:2565-2573`, `:2652-2656`).

---

## 15. Platform specifics

**observed**

* **Git binary**: not discovered or configured; `git` and `gh` are resolved from `PATH`.
  A missing `git` surfaces as a clone failure (message from simple-git), not as a
  dedicated diagnostic. *(Which exact message appears for ENOENT is **assumed** — see
  open questions.)*
* **Windows**: `core.longpaths=true` on every command (both the clone and the
  `checkout FETCH_HEAD` fallback); `windowsHide: true` for `gh api`;
  `cleanupTempDir` compares against `os.tmpdir()` using `path.sep`; local-path detection
  accepts `C:\…` and `C:/…`; subpath checks normalize `\` for traversal detection but keep
  the original separators in `ParsedSource.subpath`; skill paths inside trees are
  normalized to `/` before hashing/lookup (`src/git.ts:393-399`,
  `src/blob.ts:330-341`, `src/skills.ts:27-29`).
  `fs` symlink creation/copying on Windows is installer territory (other doc), not this
  layer.
* **macOS/Linux**: no special handling beyond `os.tmpdir()` and POSIX paths. `__MACOSX`
  directories from Finder-created zips are ignored when finding the single top-level
  directory (`src/download-source.ts:273-279`).
* **Node requirement** is `>=22.20.0` (`package.json:engines`), which is why
  `AbortSignal.timeout`, `TransformStream`, `Response`, and `TextDecoder` are used
  directly. A Go port must supply its own equivalents (notably per-request timeouts).
* **Locale**: `LC_ALL=C` is forced for git so the message-matching fallbacks work. A Go
  port that matches git stderr must do the same, or match on structured exit codes.

---

## 16. Surprises and undocumented behavior

**observed unless marked**

1. **The provider registry is dead code.** One provider, called as a singleton; `match()`
   (which excludes `huggingface.co`) is never consulted by `add`.
2. **Well-known probing is not lazy.** All 2–4 candidate index URLs are fetched on every
   well-known source, even after one succeeds, and one shared 10 s abort signal covers the
   whole loop.
3. **There is no well-known cache.** Only a per-process GitHub rate-limit memo exists.
   Integrity data is re-verified on every run; `update` just adds a request header.
4. **`owner/repo@v2` is a skill name, not a tag**, while `owner/repo@v1.2` is a tag.
   A single number is treated as ambiguous on purpose.
5. **Tree URLs cannot express a slash-containing branch.** `tree/feature/my-feature` means
   ref `feature` + subpath `my-feature`.
6. **A shorthand with `.git` yields a doubled suffix.** `owner/repo.git` matches the
   shorthand branch, which appends `.git` without stripping an existing one, producing
   `https://github.com/owner/repo.git.git` (`src/source-parser.ts:713-723`). No test covers
   this; `https://github.com/owner/repo.git` is handled correctly by the URL branch. See
   open questions.
7. **Azure `?version=GC<sha>` is silently dropped**, so the clone lands on the default
   branch rather than the commit the URL pointed at.
8. **A GHE URL loses `#ref@skill`'s skill filter** (`:565-572`), while the same fragment on
   a `GH_HOST` shorthand keeps it.
9. **`type: 'download'` is a narrow allowlist.** Any `.zip` on a random host is
   `well-known` and only *becomes* a download after discovery fails; `?`/`#` are kept in
   the URL for downloads (and `#` is never sent by `fetch`).
10. **Subpaths are validated but not normalized.** `sanitizeSubpath` returns the original
    string (backslashes included) after checking a normalized copy.
11. **tar symlinks are skipped, zip symlinks are fatal.** Same archive concept, two
    different policies — a symlinked supporting file is silently missing from a `.tar.gz`
    and rejected in a `.zip`.
12. **Options cannot lower env-var limits**, because env wins over the option
    (`src/download-source.ts:51-66`). Notion's larger limits therefore depend on the user
    not exporting the env vars.
13. **A direct SKILL.md download with a query string installs but is never locked**, so
    `update`/restore cannot reproduce it (`src/add.ts:1566-1578`).
14. **Archive entry counts include directories** for tar (so `SKILLS_EXTRACT_MAX_FILES=1`
    fails on a one-file archive with one directory), while zip counts only central
    directory entries (directories included there too, but measured before skipping).
15. **V1 well-known indexes have zero integrity checking**; the "no downgrade" rule only
    protects v2 artifacts from falling back to an *unverified* v1 index, and it is
    enforced only for digest mismatches (other v2 failures can still fall through to a v1
    candidate if one exists at a lower-priority path).
16. **The repo `skills-lock.json` inside a fetched source is read** to avoid re-discovering
    skills that the source itself merely installed (`src/skills.ts:176-187`, `:221-233`).
17. **`GH_HOST` is used for shorthand, `gh api`, and `gitlab.com`-style URL rewriting is
    not** — only shorthand and the API base change. Explicit `github.com` URLs are never
    redirected.
18. **Rename following**: after fetch, `lookupGitHubRepo` may rewrite a *GitHub.com*
    source to its canonical `owner/repo` for the lock/telemetry
    (`src/source-parser.ts:124-138`; `tests/github-rename.test.ts`). `update` pins lock
    entries to `githubHost: 'github.com'` so an ambient `GH_HOST` cannot hijack an
    existing public install (`src/update.ts:150-158`).

---

## 17. Minimal contract for a Go port (summary)

1. Port `parseSource` as a single ordered matcher with the exact precedence in §3 and the
   exact `ParsedSource` fields; expose `getOwnerRepo` with its owner/repo-only rule.
2. Keep two independent ways to obtain a tree: `git clone --depth 1 [--branch ref]`
   (+ SHA fetch fallback) and the snapshot/GitHub-API path, and keep the snapshot path
   disabled for any explicit ref and for non-allowlisted owners.
3. Reproduce the auth ladder and its *message* triggers: ambient git → `gh repo clone` →
   SSH, plus unauthenticated GitHub API → `GITHUB_TOKEN`/`GH_TOKEN` → `gh api`.
4. Treat "arbitrary URL" as well-known-then-download, but make digest mismatch and scoped
   well-known misses fatal (no fallback), and never widen a scoped URL to the root index.
5. Enforce the archive/download limits, path-traversal rules, dual-format extraction, and
   the single-top-level-directory collapse, with env-over-option precedence.

---

## Evidence index

**Source**

* `src/source-parser.ts` — `isLocalPath` 325-335; aliases 343-346; `looksLikeGitSource`
  362-412; `parseFragmentRef` 414-444; `isVersionPin` 446-456; hosted artifacts 465-492;
  `parseSource` 498-741; `isWellKnownUrl` 748-771; `sanitizeSubpath` 145-158; Azure
  161-230; `pathOnHost` 236-244; mirrors 251-320; GHE 554-578; SSH tree 580-592; GitHub
  594-620; GitLab 622-667; shorthand 691-723; `getOwnerRepo` 13-67; GitHub API 89-138;
  canonical rewrite 124-130.
* `src/git.ts` — timeouts 10-17; `isCommitSha` 31-33; `isMissingRefError` 48-54;
  `cloneAtSha` 66-78; `parseGitHubRepoUrl` 87-119; auth/SSO matchers 130-149;
  `createGitClient` 151-245; submodules 252-262; `tryGhClone` 264-291; `cloneRepo`
  301-387; tree/commit helpers 389-428; `cleanupTempDir` 430-440.
* `src/blob.ts` — download base + allowlist 52-60; slug 72-79; API fetch 119-156; token/gh
  ladder 158-267; tree 269-292; commit 294-323; priority prefixes 353-361; `findSkillMdPaths`
  370-469; raw fetch 477-492; skills.sh.json 498-509; snapshot download 515-533; snapshot
  checks 544-595; `tryBlobInstall` 605-779.
* `src/providers/wellknown.ts` — constants 10-13; paths 119-120; `match` 127-146; candidates
  168-243; `normalizeIndex` 245-304; validation 306-370; `fetchSkill` 373-410; legacy fetch
  441-502; artifact fetch 504-567; scope logic 594-665; archive sniff/limits 680-718; tar.gz
  parser 720-790; digest 846-858.
* `src/providers/registry.ts` 1-52 (unused by CLI).
* `src/download-source.ts` — limits 12-15 and 44-66; path checks 68-80; download 106-144;
  SKILL.md detection 146-154; zip extract 156-175; tar filter 177-239; sniff/fallback
  241-271; flatten 273-279; entry point 281-316.
* `src/archive.ts` — EOCD 41-52; ZIP64 54-146; names 187-206; path rules 148-160; central
  directory walk 270-411.
* `src/github-host.ts` 10-38.
* `src/constants.ts` 1-6.
* `src/concurrency.ts` 1-24.
* `src/skill-lock.ts` 136-157 (`getGitHubToken`).
* `src/notion-test.ts` — limits 15-23; `ntn` runner 180-270; packs 272-340; URL parsing
  322-398; pack/page lock URLs 362-381; downloads 384-460.
* `src/add.ts` — well-known handling 967-1080; source line 2635-2639; dispatch
  2608-2760; `resolveSkills` 1641-1770; well-known resolve 1829-1846; `fetchSource`
  2043-2069; `fetchSources` 2075-2088; blob eligibility 1689-1712; download lock rules
  1566-1635; telemetry gating 2390-2425.
* `src/skills.ts` — depth constants 27-29; `findSkillDirs` 131-153; `isSubpathSafe` 169-174;
  `discoverSkills` 189-340; frontmatter rules 86-127.
* `src/install.ts` 90-160 and 190-220 (commit pinning on restore).
* `src/update.ts` 150-158 (`githubHost: 'github.com'` pinning), 445-495 (updateCheck).
* `src/diagnostics.ts` — source/download/archive codes 77-200; Notion 257-313; provider,
  git, rejected-archive catalogs 376-455.

**Tests**

* `src/source-parser.test.ts` (prefix fragments, GitLab custom hosts, Azure, GH_HOST).
* `tests/source-parser.test.ts` (full parse matrix, aliases, `getOwnerRepo`).
* `tests/subpath-traversal.test.ts`.
* `tests/download-source.test.ts` (skill-md, tar/tgz/zip by magic bytes, limits).
* `tests/archive.test.ts` (stored/deflate, traversal, drive paths, ZIP64).
* `tests/direct-download-add.test.ts` (well-known v2 archive, per-file failures, lock
  failures).
* `tests/download-lock.test.ts` (download entries in the lock).
* `tests/wellknown-integrity.test.ts`, `tests/wellknown-provider.test.ts`,
  `tests/wellknown-update.test.ts`.
* `tests/blob-fetch-tree-auth.test.ts`, `tests/blob-snapshot-consistency.test.ts`,
  `tests/blob-root-skill.test.ts`, `tests/skills-sh-paths.test.ts`.
* `tests/parallel-source-fetch.test.ts`, `tests/lock-commit-pin.test.ts`,
  `tests/git-transport-allowlist.test.ts`, `tests/git-lfs-clone.test.ts`,
  `tests/private-repo-add-security.test.ts`, `tests/github-rename.test.ts`,
  `tests/cross-platform-paths.test.ts`, `tests/pack-pointers.test.ts`.

**Docs**

* `docs/commands.md:7-61` (source formats, private repos, auth order),
  `:108-113` (direct downloads, limits, lock recording), `:342-346` (`npm:` entries).
* `README.md:108-111` (commit pinning and SHA fetch requirement).
* `package.json` (`engines.node >= 22.20.0`).

---

## Open questions

Evidence for each claim above is listed in the Evidence index; the numbers below point at
the parts that remain unverified.

1. **`.git` shorthand doubling** — does `owner/repo.git` really produce `.git.git` in
   v1.9.0, and should a Go port preserve the bug or normalize it?
   Evidence: `src/source-parser.ts:713-723`; no test.
2. **ENOENT for a missing `git`/`gh` binary** — which diagnostic and exit path does the
   user actually see (simple-git error text vs. a wrapped clone failure)? Not covered by
   tests; relevant for Windows machines without git.
3. **Does `fetch` strip the fragment of a `download` URL** in all supported Node
   versions, and is a URL with `#fragment` ever locked? Observed code keeps it; no test.
4. **V2 → V1 fallthrough** — if a v2 entry fails for a *non-digest* reason (e.g. archive
   missing `SKILL.md`), can a lower-priority v1 index at `/.well-known/skills/index.json`
   still be used? The loop over candidates suggests yes; no test asserts it either way.
5. **`gh auth status` protocol detection** — is `Git operations protocol: ssh` stable
   across `gh` versions? It is parsed from combined stdout+stderr with a 5 s timeout.
6. **Concurrency ordering effects** — with 4 concurrent fetches, two sources sharing an
   owner interact through the `_rateLimitedThisSession` flag and through GitHub API rate
   accounting; no test covers cross-source ordering.
7. **Blob snapshot freshness** — the completeness check compares Git blob SHAs, but the
   server-side snapshot could be built from a *different* ref than the tree; is
   `hasCurrentSnapshot` sufficient in practice (root skills only check `SKILL.md`)?
8. **Notion availability** — `notion-test.ts` is present in v1.9.0 but `docs/commands.md`
   never documents `skills add notion`; whether it is intended public behavior (and whether
   `ntn` is a hard requirement for Notion URLs) is unverified.
9. **`registry` intent** — whether the unused provider registry is meant to be a future
   extension point (mintlify/huggingface provider IDs appear only in comments).
