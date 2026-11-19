# install

Turning a plan into files on disk: canonical copies, links, copies and agent-specific shapes.

## Behaviours

### Plan and apply
The installer resolves targets, plans one entry per skill (canonical directory and per-agent
targets), then applies in deterministic order and reports one result per skill and agent. Copies
are written to a sibling temporary directory and renamed into place; removals are guarded by
containment checks; an interrupted run never leaves a half-written skill.
- Criteria: plan is pure and assertable; apply reports installed/skipped/failed; no partial
  directories.
- Tests: TestPlanSymlinkForTwoDirs, TestApplySymlinkModeLinksRelatively, TestApplyReplacesOldInstallAndDropsStaleFiles, TestE2EAddLocalInstall

### Link or copy
Symlink mode writes one canonical copy and links each agent directory with a relative target on
POSIX, or a junction with an absolute target on Windows, falling back to a copy when linking is
refused. Copy mode writes independent copies and never creates a canonical directory. Copy is used
when `--copy` is given, when the selected agents resolve to at most one project directory, or when
every target is Eve.
- Criteria: mode derivation matches; links resolve after moving the project; a refused junction
  copies instead and reports it.
- Tests: TestPlanModeDerivation, TestPlanCopyForOneDir, TestApplyLinkFailureFallsBackToCopy, TestJunctionCreation, TestJunctionThenApplyLinks

### Agent-specific targets
Shared directories are written once, with one result per agent id. A non-universal project target
whose agent root directory is missing is skipped in symlink mode; Claude Code may create its
directory; universal agents read `.agents/skills`; a source that overlaps the destination is skipped
instead of deleted; category directories are never targets. Eve receives a flat `<name>.md` copy with
`name`, `description`, `license`, `compatibility`, `version` and `metadata` only.
- Criteria: each rule holds and is reported in the summary.
- Tests: TestPlanDedupesSharedProjectDirectory, TestPlanMissingAgentDirectorySkipsInSymlinkMode, TestPlanEveIsAFlatCopy, TestApplyEveReducesFrontmatter, TestApplyOpenClawOverlapIsSkipped, TestApplyRefusesCategoryDirectory

### Eve subagents
Eve installs are flat `<name>.md` copies with reduced frontmatter. Targets are the root
`agent/skills` (recorded as `""`) plus each selected subagent under
`agent/subagents/<name>/skills`; subdirectories are discovered and sorted. The interactive flow
offers root plus subagents when `agent/subagents/` exists, `--subagent` accepts `root`/`.`/names
without validating them, and the lock records the placement. Update re-applies it, remove deletes
exactly those files, and list shows it. Global scope rejects Eve and its subagents.
- Criteria: paths, sorting, prompt, lock field, update/remove/list behaviour.
- Tests: TestEveSubagentsSorted, TestPlanEveSubagentTargets, TestE2EEveSubagents, TestE2ERestoreEveFlat

### Listing during add
`add --list` prints every discovered skill and installs nothing, ignoring `--skill` (upstream
behaviour), and exits 0.
- Criteria: a `--skill` filter does not narrow the listing.
- Tests: TestAddListInstallsNothing, TestE2EListIgnoresRename

### Global placement
With `-g`, targets resolve to each agent's global directory; universal agents share
`~/.agents/skills`; Antigravity uses its own directory; project-only agents are dropped with one
notice; Eve fails with its documented error. Global installs never write the project lock.
- Criteria: placement matches the registry; no project lock appears.
- Tests: TestGlobalAddWritesOnlyTheGlobalLock, TestE2EGlobalScopeRoundTrip, TestE2EGlobalProjectOnlyAgentsFails

## Not supported

- The GitHub blob fast path and its root-skill trimming.
- Transactional rollback of a partially applied plan.
