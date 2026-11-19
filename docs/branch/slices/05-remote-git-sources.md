# Remote git sources and authentication

- Status: Done

- Intents: 20261119-remote-git-sources
- ADRs: 20261119-source-acquisition-clone-first, 20261119-package-architecture-and-dependencies

## Scope

Source grammar with upstream's precedence (shorthand, prefixes, GitHub/GitLab/Azure/generic/SSH URLs,
`tree` subpaths, `@ref` vs `@skill`), ref and commit resolution, shallow clone into a temporary
workspace, subpath containment, the auth ladder (ambient git → `gh repo clone` → SSH; API token for
metadata), and commit pinning recorded in the lock.

## End-to-end test

`TestE2EGitSourcePinnedCommit` and `TestE2EGitSourceAuthLadder` use local fixture repositories served
through `file://` plus fake `git`/`gh` executables to assert every accepted URL form, ref resolution,
pinned re-install and each authentication fallback. An optional network test installs a public
repository when `SKILLS_E2E_NETWORK=1`.

## Out of scope

- Well-known providers, direct downloads, archives and Notion (later branch).
- The GitHub blob fast path.
