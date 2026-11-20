# Notion sources

- Status: Done

- Intents: 20261119-notion-sources
- ADRs: 20261119-notion-through-ntn

## Scope

Notion page and pack URLs through `ntn`: missing-tool error, export into a temporary directory,
download-style install and lock entries (page URL, `notion://packs/<id>`), and re-export for update,
check and restore.

## End-to-end test

`TestE2ENotionPage`, `TestE2ENotionPack`, `TestE2ENotionMissingTool`, `TestE2ENotionUpdate` with a
fake `ntn` executable on `PATH`: install, lock shape, re-export after a change, missing tool exit 1,
expired page leaves the install intact.

## Out of scope

- OAuth, workspace search, and the Notion API itself.
