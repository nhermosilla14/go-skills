# Release pipeline and version metadata

- Intents: 20261119-release-engineering
- ADRs: 20261119-release-targets-and-pipeline

## Scope

GoReleaser configuration for the eight target pairs, `.zip`/`.tar.gz` archives with checksums,
version/commit/date linker flags and `skills --version`, a CI matrix that builds and tests on the
three host OSes, cross-compiles all targets, runs the merge gate and lints, and a smoke script that
executes the built binary where possible.

## End-to-end test

`TestE2EVersionMetadata` builds with known linker flags and asserts `--version` output;
`scripts/smoke.sh` runs `--version`, `--help` and a local install against each locally executable
artifact; the release workflow's `goreleaser build --snapshot` step is exercised in CI.

## Out of scope

- Signing, notarization, package-manager taps.
