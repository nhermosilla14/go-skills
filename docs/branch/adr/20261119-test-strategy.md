# Test strategy

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Context

The project must reproduce documented behavior of `vercel-labs/skills` v1.9.0 across eight
platform pairs, with three tier-1 data contracts (`docs/adr/20261119-upstream-parity-policy.md`):
the project lock (v2), the global lock (v3) and the content hash. Seventeen deliberate deviations
are listed in `docs/adr/20261119-upstream-defect-corrections.md` and must be asserted, not assumed.

Upstream's own suite (documented in `olddocs/technical/01-architecture.md`) does not transfer:

- Its e2e suite hits live GitHub, skills.sh and the npm registry, caps workers to dodge anonymous
  rate limits, retries once in CI, and is not run on Windows or on any 32-bit/arm target.
- It has no coverage measurement, no documented rule for which layer a test belongs to, and no
  guard against a test that silently starts depending on the network or the wall clock.
- It has no oracle for the four 32-bit targets that motivate this project at all.

The remaining constraints are in `docs/vision.md`: tests are offline by default, git and HTTP sit
behind interfaces, and no command may contact the network unless it is fetching, searching or
checking for updates. A test suite that needs the network cannot verify that property, cannot run
on `windows/386` or `linux/arm`, and fails for reasons unrelated to the change under review.

## Decision

### 1. Five layers, one location rule

Tests live next to the code they test (`internal/<pkg>/*_test.go`), except end-to-end tests, which
live in `e2e/`. Every layer runs under `go test`, so `go test ./...` is always the answer to "does it
pass"; there is no second runner and no script that is the real gate.

| Layer | Location | Proves | Speed | Where it runs |
|---|---|---|---|---|
| Unit (pure) | `internal/<pkg>/*_test.go` | Source grammar and precedence, `sanitizeName`, byte-order sorting, UTF-16 key ordering, hash collection/framing, exit-code mapping, JSON document shapes, TUI renderer output, agent path tables | ms per package | Every `go test ./...`, all three CI OSes |
| Golden | `internal/<pkg>/testdata/golden/`, `internal/<pkg>/testdata/*.golden` | Byte-exact artifacts: project/global lock JSON, `--help` and per-command help, TUI frames, plan JSON, per-agent path tables | ms–s | All three CI OSes |
| Integration | `internal/<pkg>/*_test.go` using `t.TempDir()` | Real filesystem effects: canonical install + links/copies, atomic replace, mutex contention, v1 migration, clone into a temp tree, corrupt-lock reporting | 100 ms–2 s | All three CI OSes |
| End-to-end | `e2e/` (`package e2e`) | The built binary as users invoke it: exit codes, stdout/stderr separation, flag combinations, full add→list→check→update→remove flows, version metadata | 20–120 s for the suite | All three CI OSes, full suite only (skipped under `-short`) |
| Platform-specific | `*_windows_test.go`, `*_unix_test.go`, `internal/agents/paths_test.go` | Junctions and copy fallback, reserved device names, case-insensitive containment, XDG vs `%APPDATA%`, CRLF normalization | ms | The OS the file is named for; path tables golden on all three |

Rules that keep the layers from blurring:

- A test that needs a real directory is integration, not unit. Unit tests may use `t.TempDir()` only
  as scratch space, never as the thing under test.
- A test that asserts on user-visible text or bytes of an installed artifact is a golden test, even
  if it is short.
- Nothing below `e2e/` executes the built binary, and `e2e/` never imports `internal/...` except
  `internal/testutil` helpers. If a behavior can only be observed through the binary, that is a
  finding: move the seam instead of growing e2e.
- Static checks (`gofmt`, `go vet`, `golangci-lint` v2) are gates, not a layer: they run once per OS
  in the merge gate (`.github/workflows/merge-gate.yml`), never per package.

Naming: standard Go. `TestXxx` in `xxx_test.go`, table-driven subtests via `t.Run` named after the
input (`t.Run("owner/repo@v1.2.3", ...)`), one exported behavior per test function. Test helpers are
`newTestXxx(t *testing.T)` and take `t` first so `t.Helper()` and `t.Cleanup` work. The first
concrete files are `internal/hash/hash_test.go`, `internal/lockfile/lockfile_test.go`,
`internal/agents/paths_test.go`, `internal/source/parse_test.go`, `internal/cli/help_test.go`,
`internal/fsutil/atomic_test.go`, `internal/app/add_test.go`.

