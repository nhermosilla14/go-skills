package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/source"
)

// CheckOptions are the inputs of the check use case.
type CheckOptions struct {
	Skills []string      // skill names to check; empty checks every tracked skill
	Global bool          // check the global scope
	Runner source.Runner // git runner; nil uses PATH, tests pass a fake
}

// CheckReport is the outcome of a check run.
type CheckReport struct {
	Scope    string
	Updates  []string // skills with a newer commit upstream
	Modified []string // skills whose files differ from the lock
	Failures []string // sources or names that could not be checked
}

// Failed reports whether the run found anything that must make the command exit non-zero.
func (r CheckReport) Failed() bool {
	return len(r.Updates) > 0 || len(r.Modified) > 0 || len(r.Failures) > 0
}

// Check reports available updates and local changes without writing anything.
func Check(ctx context.Context, opts CheckOptions, env Env) (CheckReport, error) {
	agentEnv := env.agentsEnv()
	var (
		lock      *lockfile.File
		global    *lockfile.GlobalFile
		installed []installedSkill
		keys      []string
		matched   []comparedSkill
	)
	report := CheckReport{Scope: scopeName(opts.Global)}
	if opts.Global {
		globalLock, err := lockfile.LoadGlobal(agents.GlobalLockPath(agentEnv))
		if err != nil {
			return CheckReport{}, err
		}
		global = globalLock
		installed = scanInstalled(agentEnv, agents.All(), false, env.IncludeInternal, true)
		matched, _ = compareGlobalLock(global, installed)
		keys = globalLockKeys(global)
	} else {
		projectLock, err := lockfile.Load(filepath.Join(env.WorkDir, lockfile.FileName))
		if err != nil {
			return CheckReport{}, err
		}
		lock = projectLock
		installed = scanInstalled(agentEnv, agents.All(), false, env.IncludeInternal, false)
		matched, _ = compareLock(lock, installed)
		keys = lockKeys(lock)
	}
	byKey := map[string]comparedSkill{}
	for _, c := range matched {
		if c.LockKey == "" {
			continue
		}
		if _, ok := byKey[c.LockKey]; !ok {
			byKey[c.LockKey] = c
		}
	}

	selected, unmatched := selectCheckKeys(keys, opts.Skills)
	for _, name := range unmatched {
		report.Failures = append(report.Failures,
			fmt.Sprintf("%s: no installed skill matches this name", name))
	}
	for _, key := range keys {
		if len(opts.Skills) > 0 && !selected[key] {
			continue
		}
		c, ok := byKey[key]
		if !ok {
			continue // in the lock but not on disk: nothing installed to check
		}
		if c.Status == StatusModified {
			report.Modified = append(report.Modified, key)
		}
		update, err := checkUpdate(ctx, c.Ref, opts.Runner)
		if err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		if update {
			report.Updates = append(report.Updates, key)
		}
	}
	return report, nil
}

// selectCheckKeys resolves the optional name filter, reporting the names that match no lock key.
func selectCheckKeys(keys []string, names []string) (map[string]bool, []string) {
	if len(names) == 0 {
		return nil, nil
	}
	selected := map[string]bool{}
	var unmatched []string
	for _, name := range names {
		found := false
		for _, key := range keys {
			if strings.EqualFold(key, name) {
				selected[key] = true
				found = true
			}
		}
		if !found {
			unmatched = append(unmatched, name)
		}
	}
	return selected, unmatched
}

// checkUpdate reports whether the recorded ref moved upstream. Local sources never have updates,
// and a source type this build cannot fetch is reported as uncheckable.
func checkUpdate(ctx context.Context, entry lockRef, runner source.Runner) (bool, error) {
	switch entry.SourceType {
	case "local":
		return false, nil
	case "github", "gitlab", "git":
	case "":
		return false, fmt.Errorf("the lock entry has no source type")
	default:
		return false, fmt.Errorf("cannot check a %s source", entry.SourceType)
	}
	raw := entry.Source
	if entry.SourceURL != "" {
		raw = entry.SourceURL
	}
	src, err := source.Parse(raw)
	if err != nil {
		return false, err
	}
	src.Ref = entry.Ref
	head, err := source.RemoteHead(ctx, src, runner)
	if err != nil {
		return false, err
	}
	return entry.Commit == "" || !strings.EqualFold(head, entry.Commit), nil
}
