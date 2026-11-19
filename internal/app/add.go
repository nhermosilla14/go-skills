// Package app holds the use cases. It orchestrates discovery, planning, installing and lock
// recording, and owns no command syntax or output formatting.
package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// ErrUsage marks a request that cannot be honoured because of how it was invoked. The CLI maps it
// to exit code 2.
var ErrUsage = errors.New("usage error")

// ErrPromptRequired reports that the install needs an answer to a prompt, and there is no
// interactive flow yet, so -y is required.
var ErrPromptRequired = errors.New("prompt required")

// AddOptions are the inputs of the add use case, as parsed from the command line.
type AddOptions struct {
	Source    string   // local directory containing skills
	Agents    []string // agent identifiers, or "*"; empty selects every agent
	Skills    []string // skill names, or "*"; empty selects every skill
	Yes       bool     // proceed without prompting
	Copy      bool     // copy instead of linking
	List      bool     // list the skills and install nothing
	FullDepth bool     // search every subdirectory, even below a root SKILL.md
	Rename    string   // install one skill under this name
}

// Env carries what the use case needs from the process. Now must not be nil.
type Env struct {
	WorkDir         string // project directory; the source and lock are resolved from it
	IsTTY           bool   // whether stdin is a terminal
	IncludeInternal bool   // install skills marked metadata.internal
	Now             func() time.Time
	AgentEnv        agents.Environment // home, OS and variables used by detection and paths
}

// Report is the outcome of an add run, for the CLI to render.
type Report struct {
	Listed   []skill.Skill    // set by --list
	Warnings []string         // skills that were skipped while discovering
	Results  []install.Result // one per skill and agent
	LockPath string           // set when the lock file was written
}

// Failed reports whether any skill failed for any agent.
func (r Report) Failed() bool {
	for _, res := range r.Results {
		if res.Status == install.Failed {
			return true
		}
	}
	return false
}

// Add installs the skills of a local source into the project. The flow is: validate agents,
// resolve the source, discover, stop for --list, require -y, select, plan, apply, record.
func Add(opts AddOptions, env Env) (Report, error) {
	agentEnv := env.agentsEnv()
	selected, err := selectAgents(opts.Agents, agentEnv, opts.Yes)
	if err != nil {
		return Report{}, err
	}
	src, err := sourceDir(env.WorkDir, opts.Source)
	if err != nil {
		return Report{}, err
	}
	found, err := skill.Discover(src, skill.Options{
		IncludeInternal: env.IncludeInternal,
		FullDepth:       opts.FullDepth,
	})
	if err != nil {
		return Report{}, err
	}
	rep := Report{Warnings: found.Warnings}
	if len(found.Skills) == 0 {
		return rep, fmt.Errorf("no valid skills found in %s", src)
	}
	if opts.List {
		rep.Listed = found.Skills
		return rep, nil
	}
	if !opts.Yes {
		return rep, promptRequired(env.IsTTY)
	}
	chosen, err := selectSkills(found.Skills, opts.Skills)
	if err != nil {
		return rep, err
	}
	if err := validateRename(opts.Rename, chosen); err != nil {
		return rep, err
	}
	lockPath := filepath.Join(env.WorkDir, lockfile.FileName)
	lock, err := lockfile.Load(lockPath)
	if err != nil {
		return rep, err
	}
	entries := install.Plan(env.WorkDir, chosen, selected, install.PlanOptions{
		Env:       agentEnv,
		ForceCopy: opts.Copy,
		Rename:    opts.Rename,
	})
	rep.Results = install.Apply(env.WorkDir, entries)
	saved, err := recordInstalled(lock, lockPath, recordInput{
		project: env.WorkDir,
		source:  src,
		entries: entries,
		results: rep.Results,
	})
	if err != nil {
		return rep, err
	}
	if saved {
		rep.LockPath = lockPath
	}
	return rep, nil
}

// selectAgents resolves the requested agent identifiers. No identifiers uses detection when
// installing non-interactively, and the interactive starting set otherwise.
func selectAgents(ids []string, env agents.Environment, yes bool) ([]agents.Agent, error) {
	if len(ids) == 0 {
		return agents.Select(env, yes), nil
	}
	selected, invalid := agents.Expand(ids)
	if len(invalid) > 0 {
		return nil, fmt.Errorf("%w: invalid agents: %s (valid: %s)", ErrUsage,
			strings.Join(invalid, ", "), strings.Join(agents.IDs(), ", "))
	}
	return selected, nil
}

