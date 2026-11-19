# skills.sh search client

- Status: Accepted
- Date: 2026-11-19
- Deciders: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.
- Spec area: `cli`, `network`

## Context

`find` is the only command whose purpose is reaching a third-party service (skills.sh). The API is
not a contract this project controls, it can rate-limit or change shape, and the tool has a standing
promise of no hidden network activity. `find` must therefore fail visibly and never affect local
state.

## Decision

- One HTTPS endpoint, called only by `find` and by the optional one-time find-skills offer:
  `GET https://skills.sh/api/search?q=<query>&limit=<n>` with an optional `owner` filter parameter;
  paginated owner searches merge pages up to the documented cap.
- A single client type with a 10-second timeout, a descriptive `User-Agent`, no credentials and no
  cookies or telemetry headers. No caching between runs.
- Response parsing is defensive: missing fields are skipped, malformed entries are ignored, and an
  unexpected document is reported as an API error (exit 1) rather than crashing.
- Results are ranked in the order the API returns them; the tool adds no scoring. Interactive mode
  reuses the TUI list component without the detail pane contract of `add`.
- Network and API failures print one clear line on stderr and exit 1; stdout stays empty. `find`
  never writes files except the one-time dismissal flag in the global lock.
- The one-time offer records `dismissed.findSkillsPrompt` and only runs when interactive, after a
  successful install, and when `find` support exists in the build. Declining prints the manual
  command.

## Alternatives considered

- Hard-code a mirror or scrape the website — rejected: fragile and outside the documented API.
- Cache results — rejected: the value of search is freshness and the volume does not justify it.
- Fail silently when the API is down — rejected: scripts need a non-zero exit.

## Consequences

`find` can break when skills.sh changes; the blast radius is one command with no local side effects.
Tests use an `httptest` server and never reach the network.
