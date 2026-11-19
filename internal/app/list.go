package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// Status is how an installed skill compares with the project lock.
type Status string

const (
	// StatusTracked means the skill is on disk and its content hash matches the lock.
	StatusTracked Status = "tracked"
	// StatusModified means the skill is on disk but its content differs from the lock. A lock entry
	// without a hash counts as modified: unverifiable is changed.
	StatusModified Status = "modified"
	// StatusUntracked means the skill is on disk and the lock has no entry for it.
	StatusUntracked Status = "untracked"
	// StatusMissing means the lock has an entry but no directory on disk.
	StatusMissing Status = "missing"
)

// ListOptions are the inputs of the list use case.
type ListOptions struct {
	Agents []string // agent identifiers, or "*"; empty lists every agent
	Global bool     // global scope; not supported yet
}

// SkillInfo is one skill of a list report, in the shape the CLI renders.
type SkillInfo struct {
	Name        string
	Path        string // absolute skill directory; empty for a missing skill
	Agents      []string
	Source      string
	SourceURL   string
	SourceType  string
	RenamedFrom string
	Status      Status
}

// ListReport is the outcome of a list run.
type ListReport struct {
	Scope    string
	Skills   []SkillInfo
	Warnings []string
}

// List reports the installed project skills, their agents and how they compare with the lock. It
// never writes anything.
func List(opts ListOptions, env Env) (ListReport, error) {
	if opts.Global {
		return ListReport{}, fmt.Errorf("%w: global scope is not supported yet", ErrUsage)
	}
	agentEnv := env.agentsEnv()
	selected, err := listAgents(opts.Agents)
	if err != nil {
		return ListReport{}, err
	}
	filtered := len(opts.Agents) > 0
	installed := scanInstalled(agentEnv, selected, filtered, env.IncludeInternal)

	report := ListReport{Scope: string(ScopeProject)}
	lockPath := filepath.Join(env.WorkDir, lockfile.FileName)
	lock, err := lockfile.Load(lockPath)
	if err != nil {
		// Listing is read-only: a broken lock hides the sources but never fails the run.
		report.Warnings = append(report.Warnings, fmt.Sprintf("ignoring %s: %v", lockPath, err))
		lock = lockfile.New()
	}
	matched, missing := compareLock(lock, installed)
	for _, c := range matched {
		report.Skills = append(report.Skills, skillInfo(c))
	}
	// A filtered scan hides other on-disk skills, so absence would be a false positive.
	if !filtered {
		for _, key := range missing {
			entry, _ := lock.Get(key)
			report.Skills = append(report.Skills, SkillInfo{
				Name:        key,
				Agents:      []string{},
				Source:      entry.Source,
				SourceURL:   entry.SourceURL,
				SourceType:  entry.SourceType,
				RenamedFrom: entry.SkillName,
				Status:      StatusMissing,
			})
		}
	}
	// Stable sorting by name over a deterministic scan keeps the agent order for equal names.
	sort.SliceStable(report.Skills, func(i, j int) bool {
		return report.Skills[i].Name < report.Skills[j].Name
	})
	return report, nil
}

// listAgents resolves the agent filter. No filter means every agent.
func listAgents(ids []string) ([]agents.Agent, error) {
	if len(ids) == 0 {
		return agents.All(), nil
	}
	selected, invalid := agents.Expand(ids)
	if len(invalid) > 0 {
		return nil, fmt.Errorf("%w: invalid agents: %s (valid: %s)", ErrUsage,
			strings.Join(invalid, ", "), strings.Join(agents.IDs(), ", "))
	}
	return selected, nil
}

// installedSkill is one skill directory found on disk, with every agent whose directory holds it.
type installedSkill struct {
	Name   string // frontmatter name
	Path   string // absolute directory as scanned
	Dir    string // resolved directory, the identity used to dedupe and to hash
	Agents []agents.Agent
}

// scanDir is one skills directory to scan and the agents that read it.
type scanDir struct {
	path   string
	agents []agents.Agent
}

// scanInstalled finds the installed skills in the project scope. It scans the canonical directory,
// every agent skills directory that exists (including shared and legacy ones) and Eve's subagent
// directories, deduplicating by resolved skill directory. With filtered set, only the selected
// agents' directories are scanned.
func scanInstalled(env agents.Environment, selected []agents.Agent, filtered, includeInternal bool) []installedSkill {
	dirs := projectScanDirs(env, selected, filtered)
	seen := map[string]int{}
	var out []installedSkill
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir.path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			full := filepath.Join(dir.path, entry.Name())
			meta, ok := readInstalledSkill(full, includeInternal)
			if !ok {
				continue
			}
			resolved, err := filepath.EvalSymlinks(full)
			if err != nil {
				resolved = filepath.Clean(full)
			}
			if i, seenBefore := seen[resolved]; seenBefore {
				out[i].Agents = append(out[i].Agents, dir.agents...)
				continue
			}
			seen[resolved] = len(out)
			out = append(out, installedSkill{
				Name:   meta.Name,
				Path:   filepath.Clean(full),
				Dir:    resolved,
				Agents: append([]agents.Agent(nil), dir.agents...),
			})
		}
	}
	return out
}

