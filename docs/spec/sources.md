# sources

Parsing a source argument and acquiring the tree it points at.

## Behaviours

### Source grammar
Accepted forms: local paths, GitHub shorthand `owner/repo` with `//subpath`, `@ref` or `@skill`,
`github:`/`gitlab:` prefixes, GitHub and GitLab HTTPS URLs including `/tree/<ref>/<subpath>`,
Azure DevOps URLs with `?path=` and `?version=GB<ref>`, scp-style and `ssh://` URLs, generic
`https`/`git`/`file` URLs, direct `http(s)` downloads and archives, Notion page URLs, the bare
`notion` pack argument, and `#ref` fragments. A version-looking `@` suffix is a ref, a
skill-looking one is a selector. Credentials in URLs are rejected; subpaths must stay inside the
repository. `notion.site` hosts and `npm:` arguments are not special sources.
- Criteria: the parser table covers every form; `..`, absolute subpaths and userinfo are rejected;
  Notion hosts are matched as `(^|\.)notion\.(so|com)$`.
- Tests: TestParse, TestParseLocalPaths, TestParseRejects, TestValidateSubpath, TestParseNotionPageURL, TestParseNotionArguments

### Acquisition
Git sources are shallow-cloned into a temporary workspace; a 40-hex pin fetches that commit; the
resolved commit is recorded. A public GitHub source without an explicit ref first tries the API fast
path. Local sources are used in place. Download sources are materialized into the workspace, and
Notion sources are exported into it by `ntn`. The workspace is removed on success and on every error
path. Submodules are never followed.
- Criteria: clone, tag, commit pin, moved tag, subpath and cleanup behave; a pinned install still
  fetches the pinned commit after the ref moves.
- Tests: TestAcquireClonesDefaultBranch, TestAcquireTagRef, TestAcquireCommitPinSurvivesMovedTag, TestAcquireSubpath, TestE2EGitSourcePinnedCommit

### Direct downloads and archives
A non-git HTTP(S) URL is downloaded and sniffed by magic bytes, not by extension. A single
`SKILL.md` becomes one skill; `zip`, `tar`, `tar.gz` and `.tgz` archives are extracted. Defaults are
a 10 MiB body, 25 MiB extracted and 1000 entries; `SKILLS_DOWNLOAD_MAX_BYTES`,
`SKILLS_EXTRACT_MAX_BYTES` and `SKILLS_EXTRACT_MAX_FILES` may raise a limit but never lower it.
Redirects are followed up to five within a 10-second budget, TLS verification is never disabled, and
errors report the HTTP status without the URL or its credentials. Empty bodies and content that is
neither a skill nor an archive are errors.
- Criteria: single file, archive sniffing, one top-level directory collapsed, streaming limits,
  environment overrides, HTTP failures, redacted errors.
- Tests: TestFetchDownloadSingleSkill, TestFetchDownloadRejectsABodyThatIsNotASkill, TestFetchDownloadSniffsArchives, TestFetchDownloadCollapsesOneTopLevelDirectory, TestFetchDownloadBodyLimit, TestFetchDownloadBodyLimitAppliesWithoutContentLength, TestFetchDownloadEnvironmentRaisesButNeverLowersLimits, TestFetchDownloadExtractLimit, TestFetchDownloadEntryLimit, TestFetchDownloadHTTPFailures, TestFetchDownloadErrorsHideTheURL, TestSourceDisplayAndRecordable, TestE2EDownloadSingleFile, TestE2EDownloadArchive, TestE2EDownloadLimits

### Archive extraction safety
Extraction goes into a fresh directory and rejects absolute paths, `..`, drive letters, backslashes
used as separators and entries that escape the root. Tar symlinks are skipped; zip symlinks are
refused. A rejected or oversized archive leaves nothing behind.
- Criteria: every traversal shape is refused; symlink policy; partial extraction is removed.
- Tests: TestFetchDownloadRejectsUnsafeArchivePaths, TestFetchDownloadSymlinkPolicy, TestE2EDownloadTraversal

### Well-known providers
An HTTP(S) source probes `<base>/.well-known/agent-skills/index.json`,
`<origin>/.well-known/agent-skills/index.json`, `<base>/.well-known/skills/index.json` and
`<origin>/.well-known/skills/index.json` in that order under one 10-second budget, and the
highest-priority usable index wins. Update and check requests send `X-Skills-Update-Check: 1`. A
path-scoped source never widens to the origin root index; a host with no index falls through to a
direct download. Schema v0.2.0 (`$schema` set) requires `sha256:<hex>` digests, and a mismatch is
fatal with no fallback; malformed entries are skipped individually. The legacy schema (no
`$schema`) carries no digest and is all-or-nothing. Artifacts are a single file (`skill-md`) or an
archive, bounded at 50 MiB unpacked and 1000 entries with links refused; the legacy file list is
fetched file by file and any failed file fails that skill. Declared paths follow the same traversal
rules and an index without a recognizable shape is an error.
- Criteria: candidate order and priority, one shared budget, no widening, digest enforcement,
  legacy behaviour, archive rules, defensive parsing.
