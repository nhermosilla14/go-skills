# Notion integration through ntn

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `sources`, `lockfile`

## Context

Notion pages and packs are a documented source for skills, but the integration is implemented by the
external `ntn` CLI: it owns authentication, page access and the export format. Reimplementing the
Notion API in Go would duplicate a tool the user already needs to authorize.

## Decision

- **Detection:** a `notion.so`/`notion.site` page or pack URL selects the Notion path. `ntn` must be
  on `PATH`; otherwise the command fails with install guidance and exit 1 before any network call.
- **Fetch:** run `ntn` with the documented export invocation into a temporary directory, then treat
  the result exactly like a download source (discovery, selection, hashing, install).
- **Lock:** a page is recorded with its page URL as `sourceType: "download"`; a pack is recorded as
  `notion://packs/<id>` because the export URL from `ntn` expires. `skillPath` points inside the
  exported tree.
- **Re-fetch:** `update`, `check` and restore re-run the export. An expired, deleted or unauthorized
  page fails with the `ntn` error and leaves the installed copy intact.
- **Credentials:** owned by `ntn`. The tool never reads its config, never passes tokens and never
  prints its output beyond the error text.
- **No prompts:** authorization problems are reported, not retried interactively.

## Alternatives considered

- Implement the Notion API directly — rejected: OAuth, workspace permissions and export formatting are
  a large surface that `ntn` already solves.
- Store the exported archive URL — rejected: it expires, which is exactly why upstream records
  `notion://packs/<id>`.

## Consequences

Notion users get parity if they have `ntn`, and everyone else gets a clear, actionable error. The
integration is thin: one subprocess call, then the download pipeline.
