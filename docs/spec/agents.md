# agents

The supported agents, their directories, detection and selection.

## Behaviours

### Registry
All 79 upstream agent ids are present with display name, project and global skill directories,
project-only and universal flags, hidden-from-prompt flags, canonical-global readers and the
Claude-only create-directory flag. Registry order is preserved and never sorted.
- Criteria: ids and paths match the upstream table; `ProjectSkillDirs()` is the sorted, deduplicated
  union plus the legacy directories, excluding `skills`.
- Tests: TestRegistryCounts, TestPathsEveryAgent, TestRegistryOrderMatchesDocumentedTable, TestProjectSkillDirs

### Path resolution
Resolution takes an injected environment (home, cwd, OS, environment map) so every platform is
testable anywhere. XDG variables, `%APPDATA%`, `FLATPAK_XDG_CONFIG_HOME`, `CODEX_HOME` and the
documented special cases apply (OpenClaw legacy homes, Reasonix on Windows, Kimchi, Crush, Windsurf,
Cortex, Deep Agents, Droid, Antigravity, Qoder CN, Zencoder/Zenflow, project-only Eve and
PromptScript).
- Criteria: table-driven results for linux, darwin and windows; overrides win where documented.
- Tests: TestPathsEveryAgent, TestOpenClawPrefersExistingLegacyHome, TestGlobalLockPath

### Detection
Detection is `stat`-based, in registry order, with no `PATH` or process checks. Project markers
apply for AstrBot, CodeBuddy, Continue, Eve, Jazz, PromptScript and Replit; Eve additionally needs an
`eve` dependency; `universal` never detects; errors count as absent.
- Criteria: every detector and its alternates are covered; order is preserved.
- Tests: TestDetectionMarkers, TestDetectionAlternateBranches, TestDetectionEveRequiresPackageDependency, TestDetectPreservesRegistryOrder

### Host-agent detection
Environment markers identify the process host: Claude, Codex, OpenCode, Gemini CLI, GitHub Copilot,
Cursor (strong signals only; a bare `CURSOR_TRACE_ID` is demoted), Antigravity, Augment, Devin,
Replit and `AI_AGENT`. The mapping is a data table evaluated once; unmapped names fall back to normal
detection. Detection makes `add`/`remove`/`find` non-interactive, targets the host plus the universal
set in `add` unless `-a` is given, and permits `--json` without `-y`.
- Criteria: one test per marker, Cursor demotion, fallback, host target set.
- Tests: TestDetectHostMarkers, TestDetectHostCursorDemoted, TestDetectHostUnmappedFallsBack, TestAddHostTargets

### Global directories
Every command resolves an agent's global directory through the same `BaseDir` resolver, so legacy
homes (OpenClaw `~/.clawdbot`, `~/.moltbot`) behave identically in add, list, remove and update.
- Criteria: a legacy home is used when present.
- Tests: TestOpenClawPrefersExistingLegacyHome, TestE2EGlobalLegacyDirs

### Selection rules
Explicit `-a` ids bypass detection; `*` expands to all. With no `-a`: zero detected and `-y` targets
all agents; one detected targets it plus the universal set; two or more target all detected plus the
universal set. The universal set includes hidden agents and excludes Antigravity, `replit` and
`universal`.
- Criteria: the select matrix holds; duplicates collapse at plan time.
- Tests: TestSelectMatrix, TestAutoUniversalExcludesNamedOnlyAndAntigravity, TestAddDetectsAgentsWhenNoneIsNamed, TestAddExplicitAgentsBypassDetection

## Not supported

- Banner/logo rendering (the tool has none).
- `/Applications` probes are not OS-gated, matching upstream.
