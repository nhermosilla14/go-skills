# TUI and interactive prompt architecture

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `cli`

## Context

`skills add` is the only command whose decisions happen in a conversation: scope, skill group,
agents, install method, confirmation. Upstream builds that conversation out of five independent
prompt calls, each of which opens the terminal, reads until it has one answer, and closes it again.
Everything between prompts is ordinary streaming output, and the install itself interleaves prompt
answers with filesystem work. That shape does not survive a port, because this tool must also serve
three non-terminal audiences with one binary: scripts (`-y`, `--json`), commands whose product is
stdout (`use` prints a prompt and nothing else, `--list` prints a listing), and CI on three
operating systems where no test has a terminal.

`docs/adr/20261119-package-architecture-and-dependencies.md` fixes the library choice — Bubble Tea,
Bubbles and Lip Gloss v1.x — and the layering: `internal/cli` owns argument parsing, prompts, the
TUI and both renderers; `internal/app` orchestrates and owns no I/O primitive. This ADR fixes the
shape inside that boundary: one program or many, where an answer lives, what the terminal may do to
a render, which stream each producer owns, what cancel means at each phase, and where untrusted
strings are neutralized.

Constraints inherited from `docs/vision.md` and the interactive intent: the same decisions in the
same order as upstream, a keyboard-driven grouped multi-select with a detail pane, no hang when
there is no terminal, exactly one JSON document for `--json`, deterministic ordering, no ANSI
leakage into non-terminal output. Upstream facts quoted here come from
`olddocs/behavior/01-add-and-install.md` (§2.3 prompt matrix, §2.4 prompts, §2.5 cancel, §2.8 JSON
mechanics) and `olddocs/behavior/04-cli-commands.md` (§2 parser, §3 `list`, §7 `use`, §10 output
conventions); claims marked provisional are inferences from that prose, not observations.

## Decision

### One program per flow, with step sub-models

`add` runs **exactly one Bubble Tea program**, from before the source is fetched until the last
answer is given, and exits before the first byte is installed.

`internal/cli/tui` holds:

| Type | Role |
|---|---|
| `Session` | Implements the prompt interface. Owns the program, the terminal mode, and the context. Starts lazily, stops once. |
| `flow` | Root model. Holds the answers, the ordered `[]step` compiled from flags and facts, and the current step index. Advances only forward. |
| `fetch` | Step that runs acquisition as a `tea.Cmd` and reports progress and completion as messages. |
| `skillList` | Grouped multi-select with detail pane. Generic over `[]Item{ID, Title, Detail, Group}`; returns IDs. |
| `agentList` | Same component, different item set and a locked universal section. |
| `choice` | Single-select, used for scope and method. |
| `confirm` | Yes/no, default yes. |
| `theme` | Glyph set, styles, width-aware truncation. |

The root model owns no domain data of its own: it holds the `flowAnswers` that the use case passed
in and that the use case reads back. Steps never mutate each other and never fetch anything; the
fetch step exists only because acquisition takes long enough to need a spinner, and it runs as a
`tea.Cmd` so the program keeps redrawing and keeps listening for `ctrl+c`.

The sequence of steps is **compiled up front** by a pure function,
`PlanSteps(flags, facts) []StepID`, applying the documented prompt matrix. It is a unit-tested data
structure: `-g` removes the scope step, `-a`/`--all`/single-detected-agent removes the agent step,
`--copy` or a resolved copy mode removes the method step, `-y` empties the list, `-y` and every
complete flag combination never start a program. When the list is empty the session is never
constructed, so there is no code path where a prompt is skipped by accident and none where a prompt
appears that the matrix does not call for. There is no "back" navigation: upstream has none, `esc`
cancels the whole flow, and therefore no step needs to be re-entrant.

**`remove` reuses the selector, not the flow.** `remove`'s model is a two-step flow (`skillList`
over installed skills, then `confirm`) built from the same `flow` type with a different step list,
and the same item component with a different `Item` builder: `add` builds items from discovered
skills grouped by plugin name (falling back to `General`, per
`olddocs/behavior/07-skill-format-and-discovery.md` §8.4), `remove` builds them from installed
skills grouped by scope and annotated with the agents that link them. A `singleSelect` mode of the
same component serves `find`'s result list later. The component knows nothing about skills: it
takes IDs in, returns IDs out, which is what keeps one implementation honest across three commands.

### Prompts are an interface on the consumer side; answers are plain data

`internal/app` declares the interface it needs and the answer types it consumes:

```go
type Prompter interface {
    Answers(ctx context.Context, req Prompts) (Answers, error)
}

type Prompts struct { /* the compiled step list + facts to display */ }
type Answers struct { Scope Scope; Skills, Agents []string; Mode Mode; Proceed bool }
```

