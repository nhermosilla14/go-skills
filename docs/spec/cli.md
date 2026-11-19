# cli

Command surface, argument parsing, prompts, output streams, JSON documents and exit codes.

## Behaviours

### Commands and aliases
`skills` dispatches `add` (`a`, `install`, `i` with a source), `list` (`ls`), `remove` (`rm`, `r`),
`update` (`upgrade`), `check`, `help`, `version`. `install`/`i` without a source is rejected until
restore exists. Unknown commands and flags exit 2 naming the argument.
- Criteria: every documented alias resolves; `--help`, `<cmd> --help`, `--version` work; unknown
  input exits 2.
- Tests: TestParseCommands, TestHelpListsAgentsAndOnlyWorkingFlags, TestParseAddOptions

### Argument parsing
Repeatable list options are greedy in the space form and take one value in `--flag=value`; `--`
terminates options; dash-leading values are rejected; `*` is a literal that expands per option.
- Criteria: `--agent a b` collects two agents; `--agent=a` one; `--agent -y` errors.
- Tests: TestParseAddOptions, TestParseUsageErrors, TestParseListAndCheck, TestParseRemoveOptions, TestParseUpdateOptions

### Interactive flow
When stdin is a terminal and no `-y`/`--json`, `add` asks, in order: skills (when more than one and
none selected), Eve confirmation, agents, scope, method, confirmation. The matrix is a pure
function; `--skill`, `--agent`, `-g`, `--copy`, `--list` and `-y` each remove their prompt. The
selector shows grouped skills with descriptions and a detail pane, and agents with a locked
universal section.
- Criteria: the documented matrix holds; cancel at any step installs nothing and prints
  `Installation cancelled`; TTY cancel exits 0, non-TTY exits 1; `NO_COLOR` and `TERM=dumb` disable
  the TUI; narrow terminals degrade without panics.
- Tests: TestPlanStepsMatchesThePromptMatrix, TestFlowCancelAtEveryStep, TestGoldenViews, TestSessionCancelReturnsErrCancelled

### Streams
stdout carries only product output: the `--json` document, `list` output, `use` prompts, help and
version. Human progress, warnings, prompts and summaries go to stderr.
- Criteria: `--json` writes exactly one array on stdout and every human line on stderr; `list`
  writes its table on stdout.
- Tests: TestE2EStreamSeparation, TestE2EJSONContract

### JSON for add
`--json` requires `-y` or `--all` and is incompatible with `--list`; one array is written on every
exit path, including usage errors. Entries carry `name`, `status`, `source`, `ref`, `hash`, `path`,
`scope`, `agents`, `mode`, plus `reason` or `error`. Exit 1 when an entry failed or was skipped.
- Criteria: invalid input still yields valid JSON; no `security` or `fallback` fields.
- Tests: TestE2EJSONContract, TestAddJSONNeverStartsTheFlow

### Exit codes
0 success; 1 failure, partial failure, no match, or a skipped entry in `--json`; 2 usage error,
including unsupported scope or command.
- Criteria: every command documents and honours the table.
- Tests: TestParseUsageErrors, TestE2ENoTTYPromptError, TestRemoveNoMatch

## Not supported

- `use`, `find`, `init`, `install` restore and the `experimental_*` commands (later branches).
- `update` across project and global scope in one run; the picker offers one scope.
- Telemetry, security-audit output and `--metadata`.
- `--subagent` is parsed but Eve subagent installs are not implemented.
