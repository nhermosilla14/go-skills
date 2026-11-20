# Sync stub and help polish

- Status: Done
- Intents: 20261119-sync-stub-and-help-polish
- ADRs: 20261119-upstream-parity-policy

## Scope

`experimental_sync` and `sync` exit 2 with an explanation that npm/`node_modules` integration is not
supported in this build and a pointer to `add`; help lists them under an explicit unsupported note or
omits them. `init --help` gains a short command-specific block. README, help and spec agree.

## End-to-end test

`TestE2EExperimentalSyncStub` asserts exit 2 and the message; `TestE2EInitHelp` asserts the block;
help snapshots stay stable.

## Out of scope

- Implementing sync.
