// Package install plans and applies skill installs into a project: a canonical copy under
// .agents/skills plus one link or copy per agent directory.
package install

import (
	"path/filepath"
	"sort"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// canonicalDir is the project directory shared by agents that read .agents/skills.
const canonicalDir = ".agents/skills"

// Mode is how a skill reaches an agent directory.
type Mode string

const (
	// ModeSymlink installs the canonical copy once and links every agent directory to it.
	ModeSymlink Mode = "symlink"
	// ModeCopy writes an independent copy into each agent directory.
	ModeCopy Mode = "copy"
)

// Target is the destination of one skill for one agent.
type Target struct {
	Agent agents.Agent
	Path  string // absolute destination
}

// Entry is the plan for one skill.
type Entry struct {
	Skill     skill.Skill
	Name      string // installed name: the sanitized skill name
	Canonical string // absolute canonical copy; empty in copy mode
	Mode      Mode
	Targets   []Target

	// ForcedCopy is set when --copy overrode what would otherwise have been symlink mode. Only
	// then does upstream record installMode in the lock.
	ForcedCopy bool
}

// Plan decides, without touching the disk, where each skill goes. Copy mode is used when forceCopy
// is set or when the selected agents share at most one project skills directory. Entries are
// sorted by installed name.
func Plan(projectDir string, skills []skill.Skill, selected []agents.Agent, forceCopy bool) []Entry {
	derivedCopy := distinctDirs(selected) <= 1
	mode := ModeSymlink
	if forceCopy || derivedCopy {
		mode = ModeCopy
	}
	entries := make([]Entry, 0, len(skills))
	for _, s := range skills {
		name := skill.SanitizeName(s.Name)
		e := Entry{Skill: s, Name: name, Mode: mode, ForcedCopy: forceCopy && !derivedCopy}
		if mode == ModeSymlink {
			e.Canonical = filepath.Join(projectDir, filepath.FromSlash(canonicalDir), name)
		}
		for _, a := range selected {
			dir := filepath.Join(projectDir, filepath.FromSlash(a.ProjectDir))
			e.Targets = append(e.Targets, Target{Agent: a, Path: filepath.Join(dir, name)})
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func distinctDirs(selected []agents.Agent) int {
	dirs := map[string]bool{}
	for _, a := range selected {
		dirs[a.ProjectDir] = true
	}
	return len(dirs)
}
