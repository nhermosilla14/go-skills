# management

Inspecting, removing and updating installed skills, in either scope.

## Behaviours

### list
Scans the canonical directory, every existing agent project directory (including legacy and shared
ones), and Eve subagent directories for the selected scope; a skill counts when its `SKILL.md`
parses and is not internal. Each skill is attributed to every agent that holds it. Status comes from
the scope's lock: `tracked` (hash matches), `modified` (differs or no hash), `untracked` (no entry),
`missing` (entry with no directory, only without an `--agent` filter). Output is sorted by skill name
then registry order.
- Criteria: statuses, filters, internal gating, deterministic order; human, JSON and empty forms.
- Tests: TestListStatusesAndMissing, TestListHashLessEntryIsModified, TestListDeduplicatesLinkedDirectories, TestListSortsSkillsAndAgentsDeterministically, TestListJSONShape, TestWriteListHuman

### check
Reports local changes and available updates without writing. Git sources compare the lock commit
with the remote head of the recorded ref; local sources are never outdated; an unreachable source is
a failure. Exit 1 when any update, local change or failure exists; 0 when clean.
- Criteria: all four outcomes; clean summary; exit codes.
- Tests: TestCheckUpToDate, TestCheckReportsAnUpdate, TestCheckReportsLocalChanges, TestCheckUnreachableSourceIsAFailure, TestCheckLocalSourceIsNeverOutdated, TestE2ECheckReportsGitUpdates

### remove
Selects by name, sanitized name, exact lock source, `--skill '*'` or `--all` (which implies
`--agent '*'` and `-y`). Removes from the selected agent directories; the canonical directory and
lock entry go only when no other agent still uses the skill. `--all` and `'*'` never delete
directories the lock does not track; an explicitly named untracked skill may be removed from disk
but not from the lock. Symlinks are unlinked, never followed. Usage errors exit 2, no match and
failed removals exit 1.
- Criteria: ownership rules, copy and symlink shapes, prompts, idempotent lock writes.
- Tests: TestRemoveKeepsCanonicalWhileAnotherAgentLinksIt, TestRemoveAllKeepsUntrackedDirectories, TestRemoveAgentWildcardRemovesEveryCopy, TestRemoveSymlinkTargetSurvives, TestRemoveNoMatch, TestE2ERemoveOwnership

### update
Groups lock entries by fetch URL and ref, acquires each group once, and reinstalls a skill when its
content changed, its folder is missing, or its path moved. Install mode and renamed names are
preserved; relocation with several candidates is skipped rather than guessed; the lock records the
new hash, commit and path atomically. Local entries are reported as not automatically updatable.
Unreachable sources count as failures and do not stop other groups. Exit 1 on any failure.
- Criteria: unchanged, changed, missing, moved, renamed, copy-mode, failure and filter cases.
- Tests: TestUpdateUnchangedSkillIsCurrent, TestUpdateReinstallsChangedContent, TestUpdateFollowsARelocatedSkill, TestUpdateKeepsARenamedSkill, TestUpdatePreservesCopyMode, TestUpdateUnreachableSourceIsAFailure, TestE2EUpdateRoundTrip

### Both scopes
`-g` uses the global directories and `.skill-lock.json`; project scope uses the project lock. The
locks never mix. Global statuses compare the recorded content hash, or clone the recorded commit
when a 40-hex tree hash is stored. Global installs keep `installedAt` and refresh `updatedAt`.
- Criteria: global round trip; no cross-writing; empty lock after the last removal.
- Tests: TestE2EGlobalScopeRoundTrip, TestGlobalListAndCheckReadTheGlobalScope, TestGlobalAddWritesOnlyTheGlobalLock, TestGlobalRemoveDeletesUniversalAgentOwnDir

## Not supported

- `update` across both scopes in one invocation; it runs one scope per run.
- Prompted deletion of skills that disappeared upstream.
- Repairing agent links deleted by hand.
- Updating download, well-known and `node_modules` entries.