// projectScanDirs lists the project skills directories to scan, canonical first, then the agent
// directories in registry order and finally Eve's subagent directories. Duplicate paths merge
// their agent lists.
func projectScanDirs(env agents.Environment, selected []agents.Agent, filtered bool) []scanDir {
	byPath := map[string][]agents.Agent{}
	var order []string
	add := func(path string, group []agents.Agent) {
		clean := filepath.Clean(path)
		if _, ok := byPath[clean]; !ok {
			order = append(order, clean)
		}
		byPath[clean] = append(byPath[clean], group...)
	}

	if filtered {
		for _, a := range selected {
			add(a.ProjectSkillsDir(env), []agents.Agent{a})
		}
	} else {
		add(agents.CanonicalSkillsDir(env, false), nil)
		for _, a := range selected {
			add(a.ProjectSkillsDir(env), []agents.Agent{a})
		}
		// Legacy directories are not attached to any agent anymore, but still hold skills.
		for _, rel := range agents.ProjectSkillDirs() {
			path := filepath.Join(env.Cwd, filepath.FromSlash(rel))
			if _, ok := byPath[filepath.Clean(path)]; !ok {
				add(path, nil)
			}
		}
	}

	if eve := eveAgent(selected, filtered); eve != nil {
		matches, _ := filepath.Glob(filepath.Join(env.Cwd, "agent", "subagents", "*", "skills"))
		sort.Strings(matches)
		for _, path := range matches {
			add(path, []agents.Agent{*eve})
		}
	}

	dirs := make([]scanDir, 0, len(order))
	for _, path := range order {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		dirs = append(dirs, scanDir{path: path, agents: byPath[path]})
	}
	return dirs
}

// eveAgent returns Eve when its subagent directories have to be scanned: always without a filter,
// and when Eve is one of the selected agents with one.
func eveAgent(selected []agents.Agent, filtered bool) *agents.Agent {
	if !filtered {
		for _, a := range agents.All() {
			if a.ID == "eve" {
				return &a
			}
		}
		return nil
	}
	for _, a := range selected {
		if a.ID == "eve" {
			return &a
		}
	}
	return nil
}

// readInstalledSkill reports the metadata of dir when it is a directory holding a parseable,
// non-internal (unless allowed) SKILL.md.
func readInstalledSkill(dir string, includeInternal bool) (skill.Meta, bool) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return skill.Meta{}, false
	}
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return skill.Meta{}, false
	}
	meta, err := skill.ParseSkillMD(raw)
	if err != nil {
		return skill.Meta{}, false
	}
	if meta.Internal && !includeInternal {
		return skill.Meta{}, false
	}
	return meta, true
}

// comparedSkill is an installed skill paired with the lock entry that describes it.
type comparedSkill struct {
	installedSkill
	Status  Status
	LockKey string // empty when the skill is untracked
	Entry   lockfile.Entry
}

// compareLock pairs installed skills with lock entries and lists the lock keys with no directory.
func compareLock(lock *lockfile.File, installed []installedSkill) ([]comparedSkill, []string) {
	matched := make([]comparedSkill, 0, len(installed))
	used := map[string]bool{}
	for _, s := range installed {
		key, entry, ok := lookupEntry(lock, filepath.Base(s.Path), s.Name)
		c := comparedSkill{installedSkill: s, Status: StatusUntracked, LockKey: key, Entry: entry}
		if ok {
			used[key] = true
			c.Status = compareHash(s.Dir, entry)
		}
		matched = append(matched, c)
	}
	var missing []string
	for _, key := range lockKeys(lock) {
		if !used[key] {
			missing = append(missing, key)
		}
	}
	return matched, missing
}

// lookupEntry finds the lock entry for an installed skill: the exact folder or frontmatter name
// first, then any key whose sanitized form is one of them (so "ce:review" matches "ce-review").
func lookupEntry(lock *lockfile.File, names ...string) (string, lockfile.Entry, bool) {
	for _, name := range names {
		if name == "" {
			continue
		}
		if entry, ok := lock.Get(name); ok {
			return name, entry, true
		}
	}
	for _, key := range lockKeys(lock) {
		sanitized := skill.SanitizeName(key)
		for _, name := range names {
			if name != "" && sanitized == name {
				return key, lock.Skills[key], true
			}
		}
	}
	return "", lockfile.Entry{}, false
}

// compareHash reports whether the on-disk skill still matches its lock entry. A hash that cannot be
// computed and an entry without a hash are both treated as modified.
func compareHash(dir string, entry lockfile.Entry) Status {
	if entry.ComputedHash == "" {
		return StatusModified
	}
	digest, err := hash.Dir(dir)
	if err != nil || digest != entry.ComputedHash {
		return StatusModified
	}
	return StatusTracked
}

// skillInfo converts a compared skill into the report shape.
func skillInfo(c comparedSkill) SkillInfo {
	return SkillInfo{
		Name:        c.Name,
		Path:        c.Path,
		Agents:      displayNames(c.Agents),
		Source:      c.Entry.Source,
		SourceURL:   c.Entry.SourceURL,
		SourceType:  c.Entry.SourceType,
		RenamedFrom: c.Entry.SkillName,
		Status:      c.Status,
	}
}

// displayNames returns the unique display names of the agents in registry order.
func displayNames(list []agents.Agent) []string {
	byID := map[string]agents.Agent{}
	rank := map[string]int{}
	for i, a := range agents.All() {
		byID[a.ID] = a
		rank[a.ID] = i
	}
	var ids []string
	seen := map[string]bool{}
	for _, a := range list {
		if !seen[a.ID] {
			seen[a.ID] = true
			ids = append(ids, a.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return rank[ids[i]] < rank[ids[j]] })
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, byID[id].DisplayName)
	}
	return names
}

// lockKeys returns the lock keys in sorted order.
func lockKeys(lock *lockfile.File) []string {
	keys := make([]string, 0, len(lock.Skills))
	for key := range lock.Skills {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
