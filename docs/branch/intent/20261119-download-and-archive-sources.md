# Direct download and archive sources

- Status: In progress
- Date: 2026-11-19
- Owner: Nicolás Hermosilla P.
- Approved by: Nicolás Hermosilla P.

## Goal

A skill hosted as a raw `SKILL.md` or inside a `.zip`, `.tar`, `.tar.gz` or `.tgz` archive can be
installed, updated and restored by URL, with the same safety rules as upstream.

## Acceptance criteria

- `add https://host/path` installs when the URL serves a single valid `SKILL.md` or one of the
  supported archives; the extension is irrelevant, content is sniffed by magic bytes.
- Downloads are bounded: 10 MiB body, 25 MiB extracted, 1000 archive entries by default;
  `SKILLS_DOWNLOAD_MAX_BYTES`, `SKILLS_EXTRACT_MAX_BYTES` and `SKILLS_EXTRACT_MAX_FILES` can raise
  them, and a caller cannot lower the defaults.
- Archive extraction rejects `..`, absolute paths, drive letters, backslash traversal, links that
  escape the extraction root, and a single top-level directory is collapsed. Tar symlinks are
  skipped; zip symlinks are an error.
- The lock records `sourceType: "download"`, the URL as `source`/`sourceUrl`, and the skill's path
  inside the download; a URL with a query string or userinfo is not recorded (it may hold
  credentials) and the user is told to re-add it.
- `update` re-downloads when the content changed or the folder is missing; `check` reports an update
  when the re-downloaded content differs; `install` restores from the recorded URL.
- HTTP redirects are followed within the same limits, TLS verification is not disabled, and failures
  report the status code without printing credentials.

## Spec changes

- Adds: `sources` / direct downloads and archives
- Adds: `lockfile` / download entries

## Out of scope

- Notion (separate intent).
- Downloads that require browser auth or cookies.

## Open questions

- Whether to allow `file://` download URLs for tests; tests can use an `httptest` server instead.
