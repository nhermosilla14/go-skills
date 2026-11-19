package app

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

// now is the run clock. A caller that left it unset gets the wall clock instead of a panic.
func (e Env) now() time.Time {
	if e.Now == nil {
		return time.Now()
	}
	return e.Now()
}

// loadScopeLock reads the lock of the scope. The other scope's lock is never touched.
func loadScopeLock(env Env, agentEnv agents.Environment, global bool) (string, *lockfile.File,
	*lockfile.GlobalFile, error) {
	if global {
		path := agents.GlobalLockPath(agentEnv)
		lock, err := lockfile.LoadGlobal(path)
		return path, nil, lock, err
	}
	path := filepath.Join(env.WorkDir, lockfile.FileName)
	lock, err := lockfile.Load(path)
	return path, lock, nil, err
}

// recordAgentSelection saves the agent selection of a project add in the global lock, which is
// where restore reads the fallback from. A malformed global lock is reported, never overwritten.
func recordAgentSelection(env agents.Environment, ids []string) error {
	path := agents.GlobalLockPath(env)
	lock, err := lockfile.LoadGlobal(path)
	if err != nil {
		return err
	}
	lock.SetLastSelectedAgents(ids)
	return lock.Save(path)
}

// splitGlobalAgents separates the agents that have a global skills directory from the project-only
// ones. The caller decides whether the project-only agents are dropped with a notice or reported
// as failed targets.
func splitGlobalAgents(selected []agents.Agent, env agents.Environment) (kept, projectOnly []agents.Agent) {
	for _, a := range selected {
		if a.GlobalSkillsDir(env) == "" {
			projectOnly = append(projectOnly, a)
			continue
		}
		kept = append(kept, a)
	}
	return kept, projectOnly
}

// globalFailures is one failed result per skill and project-only agent, with the documented error.
func globalFailures(entries []install.Entry, failed []agents.Agent) []install.Result {
	var out []install.Result
	for _, e := range entries {
		for _, a := range failed {
			out = append(out, install.Result{
				Skill:   e.Name,
				Agent:   a.ID,
				Status:  install.Failed,
				Message: a.DisplayName + " does not support global skill installation",
			})
		}
	}
	return out
}

// specificAgentSelection reports whether a selection names an agent rather than only the wildcard.
func specificAgentSelection(ids []string) bool {
	for _, id := range ids {
		if id != "*" {
			return true
		}
	}
	return false
}

// agentNames joins the display names of a list of agents.
func agentNames(list []agents.Agent) string {
	names := make([]string, 0, len(list))
	for _, a := range list {
		names = append(names, a.DisplayName)
	}
	return strings.Join(names, ", ")
}

// globalAgentDir is the directory an agent reads global skills from: the canonical directory for
// the universal agents that read it, and the agent's own directory otherwise. It is the global view
// of Agent.BaseDir, the one resolver every command uses, and it stays empty for project-only
// agents, which a global run rejects instead of guessing a path under home.
func globalAgentDir(a agents.Agent, env agents.Environment) string {
	if a.GlobalSkillsDir(env) == "" && !a.ReadsCanonicalGlobally {
		return ""
	}
	return a.BaseDir(env, true)
}

// scopeCanonicalDir is the canonical skills directory of the scope.
func scopeCanonicalDir(env agents.Environment, global bool) string {
	return agents.CanonicalSkillsDir(env, global)
}
