# go-skills

A fast, standalone Go reimplementation of the Node.js CLI [`vercel-labs/skills`](https://github.com/vercel-labs/skills)
v1.9.0, the command line tool for the agent-skills ecosystem: install, list, update and remove agent
skills. The goal is a single binary with no Node.js runtime, keeping data-format compatibility with
the upstream lock files and hash algorithm. See [`docs/vision.md`](docs/vision.md) for the goals and
scope behind the project.

## Status

Early development. There is no tagged release yet. `add` works for local directories and all 79
agents, writes a v2 `skills-lock.json`, and implements the content-hash contract; the release
pipeline builds every supported platform. Everything else is marked `TODO` and arrives slice by
slice; see `docs/branch/slices/` for the plan and `docs/spec/` for what is delivered.

## Prerequisites

- Go 1.27 or newer. The module targets the Go 1.27 toolchain.
- `git`, used to fetch sources and to run the repository scripts.
- No Node.js runtime. Installing the upstream CLI is not required or used.

`task` (go-task v3) is optional; it runs the same commands documented in Development guidelines.

## Getting started

```sh
git clone https://github.com/nhermosilla14/go-skills.git
cd go-skills
task build            # or: go build -trimpath -o skills ./cmd/skills
./skills --help
```

Releases are published as archives for every target pair listed below; until the first tag, build
from source. To reproduce a release locally:

```sh
task release:snapshot   # goreleaser release --snapshot --clean, artifacts in dist/
```

Version metadata is injected at build time; a plain `go build` reports development defaults:

```sh
go build -ldflags "-X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o skills ./cmd/skills
```

## Usage

The command surface is planned to match upstream v1.9.0, minus the npm library API and the
`node_modules`/workspace sync. Options, output and exit codes are not final until `docs/spec/`
describes them; commands marked `TODO` exit with a usage error.

Current, working subset:

```sh
./skills add ./path/to/skills -a opencode -a claude-code -y
./skills add vercel-labs/agent-skills --list
./skills add vercel-labs/agent-skills@v1.2.3 --skill web-design -a opencode -y
./skills add https://gitlab.com/group/repo -a claude-code -y

# Interactive: choose skills, agents, scope and install method with the keyboard.
./skills add vercel-labs/agent-skills

# Machine-readable: one JSON array on stdout, human output on stderr.
./skills add vercel-labs/agent-skills -a opencode -y --json

# Inspect what is installed, and check for drift or updates.
./skills list
./skills list --json
./skills check
./skills remove web-design -a claude-code
./skills rm --all
./skills update
./skills upgrade web-design

# Global scope: install once for every project.
./skills add vercel-labs/agent-skills -g -a opencode -y
./skills list -g
./skills remove -g --all

# Rebuild a clean checkout from the lock.
./skills install

# Hand one skill to an agent without installing it.
./skills use vercel-labs/agent-skills@web-design-guidelines | claude

# Restore exactly what the lock records (frozen by default in CI).
CI=1 skills install

# Search the directory, or scaffold a new skill.
./skills find react --owner vercel
./skills init my-skill
./skills add ./path/to/skills --list
./skills add ./path/to/skills --skill "Web Design" --rename web-design -y
./skills add ./path/to/skills --all
```

All 79 upstream agent ids are available with `-a <id>` (or `-a '*'`); each resolves to that
agent's project directory, and agents that share `.agents/skills` are installed once. Detection is
directory-based, so `-y` without `-a` installs to the agents it finds.

`add` discovers skills at the source root, in `skills/` (including `.curated`, `.experimental`,
`.system`), in agent skill directories, in containers declared by `skills.sh.json`, and in Claude
plugin manifests, down to three directory levels; a valid root `SKILL.md` makes the repository a
single skill, and `--full-depth` searches the whole tree to five levels.

| Command | Purpose | Status |
| --- | --- | --- |
| `add` | Install skills for any of the 79 agents from a local path or git source, interactively or non-interactively, with `--json` output. | Partial (git sources) |
| `list` | List installed skills and compare them with the lock file. | Delivered |
| `remove` | Remove installed skills and their lock entries, keeping anything another agent still uses. | Delivered |
| `update` | Re-resolve tracked skills to newer revisions, preserving mode, rename and Eve placement. | Delivered |
| `check` | Report whether tracked skills are outdated without changing anything. | Delivered |
| `use` | Generate a prompt for one skill on stdout, or launch an agent with it. | Delivered |
| `find` | Search the skills.sh directory by keyword or interactively. | Delivered |
| `init` | Create a new `SKILL.md` skeleton. | Delivered |
| `install` | With a source, same as `add`; without one, restore project skills from `skills-lock.json` at their pinned commits. | Project restore |
| `experimental_install` | Alias of restore from the lock file. | Project restore |
| `experimental_sync`, `sync` | Explicit not-supported stub: npm/`node_modules` integration is excluded, so it exits 2 and points at `add` for git or local sources. | Not supported |

Interactive runs use a keyboard-driven flow: grouped skill selection with descriptions and a
detail pane, agent selection, install method and confirmation. `--json` requires `-y` (or `--all`)
and writes exactly one JSON array to stdout; every human message goes to stderr. Cancelling
installs nothing.

There is no telemetry and there will not be any.

Data compatibility: `skills-lock.json` (v2, with v1 migration), the global
`$XDG_STATE_HOME/skills/.skill-lock.json` (v3) and the content hash algorithm keep validating
existing upstream lock files. Set `GITHUB_TOKEN` or `GH_TOKEN` to raise GitHub API limits when the
global lock records folder tree hashes; `SKILLS_API_URL` overrides the search endpoint;
`--frozen-lockfile` is implied by the usual CI variables (`CI`, `GITHUB_ACTIONS`, `GITLAB_CI`,
`BUILDKITE`, `CIRCLECI`, `TRAVIS`, `TF_BUILD`, `JENKINS_URL`). Where upstream documentation and upstream code disagree,
the documented behavior wins.

## Supported platforms

Release binaries are built for eight target pairs:

| OS | `GOARCH` | Notes |
| --- | --- | --- |
| Windows | `386` | |
| Windows | `amd64` | |
| Linux | `386` | |
| Linux | `amd64` | |
| Linux | `arm64` | |
| Linux | `arm` | `GOARM=6` |
| macOS | `amd64` | Intel |
| macOS | `arm64` | Apple Silicon |

Cross-platform behavior (path resolution, symlinks, junctions) is a stated requirement, so these are
the targets CI and release builds must cover.

## Project structure

```text
.
├── AGENTS.md                  Rules and conventions for humans and agents
├── README.md                  This file
├── Taskfile.yml               Build, test, lint and release tasks
├── .goreleaser.yaml           Release builds, archives and checksums
├── .golangci.yml              Linter configuration
├── cmd/
│   └── skills/                `package main`: dispatch, exit codes, build metadata
├── internal/                  Implementation packages, not importable from outside the module
├── docs/                      Decisions and current behavior: vision, spec, ADRs, architecture
│   ├── README.md              Documentation index
│   ├── vision.md              Goals, scope and non-goals
│   ├── architecture.md        How the binary is built
│   ├── spec/                  What the system does today, with its tests
│   ├── adr/                   Why it is built that way
│   └── branch/                Working records for the current branch; empty to merge
├── olddocs/                   Reference documentation for the upstream implementation
│   ├── behavior/              Upstream behavior, command by command
│   ├── technical/             Upstream architecture, dependencies and porting risks
│   └── README.md              Index and headline findings
├── scripts/
│   ├── check-branch-docs.sh   Merge gate for unfinished branch records
│   └── smoke.sh               Version, help and install smoke test for a built binary
├── .agents/
│   └── skills/                Vendored `general-software-development` skill (the workflow rules)
│       └── general-software-development/
└── .github/
    └── workflows/
        ├── merge-gate.yml     Pull-request checks: docs gate, tests, lint, cross-compile
        └── release.yml        Tag-driven release published by GoReleaser
```

Directories listed with `(TODO)` in the usage table do not exist yet; everything else is present.

## Development guidelines

The commands that must pass before a change is considered done:

```sh
go build ./...
go test ./...
go vet ./...
gofmt -l .        # must print nothing; `go tool gofmt -l .` if gofmt is not on PATH
```

Network tests are opt-in: `SKILLS_E2E_NETWORK=1 go test ./e2e`. Everything else runs offline.

`golangci-lint` v2 is the linter. The Taskfile wraps the common cases: `task build`, `task test`,
`task lint`, `task fmt`, `task vet`, `task cross`, `task smoke`, `task release:snapshot`,
`task clean`.

Release process: push a `v*` tag. `release.yml` re-runs the tests and publishes archives plus
`checksums.txt` through GoReleaser. Signing and notarization are not set up; see the ADR.

Work follows the loop Requirements → Design → Slices → Code → Test, one vertical slice at a time,
with each stage reviewed before the next. The rules are summarized in [`AGENTS.md`](AGENTS.md) and
the full process, including record templates, lives in the vendored skill at
`.agents/skills/general-software-development/` (`templates/` holds the intent, ADR, slice and spec
templates).

Git workflow is trunk-based with short-lived branches; `main` is the integration branch. Commits
follow Conventional Commits.

## Documentation

- [`docs/README.md`](docs/README.md) — index of the project documentation.
- [`docs/vision.md`](docs/vision.md) — goals, scope and non-goals.
- [`docs/architecture.md`](docs/architecture.md) — how the binary is built.
- [`docs/adr/`](docs/adr/) — accepted architecture decisions.
- [`olddocs/README.md`](olddocs/README.md) — reference documentation for upstream
  `vercel-labs/skills` v1.9.0: behavior and technical analysis. It is read-only background for
  requirements and design, not a decision record.

## License

MIT.
