# go-skills

A fast, standalone Go reimplementation of the Node.js CLI [`vercel-labs/skills`](https://github.com/vercel-labs/skills)
v1.9.0, the command line tool for the agent-skills ecosystem: install, list, update and remove agent
skills. The goal is a single binary with no Node.js runtime, keeping data-format compatibility with
the upstream lock files and hash algorithm. See [`docs/vision.md`](docs/vision.md) for the goals and
scope behind the project.

## Status

Early development. There is no release yet, no Go code and no `go.mod`; the repository currently
holds the project scaffolding and the reference documentation under `olddocs/`. Nothing described
under Usage below works until the corresponding milestone lands, and each entry is marked `TODO`
until then.

## Prerequisites

- Go 1.27 or newer. The module targets the Go 1.27 toolchain.
- `git`, used to fetch sources and to run the repository scripts.
- No Node.js runtime. Installing the upstream CLI is not required or used.

`task` is optional: it runs the same commands documented in Development guidelines.

## Getting started

```sh
git clone https://github.com/nhermosilla14/go-skills.git
cd go-skills
task build            # or: go build -o skills .
./skills --help
```

`TODO`: the build commands fail until `go.mod` and `cmd/skills` exist. Until then, only the docs,
the scripts and the CI workflow are usable.

## Usage

The command surface is planned to match upstream v1.9.0, minus the npm library API and the
`node_modules`/workspace sync. Nothing below is implemented yet: every command is `TODO`, and
options, output and exit codes are not final until `docs/spec/` describes them.

| Command | Purpose | Status |
| --- | --- | --- |
| `add` | Install skills from a source into agent directories. | `TODO` |
| `list` | List installed skills and compare them with the project lock file. | `TODO` |
| `remove` | Remove installed skills and their lock entries. | `TODO` |
| `update` | Re-resolve tracked skills to newer revisions. | `TODO` |
| `check` | Report whether tracked skills are outdated without changing anything. | `TODO` |
| `use` | Generate a prompt for one skill on stdout, or launch an agent with it. | `TODO` |
| `find` | Search skill catalogs. | `TODO` |
| `init` | Create a new `SKILL.md` skeleton. | `TODO` |
| `install` | With a source, same as `add`; without one, restore from `skills-lock.json`. | `TODO` |
| `experimental_install`, `experimental_sync` | Upstream experimental commands. `experimental_install` restores from the lock file; `experimental_sync` (the `node_modules`/workspace sync) is out of scope and will stay as a clear "not supported" stub. | `TODO` |

There is no telemetry and there will not be any.

Data compatibility commitments, once implemented: `skills-lock.json`, the global lock file and the
content hash algorithm must keep validating existing upstream lock files. Where upstream
documentation and upstream code disagree, the documented behavior wins.

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
├── cmd/
│   └── skills/                `package main`: flag parsing, dispatch, wiring (TODO)
├── internal/                  Implementation packages, not importable from outside the module (TODO)
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
│   └── check-branch-docs.sh   Merge gate for unfinished branch records
├── .agents/
│   └── skills/                Vendored `general-software-development` skill (the workflow rules)
│       └── general-software-development/
└── .github/
    └── workflows/
        └── merge-gate.yml     Pull-request checks for `main`
```

Directories listed with `(TODO)` do not exist yet.

## Development guidelines

The commands that must pass before a change is considered done:

```sh
go build ./...
go test ./...
go vet ./...
gofmt -l .        # must print nothing; `go tool gofmt -l .` if gofmt is not on PATH
```

`golangci-lint` v2 is the linter. The Taskfile wraps the common cases: `task build`, `task test`,
`task lint`, `task release:snapshot`.

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
