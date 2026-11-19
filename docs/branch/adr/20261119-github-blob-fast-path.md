# GitHub blob fast path

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `sources`

## Context

The first branch deliberately dropped upstream's API-based snapshot path and required `git` for every
remote source. That left one real capability gap: installing from a public GitHub repository on a
machine without git. The clone path is now mature, so an API path can be added as an optimisation
with the clone as its safety net.

## Decision

- **When:** GitHub sources only, with no explicit ref, when the clone path is not already required
  (non-GitHub hosts, private repositories and pinned refs always clone).
- **How:** read the commit for the default branch, fetch the recursive tree, select the skill paths,
  fetch each blob, and verify the snapshot completeness against the tree entry SHAs. Any mismatch,
  rate limit or API error falls back to the clone path after one informational line.
- **Auth ladder:** anonymous, then `GITHUB_TOKEN`/`GH_TOKEN`, then `gh api`. Tokens are never printed,
  logged or written. A 401/403 without rate-limit headers falls straight back to clone.
- **Result parity:** installed files, hashes, discovery results and lock entries match the clone path,
  except the documented root-level exception: a root skill from the blob path installs only
  `SKILL.md`, while the clone path installs the whole directory.
- **No allowlist:** upstream restricts the path to a few owners; this implementation applies it to any
  public GitHub source and relies on the completeness check and fallback instead. That keeps behaviour
  predictable for users outside the allowlist.
- **No writes:** only read endpoints are used.

## Alternatives considered

- Keep clone-only — rejected: it is a real functional gap on Windows machines without git.
- Trust the tree without verifying blob SHAs — rejected: a truncated snapshot would install silently.
- Restrict to upstream's owner allowlist — rejected: arbitrary and invisible to users; the fallback
  already bounds the risk.

## Consequences

Public GitHub installs can avoid git entirely and are faster for small skills. The API rate limit
becomes a visible failure mode, handled by falling back rather than by failing the install. Tests use
an `httptest` server that mimics the tree and blob endpoints and never call GitHub.
