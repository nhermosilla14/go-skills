# Walking skeleton: CLI shell and local install

- Status: In progress
- Intents: 20261119-cli-foundation-and-local-install
- ADRs: 20261119-package-architecture-and-dependencies, 20261119-install-plan-and-filesystem-model, 20261119-lockfile-compatibility-and-write-policy, 20261119-test-strategy

## Scope

One local source becomes installed skills on disk and in a lock file, through the real binary:

`skills add ./fixtures/skills -a claude-code -y` discovers the skills, copies each into
`.agents/skills/<name>/`, links it into `.claude/skills/<name>`, writes `skills-lock.json` with a
v2 entry and the published content hash, and prints a summary. `--help`, `--version`, unknown
commands and unknown flags behave per the CLI contract.

This slice deliberately includes the two data contracts (content hash, project lock) because
nothing else can be tested without them, and the minimum agent knowledge (`claude-code` and the
shared `.agents/skills` directory) to produce a visible install.

## Layers touched

- Foundation: `go.mod`, `cmd/skills`, `internal/cli` (parser, help, exit codes, human renderer),
  `internal/fsutil` (atomic write, containment), `internal/hash`, `internal/lockfile`.
- Services: `internal/source` (local path only), `internal/skill` (parse root and `skills/<name>`,
  sanitize names), `internal/agents` (two entries: `opencode`, `claude-code`), `internal/install`
  (plan, copy, relative symlink), `internal/app` (add use case).
- Interface: non-interactive only; a missing `-y` in a non-TTY fails with `ErrPromptRequired`.

## End-to-end test

`e2e/add_local_test.go::TestE2EAddLocalInstall` builds the binary once in `TestMain`, creates a
temporary project with a fixture source (root skill, `skills/flat`, `skills/category/nested`, one
invalid `SKILL.md`), runs it with `-a claude-code -y`, and asserts:

- the four paths exist and the invalid skill is skipped with a warning on stderr;
- the symlink target is relative and resolves;
- `skills-lock.json` parses as v2 and its `computedHash` equals the published vector for the
  fixture;
- exit code 0, and stdout is empty (human output on stderr).

Unit tests in the same slice: `internal/hash` (three published vectors plus empty skill),
`internal/lockfile` (upstream fixture round-trip), `internal/cli` (flag parsing and error text),
`internal/install` (plan golden for the two agents).

## Out of scope

- Remote sources, archives and authentication (slice 05).
- Full discovery depths, plugin manifests, `skills.sh.json`, internal skills (slice 02).
- The other 77 agents, detection, junctions and copy-mode derivation (slice 03).
- Interactive prompts and `--json` (slice 06).
- Global scope and management commands (slices 07-10).
