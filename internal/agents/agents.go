// Package agents holds the registry of coding agents, where each one reads skills, and how it is
// detected on disk. It mirrors vercel-labs/skills v1.9.0; see
// olddocs/behavior/05-agents-and-paths.md.
package agents

import "sort"

// Agent is a coding agent that reads skills from a directory. The zero value of the optional
// flags is upstream's default.
type Agent struct {
	ID          string // identifier used on the command line
	DisplayName string // human label

	// ProjectDir is the slash-separated skills directory relative to the project root.
	ProjectDir string

	// ProjectOnly marks agents without a global skills directory.
	ProjectOnly bool

	// Universal marks agents that share .agents/skills in a project.
	Universal bool

	// ReadsCanonicalGlobally marks universal agents whose global install is satisfied by
	// ~/.agents/skills, so no agent-specific copy or link is written.
	ReadsCanonicalGlobally bool

	// HiddenFromPrompt marks universal agents kept out of the interactive universal section.
	HiddenFromPrompt bool

	// NotAutoAdded marks universal agents outside the universal set: they are only installed when
	// named explicitly or through '*'.
	NotAutoAdded bool

	// CreateProjectDirByDefault lets a project install create the agent's top-level directory when
	// it is missing. Only Claude Code sets it upstream.
	CreateProjectDirByDefault bool

	globalDir func(Environment) string // absolute global skills directory; nil when project-only
	detect    func(Environment) bool   // stat-based detection; nil means never detected
}

// GlobalSkillsDir returns the agent's own global skills directory, or "" when it has none. A
// universal agent that reads the canonical directory still reports its legacy path; BaseDir is
// what decides where an install goes.
func (a Agent) GlobalSkillsDir(env Environment) string {
	if a.globalDir == nil {
		return ""
	}
	return a.globalDir(env)
}

// ProjectSkillsDir returns the agent's project skills directory, ignoring universality.
func (a Agent) ProjectSkillsDir(env Environment) string {
	return join(env.Cwd, a.ProjectDir)
}

// BaseDir is where the agent reads skills for a scope: the canonical directory for universal
// project installs and for universal agents that read it globally, and the agent's own directory
// otherwise. A project-only agent asked for global resolves to <home>/<project dir>, which
// callers reject before installing (upstream's fallback).
func (a Agent) BaseDir(env Environment, global bool) string {
	if global {
		if a.ReadsCanonicalGlobally {
			return CanonicalSkillsDir(env, true)
		}
		if dir := a.GlobalSkillsDir(env); dir != "" {
			return dir
		}
		return join(env.Home, a.ProjectDir)
	}
	if a.Universal {
		return CanonicalSkillsDir(env, false)
	}
	return a.ProjectSkillsDir(env)
}

// Detected reports whether the agent looks installed. Errors are not installed.
func (a Agent) Detected(env Environment) bool {
	return a.detect != nil && a.detect(env)
}

// All returns every known agent in registry order.
func All() []Agent {
	return append([]Agent(nil), registry...)
}

// IDs returns the identifiers of all known agents in registry order.
func IDs() []string {
	ids := make([]string, 0, len(registry))
	for _, a := range registry {
		ids = append(ids, a.ID)
	}
	return ids
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

// Detect returns the agents whose detector matches, in registry order.
func Detect(env Environment) []Agent {
	var found []Agent
	for _, a := range registry {
		if a.Detected(env) {
			found = append(found, a)
		}
	}
	return found
}

// Select returns the targets of a run that names no agent. With yes, it follows upstream's
// non-interactive rules: a detected Eve alone wins, nothing detected targets every agent, and
// otherwise the detected agents get the auto-added universal agents. Without yes the result is
// the interactive starting set; the interactive picker is not implemented yet, so the caller
// still asks for confirmation.
func Select(env Environment, yes bool) []Agent {
	detected := Detect(env)
	for _, a := range detected {
		if a.ID == "eve" {
			return []Agent{a}
		}
	}
	if len(detected) == 0 {
		if yes {
			return All()
		}
		return allExcept("eve")
	}
	return ensureUniversalAgents(detected)
}

// ensureUniversalAgents appends every universal agent that reads the canonical global directory,
// hidden ones included, keeping the base order. replit and universal are named-only targets and
// the Antigravity pair does not read the canonical directory, so none of them is added.
func ensureUniversalAgents(base []Agent) []Agent {
	out := append([]Agent(nil), base...)
	have := map[string]bool{}
	for _, a := range base {
		have[a.ID] = true
	}
	for _, a := range registry {
		if a.Universal && a.ReadsCanonicalGlobally && !a.NotAutoAdded && !have[a.ID] {
			out = append(out, a)
			have[a.ID] = true
		}
	}
	return out
}

func allExcept(id string) []Agent {
	out := make([]Agent, 0, len(registry)-1)
	for _, a := range registry {
		if a.ID != id {
			out = append(out, a)
		}
	}
	return out
}

// legacyProjectSkillDirs are project directories published by skills before their agents moved to
// the shared .agents/skills.
var legacyProjectSkillDirs = []string{
	".cline/skills", ".codex/skills", ".factory/skills", ".github/skills",
	".kilo/skills", ".kilocode/skills", ".opencode/skills", ".pi/skills",
}

// ProjectSkillDirs returns the project directories agents read skills from, including the legacy
// ones, sorted and deduplicated. The bare "skills" directory is excluded because discovery walks
// it as its own container.
func ProjectSkillDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(dir string) {
		if dir == "skills" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	for _, a := range registry {
		add(a.ProjectDir)
	}
	for _, dir := range legacyProjectSkillDirs {
		add(dir)
	}
	sort.Strings(dirs)
	return dirs
}

func lookup(id string) (Agent, bool) {
	for _, a := range registry {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}
