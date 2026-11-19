# Interactive flow and JSON contract

- Intents: 20261119-interactive-install-flow
- ADRs: 20261119-tui-architecture, 20261119-upstream-defect-corrections

## Scope

The Bubble Tea flow for `add`: scope, grouped skill multi-select with detail pane, agent
multi-select, install method, confirmation; prompt matrix as a pure function; cancellation and
interrupt semantics; `--json` as the only stdout product; `--all`, `-y`, `--list`, `--rename`,
`--subagent`, `--full-depth`; no-TTY refusal instead of hanging; ASCII fallback and `NO_COLOR`.

## End-to-end test

`TestE2EJSONContract` covers every documented `--json` success and failure shape with exactly one
document on stdout. Golden-frame tests cover 40/80/120 columns. `TestE2EInteractiveCancel` drives the
built binary with piped keystrokes and asserts no changes and the documented exit code.

## Out of scope

- `find`'s interactive search, `remove` prompts (reuse the component, but delivered later).
