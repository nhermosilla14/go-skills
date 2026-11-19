# Remote git sources and authentication

Work order in `docs/branch/intent/20261119-remote-git-sources.md`. Folded into `docs/spec/` and
deleted when the branch is finalized.

- Status: Proposed
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Depends on: `20261119-cli-foundation-and-local-install`

## Goal

A user can install skills from the git hosts upstream supports, public or private, pinned to a
branch, tag or commit, without storing any credentials in the tool.

## Acceptance criteria

- These source forms resolve to the correct repository, ref and subpath: `owner/repo`,
  `owner/repo@v1.2.3`, `owner/repo@skill-name`, `github:owner/repo`, full `https://github.com/...`
  URLs including `/tree/<ref>/<path>`, GitLab URLs, Azure DevOps URLs including `?path=` and
  `?version=`, `git@`/`ssh://` URLs, and generic HTTPS git URLs.
- A version-looking `@` suffix is treated as a ref pin; a skill-looking suffix as a skill selector,
  matching upstream's disambiguation rules.
- Private GitHub repositories install using, in order: ambient git credentials, `gh repo clone`,
  SSH. The tool never reads or prints a stored GitHub token.
- `GITHUB_TOKEN`/`GH_TOKEN`, when set, are used only for API access; absent authentication produces
  a message naming the attempts made.
- Installs are reproducible: the resolved commit is recorded, and re-installing that commit from a
  moving ref fetches the recorded commit.
- Clone and checkout happen in a temporary directory that is removed on success and on failure.
- Nested repositories and submodules are not followed; subpath escapes are rejected.
- Offline tests cover parsing, ref resolution and the auth ladder with fake git/gh executables; one
  optional network test exercises a real public repository.

## Spec changes

- Adds: `sources` / git source grammar and precedence
- Adds: `sources` / refs, tags and commit pinning
- Adds: `sources` / git authentication ladder
- Adds: `sources` / temporary workspace lifecycle

## Out of scope

- Well-known providers, direct downloads, archives and Notion (later intent).
- The GitHub blob fast path; clone-only is acceptable if installed results are identical.
- Update checks against remote refs (intent `skill-management-and-scopes`).

## Open questions

- Whether the GitHub blob fast path is worth its divergent semantics or should be dropped for
  clone-only. Decided in design.
