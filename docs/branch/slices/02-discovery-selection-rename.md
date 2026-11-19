# Discovery depth, selection and rename

- Intents: 20261119-cli-foundation-and-local-install
- ADRs: 20261119-upstream-defect-corrections, 20261119-install-plan-and-filesystem-model

## Scope

Discovery matches the documented container list and depth rules (root, `skills/`, `.curated`,
`.experimental`, `.system`, agent dirs, three-level category walk, shadowing, recursive fallback,
`--full-depth`), `skills.sh.json` `paths` are honoured, and plugin manifests contribute skills.
Selection works: `--skill`, `--skill '*'`, `--list`, `--copy`, `--rename`, `INSTALL_INTERNAL_SKILLS`,
and a duplicate name resolves deterministically by sorted order.

## End-to-end test

`TestE2EDiscoveryMatrix` installs from a fixture repository containing flat, categorized, nested,
shadowed, internal and plugin-manifest skills in one run, asserts the exact installed set and skip
warnings, then `TestE2ERename` installs one skill with `--rename` and asserts folder, lock key,
`skillName` and rewritten frontmatter name.

## Out of scope

- Remote sources, agent matrix, prompts, JSON.
