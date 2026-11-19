# Release engineering

Work order in `docs/branch/intent/20261119-release-engineering.md`. Folded into `docs/spec/` and
deleted when the branch is finalized.

- Status: Accepted
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

Every merge produces a binary that runs on all eight supported platform pairs, and a tagged release
publishes installable artifacts with checksums and honest version metadata, without a Node.js
runtime anywhere in the chain.

## Acceptance criteria

- CI builds and tests on Linux, macOS and Windows for the supported Go versions, and cross-compiles
  all eight targets from one commit.
- `skills --version` prints semantic version, short commit and build date, injected at build time;
  a local `go build` still works with development defaults.
- A tagged release produces `.zip` artifacts for Windows and `.tar.gz` for Linux/macOS, each with a
  `checksums.txt`, covering `windows/386`, `windows/amd64`, `linux/386`, `linux/amd64`,
  `linux/arm64`, `linux/arm` (`GOARM=6`), `darwin/amd64` and `darwin/arm64`.
- A smoke test runs the built binary for each target it can execute: `--version`, `--help`, and an
  install from a local fixture into a temporary project.
- The pipeline fails if `gofmt`, `go vet`, `golangci-lint` or `go test ./...` fails, or if the merge
  gate script fails.
- The README documents installation per platform and the release process; no command is documented
  that does not work.

## Spec changes

- Adds: `release` / build metadata and version command
- Adds: `release` / artifact matrix and checksums
- Adds: `release` / CI and merge gate

## Out of scope

- Code signing and Apple notarization (no signing identity today; recorded as a TODO).
- Package-manager distribution (Homebrew, Scoop, apt) — later, if wanted.
- Reproducible-build attestations.

## Open questions

- Whether the binary should also be published as a container image. Proposed: no, not until asked.