Fuzzing is part of the unit layer, not a separate job: `FuzzParseSource`, `FuzzSanitizeName`,
`FuzzHashFraming` and `FuzzReadFrontmatter` keep their seed corpora in `testdata/fuzz/`, so every
`go test` replays them, and a nightly job runs each for 60 s.

### 2. Offline discipline

Every test passes with no network. That is enforced structurally, not by convention:

- **HTTP** is an injected interface, never `http.Get`. `internal/source` takes
  `HTTPClient interface { Do(*http.Request) (*http.Response, error) }` and an API base URL in its
  constructor; `cmd/skills` wires `http.DefaultClient` and the real hosts. Tests pass
  `httptest.NewServer` URLs, which bind `127.0.0.1`. `internal/testutil/httpstub.go` provides
  `newHTTPStub(t)` that fails the test if a request leaves loopback.
- **Git** is tested against repositories the test creates, never a remote: `internal/testutil/gitrepo.go`
  exposes `newGitRepo(t)` which runs `git init` in `t.TempDir()`, writes a tree, commits with
  `GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE` fixed to `2000-01-01T00:00:00Z`, and returns a `file://`
  URL. Fixed dates make commit SHAs reproducible, which lets golden locks contain real SHAs.
  Environments are isolated with `GIT_CONFIG_NOSYSTEM=1`, `HOME` pointed at the temp dir,
  `core.autocrlf=false` and `core.symlinks=true`. The only daemon is `git daemon
  --base-path=<tmp> --listen=127.0.0.1` with an ephemeral port, used by one test that must exercise
  the `git://` grammar (`TestGitDaemonSource`); it is started by the test and killed in
  `t.Cleanup`.
- **`git` missing is a failure, not a skip.** git is a declared build/test dependency for
  `internal/source` and `e2e`; the error names `git` and the install hint. Only OS capabilities get
  skips (see §5).
- **External executables (`git`, `gh`, `ssh`, `ntn`, the launched agent)** are faked with a single
  helper binary compiled from `internal/testutil/faketool/main.go`. Its behavior is selected by the
  name it is invoked as (`git`, `gh`, `ssh`, ...) and a JSON script whose path is in
  `FAKETOOL_SCRIPT`; every invocation is appended to `FAKETOOL_LOG` as argv + stdin. `newFakeBin(t)`
  builds it once per test binary into `<tmp>/bin/{git,gh,ssh}.exe` and prepends that directory to
  `PATH`. This is portable (a copy named `gh.exe` works on Windows, where a shebang script does
  not) and makes the auth ladder assertable as a sequence: ambient git → `gh repo clone` → SSH, with
  the exact argv of each attempt.
- **Time** is injected. `internal/clock` is the only non-test package allowed to call `time.Now`;
  `internal/app` and `internal/lockfile` take a `Clock interface { Now() time.Time }`.
  `testutil.FixedClock(t, "2026-01-01T00:00:00Z")` is the test implementation, and mutex retries take
  a `Sleep func(time.Duration)` that tests replace with an immediate recorder. CI greps for
  `time.Now(` outside `internal/clock` and fails.
- **Environment** is set per test with `t.Setenv` or by building `cmd.Env` explicitly in `e2e/`. No
  test reads the developer's `HOME`, `XDG_*`, `GITHUB_TOKEN` or git config.

### 3. Compatibility fixtures

Two kinds of fixtures are kept apart, because one is evidence and the other is output:

- **`testdata/fixtures/` — upstream evidence, inputs only.** Never written by a test, never produced
  by this tool.
  - `locks/project-v2.json`: a `skills-lock.json` written by upstream v1.9.0, covering a GitHub
    entry with `ref`/`commit`/`skillPath`, a local entry with a portable source, and a download
    entry with `sourceUrl`.
  - `locks/global-v3.json`: an upstream `.skill-lock.json` with a 40-hex `skillFolderHash`, a 64-hex
    `skillFolderHash`, `installedAt`/`updatedAt`, `dismissed` and `lastSelectedAgents`.
  - `locks/project-v1.json`: a legacy v1 lock, with `locks/v1-tree/` holding the installed folder
    whose legacy hash it records, so the "matches → v2 hash" and "does not match → empty hash"
    branches have a fixture each (`project-v1-stale.json`).
  - `locks/project-v2-renamed.json`: lock key `acme-pdf` with `skillName: "pdf"`.
  - `locks/project-v2-well-known.json`: `sourceType: "well-known"`, `wellKnownDigest`, and an empty
    `computedHash` to pin the "unverifiable" status.
  - `hash-vectors/{empty,single-file,two-files}/` plus `hash-vectors/vectors.json` mapping each
    directory to its published digest. `internal/hash/hash_test.go` reads the directories, not
    in-memory maps, so collection and hashing are both exercised.
  - `PROVENANCE.md`: per fixture, the upstream version, the exact command that produced it, and the
    date. No fixture exists without an entry.
