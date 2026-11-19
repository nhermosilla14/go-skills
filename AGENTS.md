# AGENTS.md

## Project overview
See `README.md` for overview, structure, usage and all commands, and `docs/vision.md` for goals.

`go-skills` is the Go module `github.com/nhermosilla14/go-skills` (Go 1.27 toolchain) and builds the
binary `skills`: a fast, standalone reimplementation of `vercel-labs/skills` v1.9.0 with no Node.js
runtime.

## Rules (mandatory, for humans and agents)
The skill vendored at `.agents/skills/general-software-development/` holds the full rules. This
list is the entry point, not a second copy.

1. **Use the `general-software-development` skill.** All work on this project (features, fixes,
   refactors, docs, config) MUST load and follow it first, from the vendored copy: that is the
   version this project follows. Do not edit that copy by hand; it changes only through the skill's
   vendoring step. A subagent working from an orchestrator's brief follows the brief and does not
   start the workflow itself.
2. **Size the change first.** Propose the tier (trivial or full loop, as the skill defines them)
   with the reason, and wait for confirmation. Trivial changes need no records.
3. **Follow the loop.** Requirements (intents) → design (ADRs) → vertical slices → code and test one
   slice at a time, each stage approved by the user before the next. Working records live in
   `docs/branch/`, which must be empty to merge. Any decision affecting architecture, dependencies,
   interfaces, data formats, security, deployment or scope needs an ADR first.
4. **Keep docs current.** `docs/spec/` describes what the system does today, with the tests that
   prove it; `docs/architecture.md` how it is built; `docs/adr/` why. Update them, `docs/vision.md`,
   `docs/README.md` and `README.md` whenever what they describe changes, and check them before
   finishing. Documented commands must work.
5. **Ask, don't invent.** If goals are unclear, ask the user and record the answer in `docs/`, using
   specialized discovery or interview skills when available. Ask also before resolving a conflict
   between decisions.
6. **Orchestrate full-loop work.** The agent talking to the user plans, delegates, reviews and
   reports; the work goes to subagents, specialized over general, as the skill describes.
7. **Tooling and workflow live in Conventions**, not ADRs. Edit them in place.
8. **Update this file** when rules or conventions change.

## Reference material
`olddocs/` documents the legacy solution this project reimplements: its behavior and technical
analysis. It is read-only background for requirements and design, not a source of decisions: the
decisions live in `docs/`. It is not frozen and may be corrected when something in it proves wrong.
Do not copy old code or old technical choices from it without an ADR. The legacy solution is
`vercel-labs/skills` v1.9.0, replaced to get a single fast binary with no Node.js runtime and full
cross-platform coverage (Windows x86/x64, Linux x86/x64/arm64/armhf, macOS Intel/Apple Silicon).

## Conventions
- Tooling: `gofmt` (`go tool gofmt` when it is not on PATH) for formatting, `go vet` and
  `golangci-lint` v2 for static checks, `go test ./...` for tests, Taskfile for build/test/lint
  (`task build`, `task test`, `task lint`, `task release:snapshot`), GoReleaser for release
  artifacts and GitHub Actions for CI (`.github/workflows/merge-gate.yml`).
- Git workflow: trunk-based with short-lived branches. Integration branch: `main`.
- Workflow: branches are named `type/short-description` (for example `feat/add-command`,
  `fix/lock-hash`). Commits follow Conventional Commits and carry no author names or emails in their
  message. No author emails or names here; each contributor commits with their own git identity.
  Names appear only in `Owner`/`Deciders` and `Approved by` of intents and ADRs. Releases are cut
  from tags by GoReleaser for the eight supported platform pairs.
- Style: idiomatic Go, standard library first, minimal dependencies, table-driven tests, and code
  that passes `gofmt` and `goimports`.
- Never commit secrets; keep them out of git via `.gitignore` and provide `.env.example`.
