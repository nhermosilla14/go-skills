# lockfile

The two lock files and the content-hash contracts they record.

## Behaviours

### Project lock
`skills-lock.json` at the project root, `version: 2`, `skills` keyed by installed name, entries with
`source`, `sourceType`, optional `sourceUrl`/`ref`/`commit`/`skillPath`/`skillName`/`installMode`/
`subagents`/`sourceBaseUrl`/`wellKnownDigest` and `computedHash`; `subagents` records Eve placement
(`""` for the root). There are no timestamps and no `skillPath` for local sources. Writes are atomic
(temp file, fsync, rename), two-space indented, trailing newline, keys sorted by UTF-16 code units.
Unknown top-level and per-entry fields are preserved.
- Criteria: upstream fixtures round-trip; no timestamps; atomic replaces; unknown fields survive;
  `subagents` round-trips.
- Tests: TestSaveWritesSortedIndentedDocument, TestLoadPreservesUnknownFieldsOnRewrite, TestProjectLockHasNoTimestamps, TestLoadRejectsOtherVersionsAndBrokenFiles, TestSubagentsRoundTrip

### Source-specific entries
`sourceType` is one of `local`, `github`, `gitlab`, `git`, `azure`, `download` or `well-known`;
Azure is recorded as `git`. A download records its URL in `source`/`sourceUrl` and the skill's
`skillPath`; when the URL carries a query string or userinfo, the location is omitted, the install
succeeds, and the user is warned to re-add it because update and restore cannot re-fetch it. A
well-known entry records `sourceType: "well-known"`, the provider base in `source`/`sourceBaseUrl`,
`wellKnownDigest` and `skillPath`; its global `skillFolderHash` is empty. A Notion page is a
`download` entry whose source is the page URL; a pack is a `download` entry with `source: "notion"`
and `sourceUrl: "notion://packs/<id>"`, because the export URL expires. `computedHash` is the normal
content hash of the installed folder for every kind.
- Criteria: download fields, secret URLs omitted with a warning, well-known provider fields and empty
  global folder hash, Notion page and pack shapes.
- Tests: TestAddDownloadSingleFile, TestAddDownloadDoesNotRecordASecretURL, TestAddWellKnownV2RecordsTheProvider, TestAddWellKnownGlobalFolderHashIsEmpty, TestNotionPageAddCheckUpdateRestore, TestNotionPackSelectionAndLock, TestE2EDownloadSingleFile, TestE2EWellKnownInstall, TestE2ENotionPage

### Global lock
`.skill-lock.json` under `$XDG_STATE_HOME/skills/` or `~/.agents/`, `version: 3`, with `skills`,
optional `dismissed` and `lastSelectedAgents`. Entries add `skillFolderHash`, `installedAt`,
`updatedAt`, `installMode` and the source-specific optionals. `skillFolderHash` is the 40-hex Git
tree SHA for GitHub sources (Trees API, optional token) and the 64-hex content hash otherwise, with
a content-hash fallback and a warning when the API fails; a well-known entry stores an empty folder
hash. Writes are atomic and serialised; a corrupt lock is reported and not overwritten.
- Criteria: path resolution across platforms; unknown fields preserved; corrupt file refused;
  tree-hash shape and fallback; well-known empty hash.
- Tests: TestGlobalRoundTrip, TestGlobalPreservesUnknownFields, TestGlobalCorruptFileIsRefused, TestGlobalFailedEncodeLeavesFileIntact, TestGlobalFolderHashUsesGitHubTreeSHA, TestGlobalFolderHashFallsBackToContentHash, TestAddWellKnownGlobalFolderHashIsEmpty

### Content hash
Files below the skill folder, excluding `metadata.json` at any depth and the `.git`, `__pycache__`
and `__pypackages__` directories; symlinks resolved inside the folder and escaping, broken or cyclic
ones dropped; CRLF normalized to LF only in files without a NUL byte; sorted by unsigned UTF-8 path
bytes; SHA-256 over `path NUL length NUL content` per file, lowercase hex.
- Criteria: the three published vectors and the documented edge cases hold.
- Tests: TestDirPublishedVectors, TestDirIgnoresExcludedEntries, TestDirNormalizesCRLFOnlyInText, TestDirSortsByUnsignedBytes, TestDirSymlinkPolicy

### Version 1 migration
A `version: 1` project lock is upgraded on read: the legacy hash (regular files, `.git` and
`node_modules` skipped, UTF-16 path order, path+content without framing) is recomputed; a match
produces the v2 hash, otherwise `computedHash: ""`, which reports as modified and is re-fetched.
- Criteria: match and mismatch paths; other fields preserved; persisted on the next write.
- Tests: TestLegacyHashVectors, TestLoadMigratesVersion1, TestE2EProjectLockV1Migration

## Not supported

- Byte-identical output with the Node CLI; semantic compatibility is the contract.
- Locale-exact `localeCompare` ordering for non-ASCII v1 entries (they take the empty-hash path).
