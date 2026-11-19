// Package install plans and applies skill installs: a canonical copy under .agents/skills plus
// one link or copy per agent directory, with Eve's flat-file and the agents' path quirks.
package install

import (
	"path/filepath"
	"sort"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

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
	Flat  bool   // a single markdown file instead of a directory (Eve)
}

// Entry is the plan for one skill.
type Entry struct {
	Skill     skill.Skill
	Name      string // installed name: the sanitized skill name or the --rename value
	Canonical string // absolute canonical copy; empty in copy mode
	Mode      Mode
	Targets   []Target

	// RenameTo is the value written into the installed SKILL.md name field. It is empty unless
	// --rename was used.
	RenameTo string

	// ForcedCopy is set when --copy overrode what would otherwise have been symlink mode. Only
	// then does upstream record installMode in the lock.
	ForcedCopy bool
}

// PlanOptions are the inputs of planning that do not come from the skills or agents.
type PlanOptions struct {
	Env       agents.Environment // Cwd and Home used to resolve destinations
	ForceCopy bool               // --copy
	Global    bool               // global scope; its lock and CLI wiring arrive later
	Rename    string             // --rename value
	Mode      Mode               // force symlink or copy; empty derives it from the agents
}

// Plan decides, without touching the disk, where each skill goes. Copy mode is used when ForceCopy
// is set, when the selected agents share at most one project skills directory, or when every
// target is Eve, unless Mode forces a mode. A non-empty rename installs every skill under that
// name, which the caller restricts to one skill. Entries are sorted by installed name.
func Plan(projectDir string, skills []skill.Skill, selected []agents.Agent, opts PlanOptions) []Entry {
	env := opts.Env
	env.Cwd = projectDir
	derivedCopy := derivedCopyMode(selected)
	mode := ResolveMode(selected, opts.ForceCopy)
	if opts.Mode != "" {
		mode = opts.Mode
	}
	entries := make([]Entry, 0, len(skills))
	for _, s := range skills {
		name := skill.SanitizeName(s.Name)
		if opts.Rename != "" {
			name = opts.Rename
		}
		e := Entry{Skill: s, Name: name, Mode: mode, ForcedCopy: opts.ForceCopy && !derivedCopy, RenameTo: opts.Rename}
		if mode == ModeSymlink {
			e.Canonical = filepath.Join(agents.CanonicalSkillsDir(env, opts.Global), name)
		}
		for _, a := range selected {
			target := Target{Agent: a, Path: filepath.Join(a.BaseDir(env, opts.Global), name)}
			if a.ID == "eve" {
				target.Path = filepath.Join(a.BaseDir(env, opts.Global), name+".md")
				target.Flat = true
			}
			e.Targets = append(e.Targets, target)
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// ResolveMode reports how a run installs: copy when forced, when the selected agents share at
// most one project skills directory, or when every target is Eve; symlink otherwise.
func ResolveMode(selected []agents.Agent, forceCopy bool) Mode {
	if forceCopy || derivedCopyMode(selected) {
		return ModeCopy
	}
	return ModeSymlink
}

// derivedCopyMode reports whether the selected agents make copy the only sensible mode.
func derivedCopyMode(selected []agents.Agent) bool {
	return distinctDirs(selected) <= 1 || allEve(selected)
}

// distinctDirs counts the project skills directories of the selected agents. It deliberately uses
// the project paths even for global installs, as upstream's mode decision does.
func distinctDirs(selected []agents.Agent) int {
	dirs := map[string]bool{}
	for _, a := range selected {
		dirs[a.ProjectDir] = true
	}
	return len(dirs)
}

func allEve(selected []agents.Agent) bool {
	if len(selected) == 0 {
		return false
	}
	for _, a := range selected {
		if a.ID != "eve" {
			return false
		}
	}
	return true
}
