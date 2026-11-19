# `use` prompt and agent launch

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `cli`

## Context

`use` exists so one skill can be handed to an agent without installing anything. Its output is
consumed by pipes (`skills use … | claude`) and by direct launch, so stdout and argv are contracts,
not formatting choices. Upstream also leaves a temporary directory behind because the prompt points
the agent at supporting files after the process exits.

## Decision

- **Selection:** the source parser and discovery are shared with `add`. Exactly one skill must be
  selected by `@skill`, `--skill` (once), or being the only discovered skill; ambiguity or absence
  aborts before any download with exit 1 and a list of candidates.
- **Materialization:** create `skills-use-*` in the OS temp directory and copy the skill folder
  into it (sanitized directory name). Keep that directory after exit: the prompt references it.
  Clean only the intermediate clone/download workspace.
- **Prompt:** printed on stdout with no banner and no extra text, sections joined by blank lines and
  a single trailing newline:

  ```
  You are being given a Skill to execute for the user's next request.

  Use the following SKILL.md as your instructions:

  <SKILL.md>
  <verbatim contents>
  </SKILL.md>
  ```

  When the folder contains anything besides `SKILL.md`, a fourth section gives the absolute folder
  and tells the agent to resolve relative paths there. Errors go to stderr; stdout stays empty.
- **Launch:** `--agent` accepts exactly one of `claude-code` (`claude`), `codex` (`codex`) or
  `sarvam-code` (`sarvam-code`). The prompt is passed as one argv element, stdio is inherited, no
  shell is involved, and the child's exit code is propagated. A missing command yields a clear
  error and exit 1.
- **`init`:** writes the documented template to `./SKILL.md` or `./<name>/SKILL.md`, refuses to
  overwrite, and rejects names that escape the working directory (correction #6).

## Alternatives considered

- Write the prompt to stderr or add a decorative header — rejected: breaks the pipe contract.
- Delete the temp directory on exit — rejected: the agent reads supporting files after the command
  returns.
- Accept any agent id for `--agent` — rejected: only three commands are known to accept a prompt as
  an argument; guessing others produces surprising launches.

## Consequences

`use` is scriptable and its failures are visible. Temporary directories accumulate until the OS
cleans them, exactly as upstream; a future `--keep`/cleanup option would be a new decision.
