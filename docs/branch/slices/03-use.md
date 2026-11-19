# Use a skill

- Status: Done

- Intents: 20261119-use-and-init
- ADRs: 20261119-use-prompt-and-launch

## Scope

`skills use <source>[@skill]` with exactly one selected skill, `--skill`, `--full-depth`,
`--agent claude-code|codex|sarvam-code`. Materialize into `skills-use-*`, print the exact prompt on
stdout, or launch the agent with the prompt as one argv element and propagate its exit code. Errors
on stderr, stdout empty on failure.

## End-to-end test

`TestE2EUsePrompt` installs nothing, asserts stdout equals the documented prompt byte-for-byte and
stderr is empty; `TestE2EUseSelection` covers `@skill`, ambiguity, missing skill and `--full-depth`;
`TestE2EUseAgentLaunch` uses a fake `claude` executable on `PATH` and asserts argv, inherited stdio
and exit-code propagation.

## Out of scope

- Cleaning the kept temp directory.
- Agents beyond the three documented commands.
