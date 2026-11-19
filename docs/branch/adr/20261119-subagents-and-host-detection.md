# Eve subagents and host-agent detection

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `install`, `agents`, `cli`

## Context

Two parity behaviours remain that cut across existing packages. Eve projects can place a skill with
a specific subagent, and upstream detects when it is being run *by* an agent, switching to a
non-interactive install targeted at that agent. Both change install targets, so they are decided
together.

## Decision

**Eve subagents.**
- Targets are `agent/skills` (root, recorded as `""`) and `agent/subagents/<name>/skills` for each
  subdirectory of `agent/subagents/` (sorted). The install is always the Eve flat `<name>.md` copy
  with the reduced frontmatter.
- Interactive installs offer a multi-select of root plus subagents when `agent/subagents/` exists
  and neither `-y` nor `--subagent` was given, with at least one required. `--subagent` accepts
  `root`/`.` and names; names are not validated against directories, matching upstream.
- The project lock records the placement in `subagents`, and `update` re-applies it, `remove`
  deletes exactly those files, and `list` shows them. Global scope rejects Eve and subagents.

**Host-agent detection.**
- Detection is an environment-variable mapping to supported agent ids, evaluated once at start:
  `CLAUDECODE`/`CLAUDE_CODE_ENTRYPOINT` → `claude-code`, `CODEX_SANDBOX`/`CODEX_HOME` →
  `codex`, `CURSOR_TRACE_ID` → `cursor` (demoted when another marker is present), `OPENCODE`/
  `OPENCODE_BIN` → `opencode`, `GEMINI_CLI` → `gemini-cli`, `COPILOT_AGENT` →
  `github-copilot`, plus the documented CI-style markers for the remaining hosts. The list lives in
  one table with a test per row.
- When detected and no `-a` is given, the run is non-interactive (`-y` semantics) and targets that
  agent plus the universal set. `--agent` always wins; `--json` is allowed without `-y` in this
  mode. The banner is suppressed and one line reports the detected agent.

## Alternatives considered

- Validate subagent names against the filesystem — rejected: upstream does not, and names may be
  created later.
- Detect agents by process name or `PATH` — rejected: unreliable in containers and not what
  upstream does.
- Treat every unknown environment marker as a host agent — rejected: false positives would install
  without consent.

## Consequences

Eve placements survive updates and removals. Running under an agent feels native and avoids prompts
that cannot be answered. The environment mapping is data with tests, so extending it is cheap when
new hosts appear.