// agentsEnv fills the environment from the process when the caller passed none, so detection sees
// the real home and variables. A partially filled environment is used as given.
func (e Env) agentsEnv() agents.Environment {
	env := e.AgentEnv
	if env.Home == "" && env.Vars == nil && env.GOOS == "" &&
		env.ApplicationsDir == "" && env.EtcDir == "" {
		env = agents.FromOS(e.WorkDir)
	}
	if env.Cwd == "" {
		env.Cwd = e.WorkDir
	}
	return env
}

// sourceDir resolves a source argument to an absolute directory.
func sourceDir(workDir, source string) (string, error) {
	p := source
	if !filepath.IsAbs(p) {
		p = filepath.Join(workDir, p)
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("source not found: %s", source)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source is not a directory: %s", source)
	}
	return filepath.Clean(p), nil
}

func promptRequired(isTTY bool) error {
	if isTTY {
		return fmt.Errorf("%w: interactive selection is not available yet; pass -y to install", ErrPromptRequired)
	}
	return fmt.Errorf("%w: stdin is not a terminal; pass -y to install without prompts", ErrPromptRequired)
}

// selectSkills picks the skills named on the command line. No names, or "*", selects all.
func selectSkills(all []skill.Skill, names []string) ([]skill.Skill, error) {
	if len(names) == 0 {
		return all, nil
	}
	var chosen []skill.Skill
	for _, name := range names {
		if name == "*" {
			return all, nil
		}
		matches := skill.Filter(all, name)
		if len(matches) == 0 {
			return nil, fmt.Errorf("no skill named %q in source", name)
		}
		chosen = append(chosen, matches...)
	}
	return chosen, nil
}

// validateRename checks the --rename restrictions: the name must already be sanitized and exactly
// one skill must be selected.
func validateRename(name string, chosen []skill.Skill) error {
	if name == "" {
		return nil
	}
	if skill.SanitizeName(name) != name {
		return fmt.Errorf("--rename must be a skill name of lowercase letters, digits, \".\", \"_\" and \"-\"")
	}
	if len(chosen) != 1 {
		return fmt.Errorf("--rename installs one skill under a new name, but %d skills were selected", len(chosen))
	}
	return nil
}

type recordInput struct {
	project string
	source  string
	entries []install.Entry
	results []install.Result
}

// recordInstalled adds every skill that landed for at least one agent and failed for none to the
// lock, then saves the lock if anything was recorded. It reports whether the file was written.
func recordInstalled(lock *lockfile.File, lockPath string, in recordInput) (bool, error) {
	recorded := false
	for _, e := range in.entries {
		loc, ok := installedPath(in.results, e.Name)
		if !ok || failedAny(in.results, e.Name) {
			continue
		}
		digest, err := hashInstalled(loc)
		if err != nil {
			return false, fmt.Errorf("hashing %s: %w", e.Name, err)
		}
		lock.Put(e.Name, lockEntry(in, e, digest, lock))
		recorded = true
	}
	if !recorded {
		return false, nil
	}
	if err := lock.Save(lockPath); err != nil {
		return false, err
	}
	return true, nil
}

// lockEntry builds the lock record for one installed skill. installedAt keeps the value of an
// earlier install of the same name.
func lockEntry(in recordInput, e install.Entry, digest string, lock *lockfile.File) lockfile.Entry {
	entry := lockfile.Entry{
		Source:       localSource(in.project, in.source),
		SourceType:   "local",
		ComputedHash: digest,
	}
	if e.RenameTo != "" && e.RenameTo != e.Skill.Name {
		entry.SkillName = e.Skill.Name
	}
	if e.ForcedCopy {
		entry.InstallMode = "copy"
	}
	return entry
}

// hashInstalled hashes the installed skill: a directory normally, and the flat markdown file Eve
// writes.
func hashInstalled(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return hash.Dir(path)
	}
	return hash.SingleFile(path)
}

// installedPath returns where the skill was first installed successfully.
func installedPath(results []install.Result, name string) (string, bool) {
	for _, r := range results {
		if r.Skill == name && r.Status == install.Installed {
			return r.Path, true
		}
	}
	return "", false
}

func failedAny(results []install.Result, name string) bool {
	for _, r := range results {
		if r.Skill == name && r.Status == install.Failed {
			return true
		}
	}
	return false
}

// localSource writes a local source path relative to the project when it lies inside it, so the
// lock stays valid after the project moves.
func localSource(project, source string) string {
	rel, err := filepath.Rel(project, source)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(source)
	}
	if rel == "." {
		return "."
	}
	return "./" + filepath.ToSlash(rel)
}
