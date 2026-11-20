# management

Inspecting, restoring, removing and updating installed skills, in either scope.

## Behaviours

### list
Scans the canonical directory, every existing agent project directory (including legacy and shared
ones), and Eve subagent directories for the selected scope; a skill counts when its `SKILL.md` parses
and is not internal. Status comes from the scope's lock: `tracked` (hash matches), `modified`
(differs or no hash), `untracked` (no entry), `missing` (entry with no directory, only without an
`--agent` filter). Output is sorted by skill name then registry order; `list --json` carries
`subagents` for Eve placements.
- Criteria: statuses, filters, internal gating, deterministic order, human/JSON/empty forms.
- Tests: TestListStatusesAndMissing, TestListHashLessEntryIsModified, TestListSortsSkillsAndAgentsDeterministically, TestListJSONShape, TestWriteListHuman

### check
Reports local changes and available updates without writing. Git sources compare the lock commit
with the remote head of the recorded ref; downloads are re-fetched and compared by content hash;
well-known entries re-probe the provider and compare the index digest; Notion pages and packs are
re-exported through `ntn`. Local sources are never outdated; an unreachable source is a failure.
Eve placements are compared through their recorded flat files when the canonical directory is
absent. Exit 1 when any update, local change or failure exists.
- Criteria: all outcomes, clean summary, exit codes, Eve flat handling, per-source refresh.
- Tests: TestCheckUpToDate, TestCheckReportsAnUpdate, TestCheckReportsLocalChanges, TestCheckUnreachableSourceIsAFailure, TestCheckReadsEveFlatInstall, TestCheckDownloadReportsChangedContent, TestCheckWellKnownReportsChangedContent, TestNotionPageAddCheckUpdateRestore, TestE2ECheckReportsGitUpdates, TestE2ENotionUpdate

### remove
Selects by name, sanitized name, exact lock source, `--skill '*'` or `--all`. Removes from the
selected agent directories; the canonical directory and lock entry go only when no other agent uses
the skill. `--all`/`'*'` never delete untracked directories; an explicitly named untracked skill may
be removed from disk but not from the lock. Symlinks are unlinked, never followed. A named removal
that would delete an unmanaged directory inside a non-dot agent directory (OpenClaw `skills/`,
AstrBot `data/skills`, Eve `agent/skills`) is refused with exit 1. Eve subagent files are removed
exactly as recorded.
- Criteria: ownership rules, copy/symlink shapes, prompts, guard, idempotent lock writes.
- Tests: TestRemoveKeepsCanonicalWhileAnotherAgentLinksIt, TestRemoveAllKeepsUntrackedDirectories, TestRemoveAgentWildcardRemovesEveryCopy, TestRemoveSymlinkTargetSurvives, TestRemoveNoMatch, TestE2ERemoveOwnership, TestE2ERemoveUnmanagedGuard

### update
Groups lock entries by fetch URL and ref, acquires each group once, and reinstalls when content
changed, the folder is missing or the path moved. Downloads are re-fetched from the recorded URL;
well-known entries re-probe the provider and compare the digest; Notion pages and packs are
re-exported. Install mode, renamed names and Eve subagent placements are preserved; ambiguous
relocation is skipped; the lock records the new hash, commit and path atomically. Local entries and
entries without a recorded source are reported as not automatically updatable. Unreachable sources
count as failures.
- Criteria: unchanged, changed, missing, moved, renamed, copy-mode, Eve, failure and filter cases;
  per-source refresh and uncheckable reasons.
- Tests: TestUpdateUnchangedSkillIsCurrent, TestUpdateReinstallsChangedContent, TestUpdateFollowsARelocatedSkill, TestUpdateKeepsARenamedSkill, TestUpdatePreservesCopyMode, TestUpdateUnreachableSourceIsAFailure, TestUpdateDownloadReinstallsChangedContent, TestUpdateDownloadKeepsUnchangedContent, TestUpdateDownloadReinstallsAMissingFolder, TestUpdateWellKnownReinstallsChangedContent, TestUpdateWellKnownUnreachableProviderFails, TestNotionPageAddCheckUpdateRestore, TestNotionPackSelectionAndLock, TestE2EUpdateRoundTrip, TestE2ENotionUpdate

### update scope
Names with no `-g`/`-p` run the global phase then the project phase, each with its own header;
`-g`/`-p` select one phase; `-g -p` runs both; on a TTY with no names the picker offers `Project`,
`Global` and `Both`; `-y`/non-TTY chooses project when the project has skills, else global. Failures
are counted per phase and exit 1 if either failed.
- Criteria: both order and headers, picker, fallbacks, per-phase failures.
- Tests: TestUpdateScopePrompt, TestUpdateScopePromptBoth, TestE2EUpdateBothScopes, TestE2EUpdateScopeAndNames

### restore
`install`/`i` without a source (and `experimental_install`) rebuild the project from
`skills-lock.json`: entries are grouped by source and recorded commit, fetched at that commit, and
installed with the recorded mode and Eve placement. Downloads are re-fetched from their URL,
well-known entries re-probe their provider, and Notion pages and packs are re-exported through `ntn`;
local paths are not restored. Entries whose content matches `computedHash` are kept and only missing
links are repaired. `--force` reinstalls; `--dry-run` reports without writing.
Agent targets follow `-a`, then the global `lastSelectedAgents`, then detection, then canonical only.
Restore never prompts and never touches the global lock.
- Criteria: pinned restore, skip/relink, copy mode, renamed skills, Eve flat files, source isolation,
  per-source re-fetch.
- Tests: TestRestoreGroupsBySourceAndCommit, TestRestoreSkipsMatchingContentAndKeepsTheLock, TestRestoreForceReinstalls, TestRestoreAgentFallback, TestRestoreDownloadsAgain, TestRestoreUnreachableDownloadFails, TestRestoreWellKnownRefetches, TestNotionPageAddCheckUpdateRestore, TestE2ERestoreFromLock, TestE2ERestoreEveFlat

### frozen lock file
`--frozen-lockfile` (default when any of `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `BUILDKITE`,
`CIRCLECI`, `TRAVIS`, `TF_BUILD`, `JENKINS_URL` is set) validates before installing and writes
nothing on failure: missing/empty lock, entry without a hash, content mismatch, missing folder, a
skill the source no longer provides, or a migrated v1 entry with an empty hash. `--no-frozen-lockfile`
overrides the CI default; `--frozen-lockfile` always wins. Non-frozen restore repairs empty hashes and
records them.
- Criteria: each failure case, nothing written, v1 message, overrides, dry-run validation.
- Tests: TestFrozenRestoreEnvironmentTable, TestFrozenRestoreFailures, TestFrozenRestoreV1EmptyHash, TestE2ERestoreFrozen

### Global scope
`-g` uses the global directories and `.skill-lock.json`; project scope uses the project lock. The
locks never mix. Global statuses compare the recorded content hash, or clone the recorded commit for a
40-hex tree hash. Global installs keep `installedAt` and refresh `updatedAt`; `lastSelectedAgents` is
written for both scopes.
- Criteria: global round trip, no cross-writing, empty lock after the last removal.
- Tests: TestE2EGlobalScopeRoundTrip, TestGlobalListAndCheckReadTheGlobalScope, TestGlobalAddWritesOnlyTheGlobalLock, TestGlobalRemoveDeletesUniversalAgentOwnDir

## Not supported

- Global restore (upstream restore is project-only).
- Prompted deletion of skills that disappeared upstream.
- Updating or restoring a lock entry with no recorded source, or `node_modules` entries.
- Rebuilding an installation from local paths; they are installed with `add`.
