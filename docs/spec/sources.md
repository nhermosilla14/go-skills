# sources

Parsing a source argument and acquiring the tree it points at.

## Behaviours

### Source grammar
Accepted forms: local paths, GitHub shorthand `owner/repo` with `//subpath`, `@ref` or `@skill`,
`github:`/`gitlab:` prefixes, GitHub and GitLab HTTPS URLs including `/tree/<ref>/<subpath>`,
Azure DevOps URLs with `?path=` and `?version=GB<ref>`, scp-style and `ssh://` URLs, generic
`https`/`git`/`file` URLs, and `#ref` fragments. A version-looking `@` suffix is a ref, a
skill-looking one is a selector. Credentials in URLs are rejected; subpaths must stay inside the
repository.
- Criteria: the parser table covers every form; `..`, absolute subpaths and userinfo are rejected.
- Tests: TestParse, TestParseLocalPaths, TestParseRejects, TestValidateSubpath

### Acquisition
Git sources are shallow-cloned into a temporary workspace; a 40-hex pin fetches that commit; the
resolved commit is recorded. Local sources are used in place. The workspace is removed on success
and on every error path. Submodules are never followed.
- Criteria: clone, tag, commit pin, moved tag, subpath and cleanup behave; a pinned install still
  fetches the pinned commit after the ref moves.
- Tests: TestAcquireClonesDefaultBranch, TestAcquireTagRef, TestAcquireCommitPinSurvivesMovedTag, TestAcquireSubpath, TestE2EGitSourcePinnedCommit

### Authentication ladder
Attempts ambient `git` first, then `gh repo clone` for GitHub over HTTPS, then SSH. Retries only on
the documented authentication and 401/403/404 patterns; a server error is not retried. Credentials
are never read, printed or recorded.
- Criteria: each fallback triggers only on the matching failure; reported messages are redacted.
- Tests: TestAcquireAuthFirstAttemptSucceeds, TestAcquireAuth403FallsBackToGH, TestAcquireAuthGHMissingFallsBackToSSH, TestAcquireAuthServerErrorDoesNotRetry, TestAcquireAuthRedactsCredentials

### Remote ref lookup
`check` compares the lock commit with the remote head of the recorded ref using `git ls-remote`,
preferring peeled tags and branches, without cloning.
- Criteria: reported head matches the remote; failures are reported, not ignored.
- Tests: TestRemoteHeadDefaultRef, TestRemoteHeadPrefersThePeeledTag, TestRemoteHeadPrefersTheBranch, TestRemoteHeadFailures

## Not supported

- Well-known providers, direct downloads, archives, Notion and `npm:` sources (later branches).
- The GitHub blob fast path; every git source is cloned. Installing from a GitHub repository
  therefore requires `git` on `PATH`.
- GitHub Enterprise aliases and mirror/proxy rewriting.
