# cli

Command surface, argument parsing, prompts, output streams, JSON documents and exit codes.

## Behaviours

### Commands and aliases
`skills` dispatches `add` (`a`, `install`, `i` with a source), `list` (`ls`), `check`,
`update` (`upgrade`), `remove` (`rm`, `r`), `use`, `find` (`f`, `s`, `search`), `init`,
`install`/`i` without a source, `experimental_install` (alias of restore), `help` and `version`.
Unknown commands and flags exit 2 naming the argument. `experimental_sync` and `sync` are recognized
only to report that they are unsupported.
- Criteria: every documented alias resolves; each command's own `--help` works (including `use`
  and `init`); `--version` prints version, commit and build date.
- Tests: TestParseCommands, TestParseUseOptions, TestParseInit, TestParseFind, TestHelpListsAgentsAndOnlyWorkingFlags, TestE2EExperimentalSyncStub

### Argument parsing
Repeatable list options are greedy in the space form and take one value in `--flag=value`; `--`
terminates options; dash-leading values are rejected; `*` is a literal expanded per option; more than
one positional source is rejected.
- Criteria: `--agent a b` collects two agents; `--agent=a` one; `--agent -y` errors; two sources
  error.
- Tests: TestParseAddOptions, TestParseUsageErrors, TestParseListAndCheck, TestParseRemoveOptions, TestParseUpdateOptions

### Interactive flow
When stdin is a terminal and no `-y`/`--json`, `add` asks, in order: skills, Eve confirmation,
Eve subagents, agents, scope, method, confirmation. A bare `notion` source adds a pack picker before
skill selection, starting with every pack checked. Each flag that supplies an answer removes its
prompt. The selector shows grouped skills with descriptions and a detail pane; agents show a locked
universal section; Eve subagents show root plus one entry per subdirectory.
- Criteria: the matrix holds; cancel at any step installs nothing and prints `Installation
  cancelled`; TTY cancel exits 0, non-TTY exits 1; `NO_COLOR` and `TERM=dumb` disable the TUI.
- Tests: TestPlanStepsMatchesThePromptMatrix, TestFlowCancelAtEveryStep, TestGoldenViews, TestSessionCancelReturnsErrCancelled, TestE2EEveSubagentPrompt, TestNotionPackSelectionPrompt, TestPacksPromptListsEveryPackByName, TestPacksPromptReturnsCheckedPacks, TestPacksPromptCancel

### Host-agent detection
Environment markers identify the agent running the tool (Claude, Codex, OpenCode, Gemini CLI,
GitHub Copilot, Cursor with its strong signals only, Antigravity, Augment, Devin, Replit and the
generic `AI_AGENT`). In host mode `add` becomes non-interactive, targets the mapped agent plus the
universal set unless `-a` is given, prints one informational line, and allows `--json` without `-y`;
`remove` and `find` also run non-interactively. Unmapped hosts fall back to ordinary detection.
- Criteria: one test per marker row; a bare `CURSOR_TRACE_ID` does not trigger host mode; `--agent`
  always wins.
- Tests: TestDetectHostTableRowPerMarker, TestDetectHostCursorDemotion, TestDetectHostUnmappedHostFallsBack, TestAddHostModeTargetsTheHostAgent, TestE2EHostAgentDetection

### Streams
stdout carries only product output: the `--json` document, `list` output, the `use` prompt, `find`
results, help and version. Human progress, warnings, prompts, summaries and `init` messages go to
stderr.
- Criteria: `--json` writes exactly one array on stdout; `use` writes only the prompt; errors leave
  stdout empty.
- Tests: TestE2EStreamSeparation, TestE2EJSONContract, TestE2EUsePrompt

### JSON for add
`--json` requires `-y`/`--all` unless running inside a host agent; incompatible with `--list`; one
array is written on every exit path. Entries carry `name`, `status`, `source`, `ref`, `hash`,
`path`, `scope`, `agents`, `mode`, plus `reason` or `error`. Exit 1 when an entry failed or was
skipped.
- Criteria: usage errors still yield valid JSON; host mode works without `-y`; no telemetry fields.
- Tests: TestE2EJSONContract, TestAddJSONNeverStartsTheFlow, TestE2EHostAgentDetection