- **`internal/<pkg>/testdata/golden/` — this tool's output, regenerated on purpose.** Lock documents
  (`project-v2.json.golden`), help text, TUI frames, path tables. Regenerated with
  `task golden:update`, reviewed as a diff.

Protection against accidental edits: `testdata/fixtures/fixtures.sha256` lists the SHA-256 of every
evidence fixture, and `internal/compat/fixtures_test.go` (`TestFixturesUnmodified`) verifies it. The
guard lives outside `testdata/` because `go list ./...` skips `testdata` directories. Changing a
fixture means running `go test ./internal/compat -run TestFixturesUnmodified -update-fixtures`,
which rewrites the manifest and requires a `PROVENANCE.md` line explaining why upstream evidence
changed — visible in review, impossible to do by accident. `.gitattributes` marks
`testdata/** -text` so no platform rewrites fixture bytes.

### 4. Determinism

- **Ordering is asserted, never assumed.** Every list the tool prints has a test that asserts the
  exact slice (`reflect.DeepEqual` against a literal), including a fixture built in reverse creation
  order so "happens to be sorted" cannot pass. The same applies to `skills-lock.json` keys (UTF-16
  code-unit order, asserted with a name that differs between UTF-8 and UTF-16 order, e.g. an
  astral-plane emoji next to `z`) and to entry field order (golden bytes).
- **Golden files carry the byte contract.** They are compared as bytes after confirming they are LF
  only, so trailing newlines, indentation and float/int spelling are all pinned. `internal/testutil/golden.go`
  reports the first differing line with three lines of context; no third-party diff library.
- **No clock, locale, or OS dependence.** `internal/testutil.Main(m *testing.M) int` sets `TZ=UTC`,
  `LANG=C`, `LC_ALL=C`, `NO_COLOR=1`, `TERM=dumb`, clears `CI` and the agent marker variables, and
  is called from each package's `TestMain`. Timestamps in locks come from `Clock`:
  `TestGlobalLockTimestamps` installs with `FixedClock(t, "2026-01-01T00:00:00Z")`, updates with
  `FixedClock(t, "2026-02-01T00:00:00Z")`, and asserts `installedAt` stays fixed while `updatedAt`
  moves. The project lock has no timestamps at all, and a test asserts that
  (`TestProjectLockHasNoTimestamps`).
- **Map iteration never reaches output.** JSON is produced from structs and ordered slices; where
  keys are dynamic (`skills`), the writer sorts explicitly before encoding and the result is a
  golden file.
- **Filesystem metadata is not asserted where it is synthetic.** Mode bits are compared only on
  POSIX (`slices.Equal` on `fs.FileMode.Perm()`), never on Windows.

### 5. Cross-platform coverage and the skip policy

CI runs `go test ./...` on `ubuntu-latest`, `macos-latest` and `windows-latest` with the Go version
from `go.mod`, plus `go test -race -count=1 ./...` on `ubuntu-latest` only, plus a cross-compile job
that builds every target with `GOOS/GOARCH` (including `linux/386`, `linux/arm` with `GOARM=6`,
`windows/386`) and runs `go vet` for each. 32-bit and arm binaries are built, not executed: no
runner exists for them and the logic under test is not word-size dependent.

What cannot run, and what happens instead:

