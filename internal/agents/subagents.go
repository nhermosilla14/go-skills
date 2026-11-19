package agents

import (
	"os"
	"path/filepath"
	"sort"
)

// EveSubagents returns the subagent names of an Eve project: the directories directly under
// <cwd>/agent/subagents, sorted. A project without that directory has none.
func EveSubagents(env Environment) []string {
	entries, err := os.ReadDir(filepath.Join(env.Cwd, "agent", "subagents"))
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}
