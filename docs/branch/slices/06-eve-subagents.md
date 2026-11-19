# Eve subagents

- Intents: 20261119-eve-subagents
- ADRs: 20261119-subagents-and-host-detection

## Scope

Subagent target discovery under `agent/subagents/`, the interactive multi-select, `--subagent
root|name`, flat `<name>.md` copies per selected subagent, the lock `subagents` field, and
subagent-aware list, update and remove. Global scope rejects Eve.

## End-to-end test

`TestE2EEveSubagents`: an Eve project with two subagents installs to root plus one subagent, asserts
the files and lock field, updates after an upstream change, removes one subagent without touching
the other, and lists the placement. `TestE2EEveSubagentFlag` covers `root`, `.` and named targets.

## Out of scope

- Subagents for other agents.
