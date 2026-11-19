# Upstream defect corrections

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: affects `cli`, `install`, `lockfile`, `discovery`

## Context

Applying `docs/adr/20261119-upstream-parity-policy.md` to the behavior documented in `olddocs/`
produces a list of places where this implementation intentionally does not reproduce upstream
v1.9.0. They are collected here so no deviation is accidental and so tests can assert the corrected
behaviour. Evidence pointers are in the corresponding `olddocs/` files.

## Decision

Correct these behaviours. Everything not listed keeps upstream semantics.

| # | Where | Upstream | This implementation | Why |
|---|---|---|---|---|
| 1 | `remove --agent '*'` | Errors `Invalid agents: *` although docs and help say `*` means all | Accepts `*` as all agents | Documented behavior and the flag's own help |
| 2 | `remove` with no match | Prints an error, exits 0 | Exits non-zero when nothing matched | Scripts must detect failure |
| 3 | `remove` failures | Individual failures do not affect the exit code | Any failed removal exits non-zero | Same |
| 4 | `list` vs `check` | Hash-less lock entry is `tracked` in `list`, `modified` in `check` | Both report `modified` (unverifiable = changed) | One contract, one status |
| 5 | `update` | A globally unreachable source does not increment the failure count, so exit is 0 | Counts as a failure, exits non-zero | Same |
| 6 | `init <name>` | Writes outside the working directory for `../x`, accepts separators | Rejects names that escape the working directory | Safety |
| 7 | Unknown flags | Silently ignored by every command except `use` | Rejected with a clear error and usage hint | Typo safety; flags must not be no-ops |
| 8 | Extra positionals | `add a b` silently ignores `b` | Rejected: more than one source is an error | Same |
| 9 | `--json` error paths | `list` can print non-JSON for an invalid `--agent`; `add` prints `[]` after a human error | Always exactly one JSON document on stdout, errors included | Machine contract |
| 10 | Duplicate discovery | `readdir` order decides which same-named skill wins; no sorting | Directory entries sorted, so the first in sort order wins | Determinism across filesystems |
| 11 | `list` ordering | Filesystem order | Sorted, stable order (agents, then skill name) | Same |
| 12 | Partial install failure | Human mode exits 0 when some skill×agent installs fail | Exits non-zero if anything failed | Same as 2 |
| 13 | `use --help` | Prints top-level help | Prints `use`'s own help | Consistency |
| 14 | Global lock write | Non-atomic `writeFile`, unlocked; torn write silently yields an empty lock | Atomic replace under the same lock discipline as the project lock; corruption is reported | Data loss |
| 15 | Windows containment | Case-sensitive prefix checks and no reserved-name handling | Case-insensitive containment, reserved device names rejected | Correctness on the target platform |
| 16 | `experimental_install` | Drops `via` when rewriting an entry | (Moot: sync entries are not created; restore preserves unknown fields) | Forward compatibility |
| 17 | `add --json --metadata` | Telemetry-only flag | Flag removed | No telemetry |
| 18 | Output streams | Human output and progress share stdout with product output | Human output and progress go to stderr; stdout carries only the JSON document, the `use` prompt, listings and help | `--json` and `use` must be pipeable |
| 19 | Non-UTF-8 terminals | Unicode glyphs are always emitted | ASCII glyph fallback when the locale is not UTF-8 | Legacy Windows consoles |
| 20 | `CI` environment | Forces color on | Color follows TTY and `NO_COLOR` even in CI | Ansible-style logs and log files should stay clean |
| 21 | Install order | Follows toggle order in the selector | Skill order, then registry order | Deterministic output and lock write order |

Not corrected, kept for compatibility, and therefore documented in the spec: OpenClaw's legacy
global directories, hidden universal agents, `install`/`i` meaning restore without a source,
`--full-depth` semantics, Eve's lossy frontmatter rewrite and flat-file layout, and `skills use`
leaking its temp directory.

## Alternatives considered

- Fix everything that looks wrong, including layout and prompt flow — rejected: those are tier-1
  contracts or subjective UX; the parity policy limits corrections to defects.
- Fix nothing — rejected in the parity policy ADR.

## Consequences

The deviations are visible in the spec ("Not supported" or explicit notes) and covered by tests.
A user migrating from upstream sees different exit codes and clearer errors, but identical files,
locks and agent layouts.
