# Init

- Intents: 20261119-use-and-init
- ADRs: 20261119-use-prompt-and-launch

## Scope

`skills init [name]` writes the documented `SKILL.md` template in the current directory or a new
subdirectory, refuses to overwrite, rejects names escaping the working directory, and prints the
created path.

## End-to-end test

`TestE2EInit` covers the default template, a named subdirectory, an existing file, and traversal
names; asserts the frontmatter parses and the command exits 2 on rejection.

## Out of scope

- Interactively filling in the description.
