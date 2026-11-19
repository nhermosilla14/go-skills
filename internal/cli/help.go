package cli

import (
	"fmt"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/agents"
)

// Help returns the usage text. It lists only what the binary does today.
func Help() string {
	return fmt.Sprintf(`Usage: skills <command> [options]

Commands:
  add <source>      Install the skills found in a source
  install [source]  With a source, same as add; without one, restore from
                    skills-lock.json (alias: i)
  use <source>[@skill]
                    Use one skill without installing it
  experimental_install
                    Restore skills from skills-lock.json
  list, ls          List installed skills and their lock status
  check [skill...]  Report available updates and local changes, without writing
  update, upgrade   Update installed skills to their latest content
  remove, rm, r     Remove installed skills
  help              Show this help
  version           Show version, commit and build date

Sources:
  owner/repo                    GitHub repository
  owner/repo/subpath            only the skills under a subdirectory
  owner/repo@v1.2.3             pin a branch, tag or 40-character commit
  owner/repo@skill              select one skill by name
  owner/repo#ref[@skill]        ref, optionally with a skill
  github:owner/repo             explicit GitHub prefix
  gitlab:group/repo             explicit GitLab prefix
  https://github.com/o/r/tree/main/skills/x
  https://gitlab.com/group/repo/-/tree/main/src
  https://dev.azure.com/org/project/_git/repo?path=/skills/x&version=GBmain
  git@github.com:owner/repo.git       SSH
  https://host/owner/repo.git         any git host
  ./dir, /dir or ~/dir                local directory

Git sources require git on PATH. Private GitHub repositories use your git
credentials first, then 'gh repo clone', then SSH; no token is read by this tool.

Add options:
  -a, --agent <agent...>   Agents to install for (default: all). Use '*' for all.
                           Known agents: %s
  -s, --skill <name...>    Skills to install (default: all). Use '*' for all.
  -y, --yes                Install without prompting
  -g, --global             Install into the user home instead of the project
      --all                Every skill for every agent, without prompting
      --copy               Copy skills instead of linking them
      --json               Print one JSON array on stdout; requires -y or --all
      --subagent <name...> Eve subagent targets; accepted, subagents install later
      --rename <name>      Install one selected skill under a new name
      --full-depth         Search all subdirectories, even under a root SKILL.md
  -l, --list               List the skills in the source and exit
  -h, --help               Show this help

List options:
  -a, --agent <agent...>   Only these agents (default: all). Use '*' for all.
  -g, --global             List global skills and the global lock
      --json               Print one JSON array on stdout
  -h, --help               Show this help

Use options:
  -s, --skill <name>       Select the skill to use; only one value is allowed
  -a, --agent <agent>      Launch the agent with the prompt: claude-code, codex
                           or sarvam-code
      --full-depth         Search all subdirectories, even under a root SKILL.md
  -h, --help               Show this help

Restore options:
  install                  Restore every skill recorded in skills-lock.json
  experimental_install     The same restore, always
  -a, --agent <agent...>   Agents to restore links for (default: the last selection,
                           then the detected agents, then the canonical directory only).
                           Use '*' for all.
      --force              Reinstall even when the installed content matches the lock
      --dry-run            Print the plan and write nothing
      --frozen-lockfile    Fail on drift; the default when CI, GITHUB_ACTIONS,
                           GITLAB_CI, BUILDKITE, CIRCLECI, TRAVIS, TF_BUILD or
                           JENKINS_URL is set
      --no-frozen-lockfile Repair drift and rewrite hashes, even in CI
  -h, --help               Show this help

Check options:
  check [skill...]         Limit the check to the named skills
  -g, --global             Check the global scope
  -h, --help               Show this help

Update options:
  update [skill...]        Update every tracked skill, or only the named ones
  -p, --project            Update the project scope without asking
  -g, --global             Update the global scope
  -y, --yes                Update without prompting
  -h, --help               Show this help

Remove options:
  remove [skill...] [source]  Remove by name, or every skill tracked from an exact source
  -a, --agent <agent...>   Only these agents (default: every agent that has the skill).
                           Use '*' for all.
  -s, --skill <name...>    Skills to remove. Use '*' for every lock-tracked skill.
  -g, --global             Remove from the global scope
  -y, --yes                Remove without prompting
      --all                Remove every lock-tracked skill for every agent, without prompting
  -h, --help               Show this help

Output: progress, prompts and results go to stderr; --json, --list, list and check
reports go to stdout.
Exit codes: 0 success, 1 failure, no match or a check finding, 2 usage error.
`, strings.Join(agents.IDs(), ", "))
}
