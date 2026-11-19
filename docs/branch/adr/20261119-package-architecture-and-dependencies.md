# Package architecture and dependencies

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `cli`, `sources`, `install`, `lockfile`

## Context

The tool has to reproduce a large CLI surface, two data contracts and eight platform targets, while
staying small enough to audit. Upstream is a 28k-line TypeScript program with runtime dependencies on
an interactive-prompt library, a YAML parser, a tar library, a Git wrapper and an XDG helper. Most of
that does not survive the move to Go unchanged.

## Decision

**Layering.** `cmd/skills` parses `os.Args`, builds a context, dispatches to `internal/app` use cases
and maps errors to exit codes. `internal/app` orchestrates; it owns no I/O primitives. Leaf packages
own one contract each:

| Package | Owns |
|---|---|
| `internal/cli` | argument model and parser, help, prompts/TUI, renderers (human and JSON), exit codes |
| `internal/source` | source grammar, refs, acquisition (git, HTTP, archives), auth ladder |
| `internal/skill` | `SKILL.md` parse/rewrite, discovery, sanitization, selection, rename |
| `internal/agents` | registry, OS path resolution, detection |
| `internal/install` | plan/apply of files, links, copies; agent quirks |
| `internal/lockfile` | both lock schemas, status comparison, atomic persistence |
| `internal/hash` | content hash and Git tree hash contracts |
| `internal/fsutil` | atomic replace, file locking, containment, junctions |

**Argument parsing is hand-written** in `internal/cli`, not a CLI framework. The required semantics
(greedy list options, `--flag=value`, `--`, repeatable flags, exact error text, unknown-flag
rejection) are specific enough that adapting a framework costs more than the ~300 lines involved,
and every dependency added here is a compatibility and supply-chain cost.

**Testability boundaries.** Execution of `git`/`gh`, HTTP, the clock and the terminal are interfaces
with production and fake implementations. Filesystem operations are real and use temporary
directories; a virtual filesystem would hide the platform differences this tool exists to handle.

**Dependencies** (pinned in `go.mod`, nothing else without an ADR):

| Module | Why | Alternative rejected |
|---|---|---|
| `go.yaml.in/yaml/v3` | frontmatter parsing with YAML anchors/scalars; maintained successor of `gopkg.in/yaml.v3` | writing a YAML parser, `goccy/go-yaml` (larger API than needed) |
| `github.com/charmbracelet/bubbletea` + `bubbles` + `lipgloss` | grouped multi-select with detail pane, key handling, width-aware rendering | `survey`, `promptui` (no grouped list/detail pane), hand-rolled raw mode (not worth it) |
| `golang.org/x/term` | TTY detection and terminal width | `syscall` per OS |
| `golang.org/x/sys` | Windows junction creation and file-sharing flags | `os/exec` calling `mklink` (locale/privilege dependent) |

Everything else is standard library: `net/http`, `archive/tar`, `archive/zip`, `compress/gzip`,
`crypto/sha256`, `encoding/json`, `os/exec`.

**Error model.** Rooted sentinel errors per package (`fsutil.ErrOutsideRoot`, `source.ErrAuth`,
`install.ErrCategoryDir`, …) wrapped with `%w`; the CLI maps categories to exit codes (0 success,
1 failure, 2 usage or unsupported) and renders the message. No panics for user-facing conditions.

## Alternatives considered

- Mirror upstream's module layout file by file — rejected: it would carry Node-specific seams
  (prompts, telemetry, blob API) into the port.
- A single `internal/skills` package — rejected: 10k lines in one package with no boundaries makes
  the parity work untestable and the review surface enormous.
- Cobra/pflag — rejected: it does not support list flags and exact error semantics without
  workarounds, and adds a dependency tree.

## Consequences

More code in the CLI layer than a framework would need, in exchange for exact semantics and no
framework lock-in. Dependency updates are rare and each is isolated behind one package, so replacing
the YAML or TUI library is a local change.