`internal/cli/tui.Session` implements it; `cmd/skills` wires the two. `internal/app` never imports
`internal/cli`, so the dependency graph stays acyclic and the use case can be exercised with no
terminal at all. `tui` imports `internal/app` for the answer types only.

Three implementations exist, and that is the whole non-interactive story:

1. `tui.Session` — real terminal, real keys.
2. `app.Scripted` — test double. It replays a queue of `Answers` and **fails the test** if the
   flow asks for a prompt that the queue does not contain. This is how every prompt-matrix row is
   tested without a TTY and without golden files.
3. `app.NoPrompts` — production guard. It returns `app.ErrPromptRequired` for every call and is
   wired whenever stdin is not a terminal, `-y`/`--all` are set, or `TERM=dumb`. Because the step
   planner should already have produced an empty list, reaching this implementation is a bug; the
   returned error becomes the documented non-TTY diagnostic and exit code 1 instead of a hang.

TTY detection happens once, in `cmd/skills`, via `golang.org/x/term`, and is a plain bool passed
downwards. Nothing below `cmd/skills` calls `os.Stdin` or asks whether it is a terminal.

### Testing without a terminal

Nothing in the test suite reads the real terminal, and no test depends on the machine's locale,
color support, or window size.

- **Model tests.** `Update` is a pure function of the model and a `tea.KeyMsg`; `View` is a pure
  function of the model. State-transition tests are table-driven: key sequence in, assertions on
  `flow` (current step, selected IDs, group expansion) out. Views are tested separately from
  transitions, so a layout change never breaks a key test and vice versa.
- **Golden renders.** `View` output is compared against `testdata/golden/<step>-<w>x<h>.golden` at
  three widths — 40 (narrow), 80 (normal), 120 (wide) — and at height 24, with color disabled and
  the ASCII glyph set, so the files are stable and reviewable. `-update` regenerates them; CRLF is
  normalized to LF before comparison so a Windows runner compares equal.
- **Program-level tests.** `tea.NewProgram(m, tea.WithInput(strings.NewReader(keys)),
  tea.WithOutput(&buf))` drives a real program over in-memory streams; the test asserts the final
  answers, the exit error, and that the last frame contains no unclosed escape. No pseudo-terminal
  is required, which is what makes the suite work on all three CI operating systems.
- **Flow tests.** `tui.Session` is driven through the same `WithInput`/`WithOutput` seams with
  scripted key sequences that cancel at each step, confirm, and toggle groups; these assert the
  exit code and that no install ran.
- **Binary test.** One test builds the binary and runs it with stdin attached to the null device
  (`NUL` on Windows) to prove the non-TTY refusal, its diagnostic and exit 1, and one `--json` run
  to prove the single-document contract end to end.
- **Environment pinning.** `TestMain` sets `NO_COLOR=1`, `TERM=dumb` and a UTF-8 locale, so a
  developer's terminal cannot change a golden file; CI runs the same `go test ./...` on
  `ubuntu-latest`, `macos-latest` and `windows-latest`.

### Terminal handling and degradation

- **Size.** The program takes its first `tea.WindowSizeMsg` as authoritative and never queries the
  terminal itself. Only if a zero size arrives (exotic input wrapper) does it fall back to 80×24,
  matching upstream's 80-column fallback.
- **Resize.** `tea.WindowSizeMsg` recomputes layout and re-truncates; no width is cached across
  frames, and the detail pane is replaced, never clipped by hand.
- **Minimum width.** 40 columns is the design minimum: below it the detail pane is dropped and each
  row keeps only the checkbox and a truncated title. Below 24 columns the group headings and
  descriptions are dropped as well. Rendering below the minimum is still correct, never panics, and
  never scrolls horizontally.
- **Truncation.** User text is cut at cell width with `…` (ASCII `...` in fallback mode) after
  sanitization and before styling, so widths are measured on the text that is actually printed.
  Descriptions are one line in the list and wrapped in the detail pane, capped at six lines.
- **Height.** The list viewport is `height - 4` rows (header, footer, two separators), minimum 3,
  with cursor-following scroll. No more than 20 rows are shown at once, as upstream.
- **Color.** Disabled when the human channel is not a terminal, when `NO_COLOR` is set to any
  non-empty value, or when `TERM=dumb`. `NO_COLOR` wins over everything. There is no `CI`-forces-
  color rule: upstream's (`olddocs/behavior/04-cli-commands.md` §10.1, observed) is a defect for
  piped output and is not reproduced.
- **`TERM=dumb`.** No TUI and no color. Prompts are refused with the non-TTY diagnostic, exit 1.
  Raw mode on a dumb terminal corrupts the session, and a spinner that cannot move the cursor is
  worse than the message.
