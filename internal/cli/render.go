package cli

import (
	"fmt"
	"io"

	"github.com/nhermosilla14/go-skills/internal/app"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// WriteListing prints the discovered skills, one per line: name, description and SKILL.md path.
func WriteListing(w io.Writer, skills []skill.Skill) {
	for _, s := range skills {
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.Description, s.SkillPath())
	}
}

// WriteReport prints one line per skill and agent, followed by totals and the lock path.
func WriteReport(w io.Writer, rep app.Report) {
	counts := map[install.Status]int{}
	for _, r := range rep.Results {
		counts[r.Status]++
		line := fmt.Sprintf("%s: %s -> %s (%s) %s", r.Status, r.Skill, r.Agent, r.Mode, r.Path)
		if r.Message != "" {
			line += ": " + r.Message
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "Installed %d, skipped %d, failed %d\n",
		counts[install.Installed], counts[install.Skipped], counts[install.Failed])
	if rep.LockPath != "" {
		fmt.Fprintf(w, "Updated %s\n", rep.LockPath)
	}
}