- Tests: TestProbeWellKnownCandidateOrderAndPriority, TestProbeWellKnownSendsTheUpdateHeader, TestProbeWellKnownSharesOneBudget, TestProbeWellKnownDoesNotWidenAScopedSource, TestParseWellKnownIndexV2, TestParseWellKnownIndexLegacyAllOrNothing, TestFetchWellKnownDigestMismatchIsFatal, TestFetchWellKnownLegacyFetchesOnlySelectedSkills, TestFetchWellKnownArchiveRules, TestFetchWellKnownLegacyNormalizesSkillMDCase, TestE2EWellKnownInstall, TestE2EWellKnownDigestMismatch, TestE2EWellKnownScopedIndex, TestE2EWellKnownFallbackToDownload

### Notion pages and packs
A `notion.so`/`notion.com` page URL and the bare `notion` argument select the Notion path. `ntn`
must be on `PATH`; otherwise the command fails with install guidance and exit 1 before any network
call. Pages are exported by `ntn` and packs are exported from the workspace listing, then handled as
a download source. The interactive flow offers a pack picker; `-y`, `--list` and `--json` install or
list every pack. A pack export lands under `packs/<id>/`, so its recorded paths survive a re-export.
An expired, deleted or unauthorized page reports the `ntn` error and leaves the installed copy
intact. Credentials belong to `ntn`; the tool never reads, passes or prints them.
- Criteria: page and pack export, pack selection, missing tool, re-export, expired page, output and
  error bounds.
- Tests: TestAcquireNotionPage, TestAcquireNotionPack, TestFetchNotionPacksCommandsAndPagination, TestFetchNotionPacksRejectsAnInvalidCursor, TestFetchNotionPacksNoPacks, TestNotionPackLockURL, TestNotionMissingCLI, TestRunNTNTimeout, TestRunNTNStderrMapping, TestRunNTNOutputLimit, TestNotionDirectoryCommands, TestNotionWorkspaceName, TestNotionLimitFallbacks, TestE2ENotionPage, TestE2ENotionPack, TestE2ENotionMissingTool, TestE2ENotionUpdate

### GitHub API fast path
A public GitHub source with no explicit ref reads the default-branch commit, the recursive tree and
the selected blobs through the API, verifies the snapshot against the tree entry SHAs, and installs
from it. Any mismatch, missing or oversized response, API error or rate limit falls back to the
clone path after one informational line. An explicit ref, a non-GitHub host or a private repository
uses the clone path. Authentication follows anonymous → `GITHUB_TOKEN`/`GH_TOKEN` → `gh api`;
tokens are never printed, logged or written, and a 401/403 without rate-limit headers falls straight
back to clone. Only read endpoints are used. Files, hashes, discovery results and lock entries match
the clone path, except that a root-level skill carries only `SKILL.md` (the clone path carries the
whole directory).
- Criteria: result parity, completeness fallback, token ladder, no token output, root-skill payload.
- Tests: TestBlobAcquireSequenceAndFiles, TestBlobAcquireSubpath, TestBlobAcquireRootSkillKeepsOnlySkillMD, TestBlobAcquireUsesARecordedCommit, TestBlobAcquireSkipsExplicitRefs, TestBlobAcquireSkipsOtherHosts, TestBlobAcquireFallsBackOnIncompleteTree, TestBlobAcquireFallsBackOnOversizedResponse, TestBlobAcquireFallsBackOnMissingSubpath, TestBlobAcquireRejectsLinksAndSubmodules, TestBlobAcquirePrivateRepoFallsBackWithoutToken, TestBlobAcquireTokenLadder, TestBlobAcquireGHTOKENIsTheFallback, TestBlobAcquireGHApiFallback, TestBlobAcquireNeverPrintsTheToken, TestE2EBlobInstallMatchesClone, TestE2EBlobFallsBackOnIncompleteTree, TestE2EBlobTokenLadder, TestE2EBlobRootSkill

### Authentication ladder
Git acquisition attempts ambient `git` first, then `gh repo clone` for GitHub over HTTPS, then SSH.
Retries only on the documented authentication and 401/403/404 patterns; a server error is not
retried. Credentials are never read, printed or recorded.
- Criteria: each fallback triggers only on the matching failure; reported messages are redacted.
- Tests: TestAcquireAuthFirstAttemptSucceeds, TestAcquireAuth403FallsBackToGH, TestAcquireAuthGHMissingFallsBackToSSH, TestAcquireAuthServerErrorDoesNotRetry, TestAcquireAuthRedactsCredentials

### Remote ref lookup
`check` compares the lock commit with the remote head of the recorded ref using `git ls-remote`,
preferring peeled tags and branches, without cloning.
- Criteria: reported head matches the remote; failures are reported, not ignored.
- Tests: TestRemoteHeadDefaultRef, TestRemoteHeadPrefersThePeeledTag, TestRemoteHeadPrefersTheBranch, TestRemoteHeadFailures

## Not supported

- `npm:` sources, the `package.json` `skills` field and all `node_modules` integration.
- Notion OAuth, the Notion API in Go, and workspace search beyond `ntn`.
- GitLab or other hosts' APIs; only GitHub has an API fast path.
- GitHub Enterprise aliases and mirror/proxy rewriting.
- Publishing a well-known index from this tool.
- Downloads that require browser authentication or cookies.
