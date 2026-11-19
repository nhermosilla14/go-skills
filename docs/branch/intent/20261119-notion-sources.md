# Notion skill sources

- Status: Accepted
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

A skill page or pack published in Notion can be installed through the `ntn` CLI and updated later.

## Acceptance criteria

- `add https://www.notion.so/...` recognizes a Notion page or pack URL, and requires `ntn` on
  `PATH`; a missing `ntn` is a clear error with install guidance and exit 1.
- The page or pack is downloaded through `ntn` into a temporary directory, then treated like a
  download source: discovery, selection, install and hashing are unchanged.
- The lock records a Notion page by its page URL as `sourceType: "download"`, and a pack as
  `notion://packs/<id>` (because the download URL from `ntn` expires), with the skill path inside the
  download.
- `update` and `check` re-download through `ntn`; an expired or deleted page fails without corrupting
  the existing install.
- Credentials are `ntn`'s business: the tool neither reads nor stores them and never prints them.
- Notion installs never prompt for auth; failures explain which `ntn` command failed.

## Spec changes

- Adds: `sources` / Notion pages and packs
- Adds: `lockfile` / Notion download entries

## Out of scope

- Implementing the Notion API or OAuth in Go.
- Workspace search or listing.

## Open questions

- The exact `ntn` invocation and output shape follow upstream; confirm against the current `ntn`
  version at implementation time and record it in the ADR.