- **Non-UTF-8 locale.** The glyph set is chosen once: UTF-8 when the first of `LC_ALL`, `LC_CTYPE`,
  `LANG` contains `UTF-8`/`utf8`; on Windows, when the console output code page is 65001. Otherwise
  ASCII (`[x]`, `>`, `-`, `+`, `v`). Upstream has no ASCII mode (`04-cli-commands.md` §10.2,
  observed); this is a deliberate deviation needed for legacy Windows consoles and must be added to
  `docs/adr/20261119-upstream-defect-corrections.md` when this slice lands.
- **Screen readers and non-color terminals.** Status is never conveyed by color alone: every row
  carries a word or a glyph (`installed`, `skipped`, `failed`, `[x]`/`[ ]`), and the focused row is
  marked with `>` plus bold. The program renders **inline**, never in the alternate screen, so the
  terminal's scrollback and a screen reader's buffer contain the last frame; it never sets the
  window title, never emits OSC 8 hyperlinks, and restores cursor visibility and terminal state on
  every exit path, including `ctrl+c`.

### Stream ownership and TUI lifetime

Two channels, one owner each, fixed per command invocation:

| Producer | Stream |
|---|---|
| TUI frames, spinners, progress lines, warnings, summary, human errors | **stderr** |
| `--json` document | **stdout**, always exactly one, always the same entry model as the human summary |
| `use` prompt | **stdout** |
| `list` listing, `--help`, `--version` | **stdout** |

This is stricter than upstream, which writes prompts and summaries to stdout
(`04-cli-commands.md` §10.4, observed). It is the only rule that makes `skills add … > out.txt`
safe, keeps `--json` a single-document channel, and protects `use`'s contract that stdout contains
the prompt and nothing else. It is recorded as a deviation in the defect-corrections ADR with this
slice.

Enforcement: `cmd/skills` creates one buffered writer per stream and passes it down; nothing outside
`internal/cli/output.go` may reference `os.Stdout` or `os.Stderr`, checked by a test that greps the
tree. The JSON document is written once, from a `defer` that runs on success and on every error
path, after full buffering, so no failure mode can emit half a document or code 0 with no document.

The TUI is **exited, never suspended**: the program stops after the confirmation answer and before
apply begins, so no renderer writes while a program holds the terminal. Acquisition is inside the
program, not before it, precisely so that the spinner cannot print into the JSON channel. Nested
programs are forbidden; the find-skills follow-up prompt (a recursive `add`) runs after this program
has exited.

### Cancellation and partial work

One sentinel, `app.ErrCancelled`, returned by the prompt interface; `cmd/skills` maps it to the
message and exit code.

| Phase | `ctrl+c` / `esc` / EOF | Printed | Exit |
|---|---|---|---|
| Before the program (planning, flag errors) | n/a | the normal diagnostic | 1 or 2 |
| During fetch | context cancelled; git child killed; temp tree removed; nothing written | `Installation cancelled` | 0 on a TTY, 1 otherwise |
| During selection or confirmation | program quits, terminal restored, nothing written | `Installation cancelled` | 0 on a TTY, 1 otherwise |
| During apply | stop starting new pairs, finish the pair in flight, record completed skills, no rollback | `Installation interrupted` plus the partial summary | 1 |
| Second `ctrl+c` during apply | immediate exit; lock writes are atomic, so the lock is either old or new | as above, truncated | 1 |

Inside the TUI, `ctrl+c` and `esc` are the same key (upstream treats both as cancel, observed) and
EOF on the input stream is treated as `ctrl+c`, never as "keep waiting". The TTY/non-TTY split for
the cancel code is upstream's (`01-add-and-install.md` §2.5): a deliberate cancel is success, a
non-terminal that needed a prompt is a failure. `add` never returns 130: signals are translated into
the documented 0/1/2 table, because vision goal 6 makes exit codes a contract. `use --agent`
continues to propagate the child's exit code; it has no TUI.

### Sanitization before rendering, in two places

Untrusted text reaches the UI from three directions: skill frontmatter, the search API, and the
locks. It is sanitized **at the entry boundary and again at the render boundary**, and never
between.

1. **Parse/discovery time** (`internal/skill`): `SanitizeDisplay` = strip OSC, DCS/PM/APC, CSI,
   simple escapes, C1 and remaining control characters (keeping `\t` and `\n`), then collapse line
   breaks and trim — upstream's `sanitizeMetadata` order, which must be preserved because OSC
   payloads contain `ESC [` (`07-skill-format-and-discovery.md` §4.3). Applied to `name`,
   `description`, `pluginName` and displayed `source` strings. `rawContent` is never touched, so
   hashes are unaffected.
