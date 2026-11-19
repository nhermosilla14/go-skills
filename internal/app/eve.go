package app

import (
	"bytes"
	"os"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// evePlaced reports whether a lock entry records an Eve placement: only Eve installs write the
// subagents field.
func evePlaced(subagents []string) bool {
	return len(subagents) > 0
}

// eveFlatFiles lists the flat files an Eve placement wrote: the root agent file and one per
// recorded subagent, in the recorded order.
func eveFlatFiles(env agents.Environment, name string, subagents []string) []string {
	selected, _ := agents.Expand([]string{"eve"})
	files := make([]string, 0, len(subagents))
	for _, sub := range install.NormalizeSubagents(subagents) {
		if sub == "" {
			if len(selected) != 1 {
				continue
			}
			files = append(files, install.EveRootFile(env, selected[0], name, false))
			continue
		}
		files = append(files, install.EveSubagentFile(env, sub, name))
	}
	return files
}

// eveCurrent reports whether an Eve placement on disk still matches the fetched skill: every
// recorded file exists and the root file is byte-identical to a fresh install.
func eveCurrent(env agents.Environment, name string, subagents []string, found skill.Skill) bool {
	want, err := install.FlatContent(found, restoreRename(found, name))
	if err != nil {
		return false
	}
	files := eveFlatFiles(env, name, subagents)
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if _, err := os.Stat(file); err != nil {
			return false
		}
	}
	got, err := os.ReadFile(files[0])
	return err == nil && bytes.Equal(got, want)
}