| Case | Behavior |
|---|---|
| Junction creation on Windows without the privilege | The test attempts the junction; on `ERROR_PRIVILEGE_NOT_HELD` it asserts the documented copy fallback (independent files, no link inode) and then calls `t.Skip` with the capability name. The fallback assertion is the real test; the skip only removes the junction half. |
| Symlink creation on Windows (POSIX-style links) | Same pattern: assert copy fallback, then skip the link assertion. On POSIX the link assertion is unconditional. |
| Reserved device names (`CON`, `NUL`, `COM1`, `aux.txt`) | Pure path logic is unit-tested on every OS; one `_windows_test.go` test creates the directory to prove the filesystem agrees. |
| Case-insensitive containment | The decision function is unit-tested on every OS with both casings; macOS (APFS, case-insensitive by default) runs it end to end. Linux adds the case-sensitive counterpart. |
| Locking and atomic replace under concurrent processes | Re-exec pattern (`GO_WANT_HELPER_PROCESS=1`, `-test.run=TestHelperLockWriter`) on all three OSes: four processes contend, every entry survives, no `.tmp`/`.lock` file remains. |
| 32-bit / arm execution | Not tested. Covered by cross-compilation and by the GoReleaser snapshot smoke in `e2e/release_test.go` for targets the runner can execute. |

Skip policy: a skip is allowed only for a missing OS capability or a platform to which the test does
not apply. It must name the capability and the platform in the message, and it must follow an
assertion that the degraded path works. Platform conditionals live in `_windows_test.go` and
`_unix_test.go` build-tagged files, not in `if runtime.GOOS == ...` inside a shared test, so the code
either exists for that platform or does not compile there. A capability skip that fires in CI is
recorded as `Unverified:` in the corresponding `docs/spec` entry, because a green run must not
imply coverage it does not have.

### 6. End-to-end tests through the CLI

`e2e/main_test.go` builds the binary once in `TestMain`:

```
go build -trimpath -o <tmp>/skills ./cmd/skills
    -ldflags "-X main.version=0.0.0-test -X main.commit=0000000 -X main.date=2000-01-01T00:00:00Z"
```

`SKILLS_E2E_BIN` overrides the build, so the same suite can run against a released artifact. The
build output path is passed through a package-level variable; every test gets a fresh `sandbox`
(`e2e/harness_test.go`: `newSandbox(t)` → temp `home/` and `project/`, an allow-listed `cmd.Env`,
`NO_COLOR=1`, `TERM=dumb`, no `CI`, no agent markers). `run(t, sandbox, args...)` returns
`Result{Stdout, Stderr string, Exit int}` from separate buffers, with `Exit` from
`(*exec.ExitError).ExitCode()`. Tests assert on each stream independently; nothing parses combined
output, and no assertion strips ANSI because none is emitted without a TTY.

TTY-specific behavior is not driven through a pseudo-terminal. The TUI is a pure renderer + key
reducer tested in `internal/cli/tui/render_test.go` with golden frames for widths 40, 80 and 200 and
scripted key sequences (`space`, arrows, `a`, `enter`, `esc`, `ctrl+c`), and the TTY/non-TTY exit
difference is a unit test of the injected `isTTY` flag. e2e always runs without a TTY and asserts
the documented non-TTY errors. This keeps a third-party pty dependency out of the module.

First scenarios, one file per intent area, each named for the acceptance criterion it proves:

