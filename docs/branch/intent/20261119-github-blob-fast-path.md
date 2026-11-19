# GitHub blob fast path

- Status: Accepted
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

Installing from a public GitHub repository works without `git` installed, using the GitHub API the
way upstream does, and falls back to a clone when the API cannot provide a complete tree.

## Acceptance criteria

- For a GitHub source with no explicit ref, the tool first reads the repository tree and file blobs
  through the API, verifies the snapshot is complete against the tree entry SHAs, and installs from
  it; any failure falls back to the existing clone path with a single informational line.
- An explicit ref, a non-GitHub host, or a source that is not a public repository continues to use
  the clone path; private repositories keep the existing authenticated clone.
- Authentication for the API follows the documented ladder: anonymous first, then
  `GITHUB_TOKEN`/`GH_TOKEN`, then `gh api`; a token is never printed or written.
- Installed files, hashes, lock entries and discovery results are identical to the clone path for a
  well-formed repository, including subpaths and renamed skills.
- A root-level skill installed through the blob path carries only `SKILL.md`, matching upstream's
  documented exception; all other skills carry their full folder.
- Requests are rate-limit aware: a 403 with rate-limit headers retries through the next
  authentication step, and other errors fall back to the clone.
- No writes to the API: the tool only reads.

## Spec changes

- Changes: `sources` / GitHub acquisition
- Changes: `install` / root-level skill payload

## Out of scope

- GitLab or other hosts' APIs.
- Creating issues, pull requests or any write API.
- The owner allowlist upstream uses; this implementation applies the path generally and falls back
  on failure.

## Open questions

- Whether a later change should restrict the API path to upstream's allowlist for predictability; the
  tradeoff is fewer API calls versus fewer surprises. Decided in the ADR.
