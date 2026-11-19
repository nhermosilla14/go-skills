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
  list, ls          List installed project skills and their lock status
  check [skill...]  Report available updates and local changes, without writing
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
      --json               Print one JSON array on stdout
  -h, --help               Show this help

Check options:
  check [skill...]         Limit the check to the named skills
  -h, --help               Show this help

Output: progress, prompts and results go to stderr; --json, --list, list and check
reports go to stdout.
Exit codes: 0 success, 1 failure or a check finding, 2 usage error.
`, strings.Join(agents.IDs(), ", "))
}
