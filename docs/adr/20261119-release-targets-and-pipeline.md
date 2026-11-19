# Release targets and pipeline

- Status: Accepted
- Date: 2026-11-19
- Approved by: Nicolás Hermosilla P.
- Deciders: Nicolás Hermosilla P.

## Context

The tool is useful only if every supported platform gets a working binary without a local
toolchain. Upstream's npm distribution excludes 32-bit Linux, armhf and Windows x86.

## Decision

Build and publish with **GoReleaser** driven by **GitHub Actions** on tag push:

| OS | Architecture | Go target |
|---|---|---|
| Windows | x86, x86_64 | `windows/386`, `windows/amd64` |
| Linux | x86, x86_64, aarch64, armhf | `linux/386`, `linux/amd64`, `linux/arm64`, `linux/arm` (`GOARM=6`) |
| macOS | Intel, Apple Silicon | `darwin/amd64`, `darwin/arm64` |

Artifacts are `.zip` on Windows and `.tar.gz` elsewhere, with a `checksums.txt`. Version, commit and
build date are injected through linker flags and reported by `skills --version`. Release notes are
generated from Conventional Commits.

Signing and notarization are out of scope until a release actually needs them (no Apple Developer ID
exists today); this is tracked as an open TODO in the README.

## Alternatives considered

- Hand-written cross-compilation in a Taskfile — rejected: fewer moving parts, but no checksums,
  changelog or artifact naming conventions, and more YAML to maintain than the GoReleaser config.
- Server-side build service — rejected: unnecessary for a static Go binary.
- Ship only source — rejected: defeats the "no toolchain" goal for end users.

## Consequences

Releases are reproducible from a tag and a clean checkout. GoReleaser becomes a repository tool that
must be installed for local snapshot builds (`task release:snapshot`) and is pinned in CI.
