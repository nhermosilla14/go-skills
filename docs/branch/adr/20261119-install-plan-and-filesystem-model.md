# Install plan and filesystem model

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `install`

## Context

Installing is not a single `cp`: several skills go to several agents, some agents share a canonical
directory, links may be relative or junctions or copies, existing directories may be ours or not,
and the lock must only record what actually landed. Upstream interleaves these decisions with
prompting and filesystem calls, which makes the rules hard to see and impossible to test in
isolation.

## Decision

**Resolve → discover → plan → apply → record.** Each stage is a separate function; the plan is a
plain data structure that tests can assert without touching a disk.

1. **Acquire** the source into a read-only tree (local path or temporary clone) and discover skills.
2. **Select** skills and agents from flags/prompts.
3. **Plan** one `InstallEntry` per skill: canonical location and mode (`symlink` or `copy`), then one
   `TargetEntry` per agent with its directory, link target or copy destination, and the skip reason
   when it does not apply.
4. **Apply** entries in a deterministic order (skill name, then registry order), collecting a
   `Result` per skill×agent (`installed`, `skipped`, `failed` with error). Nothing is rolled back;
   a failure is reported and leaves no partially written directory.
5. **Record** only successful skills in the lock, then print the summary.

**Mode derivation** mirrors upstream: copy if `--copy`, or if all selected agents resolve to at most
one distinct project skills directory, or if every target is Eve; otherwise symlink. Copy mode never
creates the canonical directory.

**Links.** POSIX: relative target computed from the realpath of the link's parent, so a moved project
still resolves. Windows: directory junction with an absolute target; if creation is refused, fall
back to a copy and report it. Before removing a destination, compare realpaths: identical paths and
source-inside-destination cases are skipped, never deleted.

**Overwrites.** Existing destinations are replaced without prompting (parity), after a containment
check that the destination is inside a directory this tool owns for that agent. A destination that
is a source directory containing the same skill (OpenClaw case) is skipped, not deleted. Category
directories (`.curated`, `.experimental`, `.system`) are never install targets.

**Apply safety.** Each copy is written into a sibling temporary directory and renamed into place, so
an interrupted install never leaves a half-written skill. Removal only deletes paths that the plan
created or that the lock tracks.

**Concurrency.** Source acquisition and parsing may run in parallel with a small bound; the apply and
lock-write phases are sequential. This keeps output ordering deterministic and avoids two writers
racing on the same lock file.

## Alternatives considered

- Copy-paste of upstream's flow — rejected: it couples prompting to I/O and cannot be unit-tested.
- Transactional install with rollback — rejected: invented behavior, and upstream's contract already
  tolerates partial results; the plan/apply split plus per-entry status is enough.
- Parallel apply — rejected: no measurable benefit at realistic skill counts, and it makes error
  output and the lock write order non-deterministic.

## Consequences

The plan can be golden-tested for every agent and mode, which is how the 79-agent registry stays
honest. Partial failures are visible and yield a non-zero exit. A crash mid-apply leaves either the
old or the new skill directory, never a mixture.