| Intent | e2e test | Asserts |
|---|---|---|
| `cli-foundation-and-local-install` | `e2e/add_local_test.go:TestE2EAddLocalInstall` | Skills at root, `skills/<name>/` and `skills/<cat>/<name>/` land in `.agents/skills/<name>/` and `.claude/skills/<name>`; the lock validates against the v2 schema and its `computedHash` equals the published vector |
| " | `TestE2EHelpVersionAndUsageErrors` | `--help`, `<cmd> --help`, `--version` exit 0; unknown flag and extra positional exit 2 with the offending argument in stderr |
| " | `TestE2EAddNonTTYWithoutYes` | Exits non-zero with a message, does not hang (bounded by the test deadline) |
| " | `TestE2ERenameAndCopy` | `--rename` rewrites folder, lock key and frontmatter `name` while recording `skillName`; `--copy` produces independent files |
| `agent-registry-and-install-modes` | `e2e/agents_test.go:TestE2EAgentMatrix` | Every one of the 79 ids installs into its resolved project path; the path table matches the golden `internal/agents/testdata/golden/project-paths.golden` |
| " | `TestE2ESymlinkAndCopyModes` | Relative link target on POSIX; junction on Windows; copy forced by `--copy`, by a single shared directory, and for Eve |
| " | `TestE2EDetectionAndAgentTargeting` | `-y` with nothing detected targets all agents; a single detected agent is chosen; an agent marker selects that agent |
| `remote-git-sources` | `e2e/remote_git_test.go:TestE2EGitSourcePinnedCommit` | Install from a `file://` repo at a tag records the commit; re-installing the recorded commit works after the tag moves; `owner/repo@v1.2.3` vs `owner/repo@skill` disambiguation |
| " | `TestE2EAuthLadder` | With fake `git`/`gh`/`ssh`, the attempt order is ambient → `gh repo clone` → SSH; no token is read or printed; the failure message names the attempts |
| " | `TestE2ESubpathEscapeRejected` | `../` and symlink escapes in `skillPath` are rejected before any write |
| " | `TestE2ETempWorkspaceCleanup` | No `skills-*` directory remains in the temp root after success or failure |
| `interactive-install-flow` | `e2e/json_test.go:TestE2EJSONContract` | `add --json` and `list --json` emit exactly one JSON document on stdout for success and for each failure; human text is on stderr; every entry has skill, agent, status, path and error |
| " | `TestE2EFlagPrecedence` | `--all`, `--skill '*'`, `--agent '*'`, `--list` and `-y` combinations, including the rejected ones |
| `skill-management-and-scopes` | `e2e/management_test.go:TestE2EManagementRoundTrip` | add → list → check → update → remove on project and global scope leaves no orphaned files, links or lock entries (S5) |
| " | `TestE2EListStatuses` | `tracked`, `modified`, `untracked`, `missing`, with a hash-less entry reported as `modified` by both `list` and `check` |
| " | `TestE2ERemoveSelectionAndExitCodes` | `--agent '*'`, no-match exit non-zero, failed removal exit non-zero, untracked directories untouched |
| " | `TestE2EGlobalLockAtomicity` | Global lock is replaced atomically, a corrupt one is reported and left byte-identical |
| " | `TestE2EV1MigrationAndFrozenLock` | v1 lock upgraded on read; matched entries get v2 hashes; a stale entry gets `""`; `--frozen-lockfile` fails with the specific message and writes nothing |
| `release-engineering` | `e2e/release_test.go:TestE2EVersionMetadata` | `--version` prints version, short commit and build date from the injected linker flags; a plain `go build` still reports development defaults |
| " | `TestE2EReleaseSmoke` | For each artifact produced by `task release:snapshot` that the host can execute: `--version`, `--help`, and an install from a local fixture into a temp project |

Later intents (`use`, `find`, `init`, restore-from-lock) add scenarios to the same files; the rule is
that every acceptance criterion in an intent maps to at least one named e2e test or to a spec entry
that says why it is `Unverified:`.

### 7. No Node CLI oracle in CI

The upstream CLI is not run in CI as a differential oracle. It is used as the source of the
compatibility fixtures in `testdata/fixtures/`, captured once with the commands recorded in
`PROVENANCE.md`.

Reasons: it needs a Node 22.20+ toolchain and an npm install on all three CI OSes, which is the
runtime this project exists to avoid; its useful paths reach GitHub and skills.sh, so a differential
run is a network test with rate limits, retries and outages in the merge path; upstream is a moving
tag, so a pinned version is the only defensible option and fixtures already pin it exactly; and it
cannot run on `linux/386`, `linux/arm` or `windows/386`, so it can never validate the platforms that
motivate the port. The parity tiers that matter are data formats, and those are pinned as bytes.

What exists instead: `scripts/parity-check.sh`, run manually before a release, which takes a corpus
of sources, installs it with both CLIs, and diffs `skills-lock.json`, `.skill-lock.json` and the
installed trees. It is documented in `README.md`, needs network and Node, is not wired into CI, and
its output is a report a human reads.

### 8. Coverage policy

No percentage gate and no coverage threshold. `task test:cover` prints `go test -cover` numbers and
they inform review; they never fail a build.

Three rules replace the gate:

1. **Every spec entry names its test.** Each entry in `docs/spec/<area>.md` ends with
   `Tests: TestA, TestB` or `Unverified: <reason>`. `scripts/check-spec-tests.sh` (added to the
   merge gate next to `scripts/check-branch-docs.sh`) extracts every backticked `TestXxx` from
   `Tests:` lines and fails if no `func TestXxx(` exists in the module. Adding behavior without a
   test means the spec entry cannot be written, and the merge gate refuses it.
2. **Data contracts require tests unconditionally.** Any change to the project lock, the global
   lock, the content hash, the install layout, the frontmatter contract or the `use` prompt is not
   reviewable without: a hash vector test in `internal/hash/hash_test.go`, a lock golden in
   `internal/lockfile/testdata/golden/`, a round-trip test against the matching upstream fixture,
   and an entry in the same PR. A tier-1 change also needs the ADR the parity policy requires.
