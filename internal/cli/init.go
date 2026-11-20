package cli

import (
	"fmt"
	"io"

	"github.com/nhermosilla14/go-skills/internal/app"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// InitHelp returns the command-specific help text for init.
func InitHelp() string {
	return `Usage: skills init [name] [options]

Create the documented SKILL.md template here (./SKILL.md), or in a new
directory (./<name>/SKILL.md) when a name is given. The name in the template
is YAML-escaped.

A name must stay inside the working directory: absolute paths and names that
escape it, such as ../x, are rejected with exit 2. An existing SKILL.md is
never overwritten; init prints its path and exits 0.

Init options:
  -h, --help           Show this help
`
}

// WriteInit prints the result of init, or the no-op notice when SKILL.md already existed.
func WriteInit(w io.Writer, rep app.InitReport) {
	display := skill.SanitizeDisplay(rep.DisplayPath)
	if rep.Exists {
		fmt.Fprintf(w, "Skill already exists at %s\n", display)
		return
	}
	fmt.Fprintf(w, "Initialized skill: %s\n\n", skill.SanitizeDisplay(rep.Name))
	fmt.Fprintf(w, "Created:\n  %s\n\n", display)
	fmt.Fprintf(w, "Next steps:\n"+
		"  1. Edit %s to define your skill instructions\n"+
		"  2. Add supporting files next to SKILL.md and reference them from the instructions.\n",
		display)
}
