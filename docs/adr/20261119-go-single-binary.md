# Go single-binary implementation

- Status: Proposed
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.

## Context

The upstream CLI is TypeScript on Node.js 22.20+, distributed through npm. Users need no runtime,
eight platform/architecture pairs (including 32-bit Linux and armhf), and fast startup. The behavior
to reproduce is documented in `olddocs/`; its source code is not being carried over.

## Decision

Implement the tool in Go (module `github.com/nhermesilla14/go-skills`, corrected to
`github.com/nhermosilla14/go-skills` in the module file) as a single statically linked binary named
`skills`, built from `cmd/skills`.

Dependency policy:

- Standard library first. HTTP, archives, JSON, hashing and process execution come from the
  standard library.
- Third-party packages are allowed only where the standard library is clearly inadequate, and each
  one is recorded in an ADR with its purpose: YAML frontmatter parsing, terminal UI, terminal size.
- Git access shells out to the `git` (and `gh`) binaries so that existing credentials, helpers and
  SSH agents keep working. No Git library, no credential storage.

## Alternatives considered

- Rust — rejected: correct for the requirements, but Go's cross-compilation of the eight targets
  with no toolchain per target and the contributor's current learning focus favour Go.
- Keep TypeScript with Bun/Deno single-file compilation — rejected: still a JavaScript runtime and
  does not cover armhf/32-bit Windows cleanly.
- A Git library (go-git) — rejected: reimplementing the GitHub CLI/credential-helper/SSH ladder is
  the highest-risk part of authentication; shelling out preserves upstream behavior exactly.

## Consequences

Cross-compilation is a single `GOOS`/`GOARCH` matrix. Platform-specific code is limited to path
resolution, link creation and file locking. The binary depends on `git` being installed for remote
sources; local sources work without it.