3. **Every defect correction has a named test.** `internal/app/corrections_test.go` holds
   `TestCorrection01RemoveAcceptsAllAgents` through `TestCorrection17MetadataFlagRemoved`, one per
   row of `docs/adr/20261119-upstream-defect-corrections.md`. A new correction adds a row and a
   test in the same commit; the numbering is the ADR's, not the file's.

### 9. Flake prevention and hygiene

- No `time.Sleep` in tests. Waits are channel receives, context deadlines, or a bounded poll helper
  (`testutil.waitFor(t, deadline, cond)`) whose failure message prints the last observed state. The
  file mutex is tested with an injected sleeper that returns immediately, so its backoff is not
  wall-clock dependent.
- No wall-clock assertions: nothing compares `time.Since` to a threshold, and nothing asserts a
  timestamp the code did not receive from the injected clock.
- `t.TempDir()` for every path a test writes, and no test sets a process-global `HOME` or `cwd`.
  `os.Chdir` is forbidden (it is not parallel-safe); `internal/app` takes a `WorkDir` in its options.
- No two tests write the same file. Concurrency tests that must share a file share it inside one
  `t.TempDir()` and use `t.Parallel()` nowhere.
- `t.Parallel()` is allowed only in pure unit and golden tests. It is forbidden in tests that call
  `t.Setenv`, mutate `PATH`, or spawn a fake-binary tree, because those are process-wide.
- `-race -count=1` on `ubuntu-latest` for every package; the mutation tests around the lock mutex
  and the bounded source fan-out are the reason.
- Git fixtures pin author/committer name, email, date, default branch, and line-ending config, so a
  developer's global git config cannot change a fixture's commit SHA.
- Every test that touches the filesystem removes its leftovers via `t.Cleanup`, and the lock tests
  assert no `.tmp`, `.lock` or `.stale` file survives.
- CI runs the suite once, with no retries. A flaky test is a defect: it is quarantined by deleting
  it, not by rerunning it.

## Alternatives considered

- **Copy upstream's three-layer vitest shape with live e2e** — rejected: it needs Node, GitHub and a
  tolerant rate limit, and it skips Windows and every 32-bit/arm target, so it would leave the
  project's own differentiators untested while failing for reasons unrelated to the change.
- **Golden-only testing of the CLI** — rejected: golden files prove output bytes, not that a lock
  written here is readable upstream, nor that links are correct on a real filesystem. Compatibility
  fixtures and integration tests cover what goldens cannot.
- **Snapshot every command's full output and call it coverage** — rejected: it makes any message
  improvement a mass golden update and hides which behavior broke. Goldens are limited to output
  that is a contract: help text, lock bytes, path tables, TUI frames, plan JSON.
- **PTY-driven e2e with a terminal library (`creack/pty`)** — rejected: a third-party dependency, a
  second source of platform flakiness (Windows console APIs), for behavior a pure renderer test
  covers deterministically.
- **Coverage percentage gate (for example 80 %)** — rejected: it rewards testing trivial getters,
  says nothing about the hash or lock contracts, and invites tests written to raise the number.
- **Differential testing against the Node CLI in CI** — rejected: Node toolchain, npm, network,
  rate limits and moving tags in the merge path, for a parity surface that fixtures already pin.
- **Run tests only on Linux, cross-compile the rest** — rejected: the platform-specific code
  (junctions, reserved names, case-insensitive containment, `%APPDATA%`) is exactly what compile-time
  checks cannot verify.

## Consequences

- `go test ./...` is the whole suite and it works with the network down. The only external
  requirement is `git`.
- The compatibility fixtures become the project's most valuable test asset and the one that needs
  the most care: changing one requires a manifest update and a provenance note, which is deliberate
  friction.
- Windows and macOS runners carry real cost and real skips. The skip policy makes those skips
  visible in the spec rather than silently green.
- Coverage is a conversation in review, not a number in CI. The trade is that the spec/test link
  check must stay enforced, or the "every entry names a test" rule decays into decoration.
- The fake-tool binary in `internal/testutil` is test-only infrastructure that must never be
  imported by production code; `go list -deps ./cmd/skills` is checked in CI and must not contain
  `internal/testutil`.
- Adding a test dependency (diff library, pty, assertion library) now requires an ADR, because the
  strategy is built to need none.