2. **Render time** (`internal/cli/tui`, `internal/cli/render`): every untrusted string passes
   through `SanitizeDisplay` (or `SanitizeDisplayBlock` where wrapping is wanted) *before* it is
   measured or styled, because Lip Gloss measures cell width and an embedded escape both lies about
   width and injects control. Strings from the API and from lock files only get this second pass.

The renderer emits no ANSI of its own invention beyond the theme's SGR sequences, and applies no
style to text that has not been through the sanitizer. A golden test feeds a hostile skill
(`\x1b]0;PWNED\x07`, a `\x9b` CSI, a `\r`, a tab, a 400-byte description) through every step's
`View` and asserts that the only escapes present are the theme's own and that no line exceeds the
target width. Width, truncation and ordering are computed on sanitized text, which is why the two
passes cannot be merged into one.

### Determinism, flags and the JSON contract

- Skills are ordered by plugin group name then display name; agents follow registry order; removal
  candidates by skill name. Toggle order is discarded: upstream installs in the order items were
  toggled (`01-add-and-install.md` §2.4, observed, provisional for our output), which would make
  output and lock write order depend on keystrokes. Selection sets are the contract, order is ours.
- Unknown flags are rejected before any program starts, as decided in defect correction 7, so a
  typo can never open a screen that then waits for input.
- `--json` implies non-interactive, so the JSON renderer and the TUI can never coexist; a test
  constructs a session with `--json` and asserts that no program is started, which keeps the
  invariant enforced at build time rather than by a runtime check. Both renderers consume the same
  `[]Result`; the human summary is a rendering of the same statuses, so the two can never disagree
  about what happened.

## Alternatives considered

- **One short-lived program per prompt.** Rejected: each exit flushes and re-enters raw mode, so
  resizes and key repeat race with the transition; a `ctrl+c` between prompts has no owner that
  can cancel the fetch; the state has to be smuggled through six function returns; and the
  sequence stops being testable as one object. The single-program shape costs one `tea.Cmd` and one
  `step` list, and buys a single cancel path and a single place where "which prompts are needed"
  lives.
- **A separate flow model per command** (`addFlow`, `removeFlow`, `findFlow`). Rejected: three
  copies of step advancement and cancellation. One `flow` with a compiled step list and one item
  component shared by three commands keeps the differences where they belong — in the item builders
  and the step lists.
- **Prompts as callbacks into `internal/app`.** Rejected: it puts terminal concerns inside the use
  case, makes the non-interactive path a branch inside the flow instead of an absence of a
  prompter, and cannot express "this prompt should never have been asked" as a test failure.
- **A headless-tea fake terminal (`teatest`) for all UI tests.** Rejected for the model layer: it
  tests Bubble Tea's event loop more than our views, and it adds a module. Direct `Update`/`View`
  tests plus a few `WithInput` runs cover the same ground with less machinery.
- **Keeping human output on stdout (parity).** Rejected: it makes `--json` depend on redirection
  tricks, breaks `add | tee`, and puts interleaved text in the path of `use`'s contract.
- **A `--no-tui` flag for accessibility.** Not added: parity forbids new flags and the non-TTY,
  `TERM=dumb` and inline-rendering rules already give a usable path. Left as an open question
  rather than invented now.
- **Suspending the program (stop/start around long work).** Rejected: a suspended program has
  restored the terminal, so `ctrl+c` during the gap is not ours to interpret; and the JSON channel
  would then need the spinner anyway.

## Consequences

Interactive and scripted runs share one code path for every decision; the difference is which
`Prompter` is wired. The prompt matrix becomes a pure function with its own tests, so the
documented matrix is enforceable without a terminal. Golden files pin the three widths, which makes
layout regressions reviewable but requires regenerating them deliberately when the design changes.

Three deviations accumulate and must land in `docs/adr/20261119-upstream-defect-corrections.md`
with this slice: human output on stderr, ASCII fallback for non-UTF-8 terminals, and `CI` no longer
forcing color. Install order no longer follows toggle order. `remove` and `find` inherit a component
they did not have to write, at the cost of the item builder being the place where their differences
live. The TUI is inline and never full-screen, so it looks less like a modern TUI and more like a
CLI, which is the point.

Bubble Tea, Bubbles and Lip Gloss stay on major version 1 (`go.mod` pins the exact minors when the
first slice imports them); the v2 beta is not adopted. Their transitive `go-runewidth` is accepted
because cell-accurate truncation and this ADR's width guarantees depend on it.

## Open questions

- **`find`'s lifecycle.** This ADR fixes the selector component for `find` but not whether its
  interactive search runs in the shared `Session` or its own program; the later `find` intent
  decides, and it may want a search input that this component does not need.
- **An escape hatch for screen-reader users.** Whether to expose `--no-tui` (and with which name)
  is unresolved: parity says no new flags, accessibility says there should be one. Deferred until
  the inline rendering has been tried with an actual screen reader.
