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
  add <source>      Install the skills found in a local directory
  help              Show this help
  version           Show version, commit and build date

Add options:
  -a, --agent <agent...>   Agents to install for (default: all). Use '*' for all.
                           Known agents: %s
  -s, --skill <name...>    Skills to install (default: all). Use '*' for all.
  -y, --yes                Install without prompting (required for now)
      --all                Every skill for every agent, without prompting
      --copy               Copy skills instead of linking them
      --rename <name>      Install one selected skill under a new name
      --full-depth         Search all subdirectories, even under a root SKILL.md
  -l, --list               List the skills in the source and exit
  -h, --help               Show this help

Output: progress and results go to stderr; --list writes to stdout.
Exit codes: 0 success, 1 failure, 2 usage error.
`, strings.Join(agents.IDs(), ", "))
}
