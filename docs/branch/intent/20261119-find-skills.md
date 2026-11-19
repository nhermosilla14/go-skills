# Search the skills directory

- Status: Accepted
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

A user can discover new skills by keyword or interactively, and can search everything published by
one owner, without leaving the terminal.

## Acceptance criteria

- `skills find` opens an interactive search; typing filters results; selecting a result prints the
  install command for it and exits 0; `esc`/`ctrl+c` exits without output.
- `skills find <query>` searches by keyword, prints ranked results (name, source, install count if
  provided) and exits 0; no results prints a short message and exits 0.
- `--owner <login>` restricts the search to one GitHub owner/user and merges paginated results.
- The client calls the skills.sh search endpoint over HTTPS with a timeout, sends no credentials or
  personal data, caps results as documented, and reports network/API failures with exit 1.
- `find` aliases `f`, `s` and `search` work; the command is in help.
- After the first successful `add`, when interactive, the tool offers to install
  `vercel-labs/skills@find-skills` once, records the dismissal in the global lock, and never asks
  again; declining prints how to install it later.

## Spec changes

- Adds: `cli` / `find` search and interactive selection
- Adds: `cli` / one-time find-skills offer
- Adds: `network` / skills.sh search client

## Out of scope

- Offline caching of search results.
- Search over installed skills (`list` covers that).

## Open questions

- The skills.sh API is not a contract we control; failures must stay non-destructive. Decided in the
  design ADR.