### use
`use <source>[@skill] [--skill] [--full-depth] [--agent claude-code|codex|sarvam-code]` selects one
skill, materializes it into a `skills-use-*` directory that is kept, and prints the documented prompt
with no other stdout. With `--agent` it launches the agent with the prompt as one argv element,
inherits stdio and propagates its exit code.
- Criteria: prompt bytes match the golden; ambiguity and conflicts exit 1; `--skill` twice and `*`
  agents are rejected; a missing command reports `Could not launch`.
- Tests: TestBuildUsePromptGolden, TestSelectUseSkill, TestLaunchAgentPassesPromptAndStreams, TestE2EUsePrompt, TestE2EUseSelection, TestE2EUseAgentLaunch

### init
`init [name]` writes the documented `SKILL.md` template (YAML-escaped name) to `./SKILL.md` or
`./<name>/SKILL.md`, refuses to overwrite (message, exit 0) and rejects names that escape the working
directory, absolute paths and extra positionals (exit 2).
- Criteria: template matches the golden; traversal rejected; generated file parses.
- Tests: TestInitTemplateGolden, TestInitRejectsUnsafeNames, TestWriteInit, TestE2EInit

### init help
`skills init --help` prints a command-specific block on stdout, like `use`: the template target
(`./SKILL.md` or `./<name>/SKILL.md`), the overwrite rule, the traversal rejection and the exit codes.
It does not print the top-level help.
- Criteria: the block names the command and covers template, target, overwrite and traversal; stderr
  is empty and the exit code is 0.
- Tests: TestInitHelpNamesTheCommand, TestParseInitHelp, TestRunInitHelp, TestE2EInitHelp

### Unsupported command stubs
`experimental_sync` and its `sync` alias exist only as stubs for the excluded npm/`node_modules`
integration. They exit 2 with a message naming the feature, saying that integration is excluded, and
pointing at `skills add` for git or local sources; `--help` does not revive them. The top-level help
lists them under an explicit not-supported note, and no other command's behaviour or unknown-command
handling changes.
- Criteria: both spellings exit 2 with the explanation and the `add` pointer; help marks them as
  unsupported; unknown commands keep their existing error.
- Tests: TestParseUsageErrors, TestHelpListsAgentsAndOnlyWorkingFlags, TestE2EExperimentalSyncStub, TestE2EExperimentalSyncHelpStaysAStub

### find
`find [query...] [--owner]` searches the skills.sh endpoint (`SKILLS_API_URL` override), with the
documented partial-match fan-out, install-command rendering and badges. No query on a TTY runs an
interactive search; without a TTY it prints the tip. Network/API failures exit 1.
- Criteria: owner validation, exactly encoded query, fan-out caps, no-results message, interactive
  cancel.
- Tests: TestSearchExact, TestSearchPartialFanOut, TestWriteFindResults, TestRunSearchNeedsAFunction, TestRunSearchCancel, TestE2EFindKeyword, TestE2EFindAPIFailure

### One-time find-skills offer
After a successful interactive install, when `dismissed.findSkillsPrompt` is not set, the tool offers
to install `find-skills` globally; declining prints the manual command and either answer records the
dismissal. It never runs non-interactively.
- Criteria: asked once; dismissal persisted atomically; failures do not fail the original install.
- Tests: TestFindSkillsOfferDeclinedRecordsDismissal, TestE2EFindSkillsOfferOnce

### Exit codes
0 success; 1 failure, partial failure, no match, or a skipped entry in `--json`; 2 usage error,
unsupported scope or unknown option.
- Criteria: every command documents and honours the table.
- Tests: TestParseUsageErrors, TestE2ENoTTYPromptError, TestRemoveNoMatch

## Not supported

- `experimental_sync`/`sync` and the `package.json` `skills` field: explicit exit-2 stubs, no npm or
  `node_modules` integration.
- Telemetry, security-audit output and `--metadata`.
- Banner/logo output: the tool prints no decorative banner at all.
