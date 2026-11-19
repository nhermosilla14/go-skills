// Package agents holds the registry of coding agents and where each one reads project skills.
package agents

// Agent is a coding agent that reads skills from a directory.
type Agent struct {
	ID         string // identifier used on the command line
	Name       string // display name
	ProjectDir string // slash-separated skills directory relative to the project root
}

// registry is the ordered list of known agents. Adding an agent is adding an entry here.
var registry = []Agent{
	{ID: "opencode", Name: "OpenCode", ProjectDir: ".agents/skills"},
	{ID: "claude-code", Name: "Claude Code", ProjectDir: ".claude/skills"},
}

// All returns every known agent in registry order.
func All() []Agent {
	return append([]Agent(nil), registry...)
}

// Expand resolves agent identifiers to agents. "*" selects every agent. The result follows
// registry order without duplicates; unknown identifiers are returned in invalid.
func Expand(ids []string) (selected []Agent, invalid []string) {
	want := map[string]bool{}
	for _, id := range ids {
		if id == "*" {
			for _, a := range registry {
				want[a.ID] = true
			}
			continue
		}
		if _, ok := lookup(id); ok {
			want[id] = true
		} else {
			invalid = append(invalid, id)
		}
	}
	for _, a := range registry {
		if want[a.ID] {
			selected = append(selected, a)
		}
	}
	return selected, invalid
}

// IDs returns the identifiers of all known agents in registry order.
func IDs() []string {
	ids := make([]string, 0, len(registry))
	for _, a := range registry {
		ids = append(ids, a.ID)
	}
	return ids
}

func lookup(id string) (Agent, bool) {
	for _, a := range registry {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}
