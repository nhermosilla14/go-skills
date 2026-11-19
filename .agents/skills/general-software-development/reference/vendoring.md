# Vendoring the skill

Every case ends with the skill vendored in the repo. Copy this skill's whole directory (`SKILL.md`,
`cases/`, `reference/` and `templates/`) to `.agents/skills/general-software-development/`, tracked
in git (never in `.gitignore`).

**The vendored copy is the one to follow.** It pins the rules for everyone working on the repo. If
it differs from the skill you were started with, read the repo copy and work by it.

Compare `metadata.version` in the frontmatter of the repo copy and of the installed skill:
- No repo copy: create it.
- Same version and identical content: do nothing.
- Installed skill newer: follow the repo copy, tell the user and offer to upgrade. Upgrade only if
  the user agrees (see below).
- Repo copy newer: follow it and tell the user their installed skill is behind. Do not touch the
  installed skill.
- Same version but different content: warn that a copy was edited without bumping the version, and
  ask the user which one to keep.

## Upgrading a repo
There is no written migration chain. Compare what the repo has with what the new version expects
(folders, templates, `AGENTS.md` rules, merge gate), show the user the list of differences and
agree on each change before making it. In particular:
- Numbered intents from older versions: fold every `Done` one into spec entries (criteria and
  tests), show the result, and delete the intent files once the user confirms. Git keeps them.
- ADRs keep their existing file names.
- Never rewrite the content of a merged ADR to fit a new template.

## Maintaining the skill
- Versioning is semantic: major for changes that break existing repos (folder layout, record
  format, rule numbering), minor for new backward-compatible steps or rules, patch for wording
  fixes. Bump the version in every change to this skill.
- Copy nothing outside the skill's directory; keep absolute and user-specific paths out of it.
- Mention the vendored copy in the README's project structure and in the summary to the user.
